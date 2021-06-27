package main

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
	"github.com/spf13/pflag"
)

type Config struct {
	Broker   string
	ClientID string

	Listen string

	Verbose bool
}

func DefaultConfig() Config {
	c := Config{
		ClientID: "promqtt",
		Listen:   ":9337",
		Verbose:  false,
	}

	if hostname, err := os.Hostname(); err == nil {
		c.ClientID += "@" + hostname
	}

	return c
}

func main() {
	cfg := DefaultConfig()
	pflag.StringVar(&cfg.Listen, "listen", cfg.Listen, "address to listen on")
	pflag.StringVar(&cfg.ClientID, "client-id", cfg.ClientID, "mqtt client id")
	pflag.BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "verbose logging")
	pflag.Usage = func() {
		fmt.Printf("Usage: %s <broker> [flags]\n", os.Args[0])
		pflag.PrintDefaults()
		os.Exit(1)
	}

	pflag.Parse()
	if len(pflag.Args()) != 1 {
		pflag.Usage()
	}

	cfg.Broker = pflag.Arg(0)

	if cfg.Verbose {
		mqtt.ERROR = log.New(os.Stdout, "", 0)
	}

	c := mqtt.NewClient(mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetConnectionLostHandler(func(c mqtt.Client, e error) { log.Printf("connection lost: %s", e.Error()) }),
	)
	if t := c.Connect(); t.Wait() && t.Error() != nil {
		log.Fatalf("failed to connect to broker: %s", t.Error())
	}
	log.Printf("connected to broker at '%s' as '%s'", cfg.Broker, cfg.ClientID)
	defer c.Disconnect(0)

	r, err := NewRelay(c)
	if err != nil {
		log.Fatalln(err)
	}

	http.Handle("/mqtt", r)
	http.Handle("/metrics", promhttp.Handler())

	log.Printf("listening at %s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, nil); err != nil {
		log.Fatalln(err)
	}
}

func NewRelay(m mqtt.Client) (*Relay, error) {
	r := &Relay{
		data: make(map[string]string),
	}

	if tok := m.Subscribe("#", 0, r.HandleMQTT); tok.Wait() && tok.Error() != nil {
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
