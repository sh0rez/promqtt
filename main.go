package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
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
}

func DefaultConfig() Config {
	return Config{
		ClientID: "promqtt",
		Listen:   ":9337",
	}
}

func main() {
	cfg := DefaultConfig()
	pflag.StringVar(&cfg.Listen, "listen", cfg.Listen, "address to listen on")
	pflag.StringVar(&cfg.ClientID, "client-id", cfg.ClientID, "mqtt client id")
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

	c := mqtt.NewClient(mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID),
	)
	if t := c.Connect(); t.Wait() && t.Error() != nil {
		log.Fatalf("failed to connect to broker: %s", t.Error())
	}
	log.Printf("connected to broker at '%s' as '%s'", cfg.Broker, cfg.ClientID)

	r := NewRelay(c)

	http.Handle("/mqtt", r)
	http.Handle("/metrics", promhttp.Handler())

	log.Printf("listening at %s", cfg.Listen)
	if err := http.ListenAndServe(cfg.Listen, nil); err != nil {
		log.Fatalln(err)
	}
}

func NewRelay(m mqtt.Client) *Relay {
	return &Relay{
		mqtt:    m,
		targets: make(map[string]*Target),
	}
}

type Relay struct {
	mqtt mqtt.Client

	targets map[string]*Target
	mu      sync.RWMutex
}

func (rl *Relay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		http.Error(w, "must pass topic", http.StatusBadRequest)
		return
	}

	if _, has := rl.targets[topic]; !has {
		if err := rl.addTarget(topic); err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	reg := prometheus.NewRegistry()
	t := rl.targets[topic]
	for topic, val := range t.data {
		opts := prometheus.GaugeOpts{
			Name:        metricName(topic),
			ConstLabels: prometheus.Labels{"topic": topic},
		}
		g := prometheus.NewGauge(opts)
		g.Set(val)

		if err := reg.Register(g); err != nil {
			log.Println(err)
		}
	}

	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

func (rl *Relay) HandleMQTT(c mqtt.Client, m mqtt.Message) {
	val, err := strconv.ParseFloat(string(m.Payload()), 64)
	if err != nil {
		log.Printf("failed to parse '%s' from '%s' as float: %s", m.Payload(), m.Topic(), err)
		return
	}

	// add received value to all targets that match this topic
	for name, t := range rl.targets {
		if !mqttMatches(name, m.Topic()) {
			continue
		}

		t.data[m.Topic()] = val
	}
}

// addTarget subscribes the relay to the given topic and sets up the required
// internal data structures
func (rl *Relay) addTarget(topic string) error {
	tok := rl.mqtt.Subscribe(topic, 0, rl.HandleMQTT)
	if tok.Wait() && tok.Error() != nil {
		return fmt.Errorf("failed subscribint to '%s': %w", topic, tok.Error())
	}
	log.Printf("subscribed to '%s'", topic)

	rl.targets[topic] = &Target{
		data: make(map[string]float64),
	}
	return nil
}

// mqttMatches determines whether topic is matched by the wildcard
func mqttMatches(wildcard string, topic string) bool {
	return true
}

// metricName sanitizes a string so that it becomes a valid Prometheus metric
// name by replacing all illegal characters with underscores (_)
func metricName(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}

// Target represents any topic we are subscribed to. Because these are likely
// wildcards, we may receive data for several distinct topics, thus data is a
// map
type Target struct {
	data map[string]float64
	mu   sync.RWMutex
}
