package relay

import (
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Broker   string
	ClientID string

	Listen  string
	Verbose bool

	Username string
	Password string

	PingTimeout time.Duration
}

func DefaultConfig() Config {
	opts := mqtt.NewClientOptions()

	clientID := "promqtt"
	if hostname, err := os.Hostname(); err == nil {
		clientID += "@" + hostname
	}

	return Config{
		ClientID: clientID,

		Listen:  ":9337",
		Verbose: false,

		PingTimeout: opts.PingTimeout,
	}
}

func (c Config) MQTT() *mqtt.ClientOptions {
	return mqtt.NewClientOptions().
		AddBroker(c.Broker).
		SetClientID(c.ClientID).
		SetUsername(c.Username).
		SetPassword(c.Password).
		SetPingTimeout(c.PingTimeout)
}
