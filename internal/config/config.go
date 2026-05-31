// Package config holds the server's runtime configuration.
package config

import "os"

// Config is the server's runtime configuration.
type Config struct {
	ListenAddr  string // game wire protocol (Protobuf over TCP)
	AdminAddr   string // web-friendly admin surface for the admin tool
	DatabaseURL string // PostgreSQL DSN — the single source of truth
}

// Load reads configuration from the environment with dev defaults.
func Load() Config {
	return Config{
		ListenAddr:  env("STARRAID_LISTEN", ":60000"),
		AdminAddr:   env("STARRAID_ADMIN", ":8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://starraid:starraid@localhost:5432/starraid?sslmode=disable"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
