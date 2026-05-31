// Package config holds the server's runtime configuration.
package config

import (
	"os"
	"time"
)

// ProtocolVersion is the wire protocol version this server speaks, and
// MinClientVersion the oldest client it still accepts. Clients below the minimum
// are told to update (see docs/protocol.md "Session lifecycle"). Bump these as
// the schema evolves; they are compile-time constants, not env-tunable.
const (
	ProtocolVersion  uint32 = 1
	MinClientVersion uint32 = 1
)

// Config is the server's runtime configuration.
type Config struct {
	ListenAddr  string // game wire protocol (Protobuf over TCP)
	AdminAddr   string // web-friendly admin surface for the admin tool
	DatabaseURL string // PostgreSQL DSN — the single source of truth

	HandshakeTimeout time.Duration // read deadline for the version+login handshake

	// AuthMode selects the authenticator: "db" (default, PostgreSQL + bcrypt) or
	// "dev" (the offline stub below, for running without seeded credentials).
	AuthMode string

	// Dev auth (stub): a single configured account, used with AuthMode="dev".
	// An empty DevSecret rejects all logins (no accidental accept-all).
	DevUser   string
	DevSecret string
}

// Load reads configuration from the environment with dev defaults.
func Load() Config {
	return Config{
		ListenAddr:  env("STARRAID_LISTEN", ":60000"),
		AdminAddr:   env("STARRAID_ADMIN", ":8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://starraid:starraid@localhost:5432/starraid?sslmode=disable"),

		HandshakeTimeout: envDuration("STARRAID_HANDSHAKE_TIMEOUT", 10*time.Second),

		AuthMode:  env("STARRAID_AUTH", "db"),
		DevUser:   env("STARRAID_DEV_USER", "dev"),
		DevSecret: env("STARRAID_DEV_SECRET", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envDuration parses a Go duration string (e.g. "10s") from the environment,
// falling back to def when unset or unparseable.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
