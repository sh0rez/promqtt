package relay

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func New(cfg Config) (*Relay, error) {
	mqtt.ERROR = log.New(os.Stderr, "", log.Flags())
	if cfg.Verbose {
		mqtt.DEBUG = log.New(os.Stderr, "debug:", log.Flags())
	}

	r := &Relay{
		data: make(map[string]string),
	}

	opts := cfg.MQTT()
	if len(opts.Servers) == 0 {
		return nil, fmt.Errorf("must specify broker url")
	}

	opts.SetConnectionLostHandler(func(c mqtt.Client, e error) {
		log.Printf("connection lost: %s", e.Error())
	})

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		tok := c.Subscribe("#", 0, r.HandleMQTT)

		if tok.Wait() && tok.Error() != nil {
			log.Fatalf("failed to subscribe to all topics: %s", tok.Error())
		}
		log.Println("subscribed to '#'")
	})

	if tok := mqtt.NewClient(opts).Connect(); tok.Wait() && tok.Error() != nil {
		return nil, tok.Error()
	}

	return r, nil
}

type Relay struct {
	mu   sync.RWMutex
	data map[string]string
}

func (rl *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		http.Error(w, "must pass topic", http.StatusBadRequest)
		return
	}
	topicExp, err := regexp.Compile(topic)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	regex := r.URL.Query().Get("regex")
	if regex == "" {
		regex = `(.*)`
	}

	regexExp, err := regexp.Compile(regex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reg := rl.metrics(topicExp, regexExp)
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

func (rl *Relay) metrics(topicExp, matcher *regexp.Regexp) *prometheus.Registry {
	reg := prometheus.NewRegistry()

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	for topic, data := range rl.data {
		if !topicExp.MatchString(topic) {
			continue
		}

		matches := matcher.FindStringSubmatch(data)
		if len(matches) == 0 {
			continue
		}

		for i, name := range matcher.SubexpNames() {
			if i == 0 {
				continue
			}

			value, err := strconv.ParseFloat(matches[i], 64)
			if err != nil {
				log.Printf("failed to parse '%s' from '%s' as float64", data, topic)
				continue
			}

			g := prometheus.NewGauge(prometheus.GaugeOpts{
				Name:        metricName(topic) + name,
				ConstLabels: prometheus.Labels{"topic": topic},
			})
			g.Set(value)

			if err := reg.Register(g); err != nil {
				log.Println(err)
			}
		}
	}

	return reg
}

func (rl *Relay) HandleMQTT(c mqtt.Client, m mqtt.Message) {
	rl.mu.Lock()
	rl.data[m.Topic()] = string(m.Payload())
	rl.mu.Unlock()
}

// metricName sanitizes a string so that it becomes a valid Prometheus metric
// name by replacing all illegal characters with underscores (_)
func metricName(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
