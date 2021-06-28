package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sh0rez/promqtt/relay"
	"github.com/spf13/pflag"
)

func main() {
	cfg := relay.DefaultConfig()
	pflag.StringVar(&cfg.ClientID, "client-id", cfg.ClientID, "mqtt client id")
	pflag.StringVar(&cfg.Listen, "listen", cfg.Listen, "address to listen on")
	pflag.BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "verbose logging")

	pflag.StringVarP(&cfg.Username, "username", "u", cfg.Username, "mqtt username")
	pflag.StringVarP(&cfg.Password, "password", "p", cfg.Password, "mqtt password")

	pflag.DurationVar(&cfg.PingTimeout, "ping-timeout", cfg.PingTimeout, "time to wait for PING response from broker")

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

	r, err := relay.New(cfg)
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
