// Command migrate applies the server's database migrations, syncs the code-defined
// catalog, and exits. It shares the server's config (DATABASE_URL), migration set,
// and catalog; the server also runs both on startup, so this is for CI/dev
// (`just migrate`) — and lets `just seed` resolve the catalog without the server.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/xuedi/starraid-server/internal/catalog"
	"github.com/xuedi/starraid-server/internal/config"
	"github.com/xuedi/starraid-server/internal/db"
)

func main() {
	cfg := config.Load()
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := catalog.Sync(ctx, pool.Pool); err != nil {
		slog.Error("catalog sync failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied, catalog synced")
}
