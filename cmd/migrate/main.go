// Command migrate applies the server's database migrations and exits. It shares
// the server's config (DATABASE_URL) and migration set; the server also runs
// these on startup, so this is for CI/dev (`just migrate`).
package main

import (
	"log/slog"
	"os"

	"github.com/xuedi/starraid-server/internal/config"
	"github.com/xuedi/starraid-server/internal/db"
)

func main() {
	cfg := config.Load()
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")
}
