package config

import (
	"errors"
	"os"
)

type Config struct {
	NATSUrl string
	PGUrl   string
}

func Load() (Config, error) {
	cfg := Config{
		NATSUrl: os.Getenv("NATS_URL"),
		PGUrl:   os.Getenv("PG_URL"),
	}
	if cfg.NATSUrl == "" || cfg.PGUrl == "" {
		return Config{}, errors.New("NATS_URL and PG_URL must be set")
	}
	return cfg, nil
}
