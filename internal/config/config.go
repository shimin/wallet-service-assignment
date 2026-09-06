package config

import "os"

type Config struct {
	NATSUrl string
	PGUrl   string
}

func Load() Config {
	return Config{
		NATSUrl: getEnv("NATS_URL", "nats://localhost:4222"),
		PGUrl:   getEnv("PG_URL", "postgres://walletuser:walletpass@localhost:5432/wallet?sslmode=disable"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
