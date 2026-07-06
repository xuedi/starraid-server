// Command server is the StarRaid authoritative game server (see docs/server.md).
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/xuedi/starraid-server/internal/auth"
	"github.com/xuedi/starraid-server/internal/catalog"
	"github.com/xuedi/starraid-server/internal/config"
	"github.com/xuedi/starraid-server/internal/db"
	"github.com/xuedi/starraid-server/internal/game"
	"github.com/xuedi/starraid-server/internal/session"
	"github.com/xuedi/starraid-server/internal/stats"
)

func main() {
	cfg := config.Load()
	slog.Info("starraid server starting", "listen", cfg.ListenAddr, "admin", cfg.AdminAddr,
		"protocol_version", config.ProtocolVersion, "min_client_version", config.MinClientVersion)
	// The server owns the schema and applies migrations on startup (idempotent;
	// see docs/database.md). Standalone runs use cmd/migrate (`just migrate`).
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		slog.Error("database migration failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PostgreSQL is the single source of truth. Open the pool and prime the
	// in-memory active slice from the starting sector (see docs/database.md).
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Code is the source of truth for the catalog's STRUCTURE: sync the
	// code-defined classes/modules/items into the catalog tables so the admin
	// (and the world-load join below) can resolve them (see docs/database.md).
	if err := catalog.Sync(ctx, pool.Pool); err != nil {
		slog.Error("catalog sync failed", "err", err)
		os.Exit(1)
	}

	world := game.New()
	if sectorID, ok, err := pool.FirstSectorID(ctx); err != nil {
		slog.Error("load starting sector failed", "err", err)
		os.Exit(1)
	} else if ok {
		objs, err := pool.LoadSectorObjects(ctx, sectorID)
		if err != nil {
			slog.Error("load sector objects failed", "err", err)
			os.Exit(1)
		}
		seeds := make([]game.Seed, len(objs))
		for i, o := range objs {
			s := game.Seed{ID: o.ID, X: o.X, Y: o.Y, TypeKey: o.TypeKey, BaseMass: o.BaseMass}
			for _, m := range o.Modules {
				s.Modules = append(s.Modules, game.Module{Mass: m.Mass, Params: m.Params})
			}
			for _, c := range o.Cargo {
				s.Cargo = append(s.Cargo, game.Cargo{UnitMass: c.UnitMass, Quantity: c.Quantity})
			}
			seeds[i] = s
		}
		world.Load(seeds)
		slog.Info("loaded starting sector", "sector_id", sectorID, "objects", len(seeds))
	} else {
		slog.Warn("no sector found; world starts empty (run the admin seed)")
	}
	go world.Run(ctx)

	// Read-only telemetry surface: live session/object/tick counts polled by the
	// control tools (stackctl, later the admin console) over HTTP on AdminAddr.
	reg := stats.New(world.Count, 10.0) // ~10 Hz placeholder tick (see game.World.Run)
	go serveControl(ctx, cfg.AdminAddr, reg)

	// Authenticator: DB-backed (bcrypt) by default; the offline dev stub when
	// AuthMode="dev" (see docs/server.md).
	var authn auth.Authenticator
	switch cfg.AuthMode {
	case "dev":
		if cfg.DevSecret == "" {
			slog.Warn("dev auth selected but STARRAID_DEV_SECRET is empty: all logins will be rejected")
		}
		authn = auth.Dev{User: cfg.DevUser, Secret: cfg.DevSecret}
	default:
		authn = auth.DBAuthenticator{Pool: pool}
	}

	// Per-connection handshake dependencies (version negotiation + auth).
	deps := session.Deps{
		ProtocolVersion:  config.ProtocolVersion,
		MinClientVersion: config.MinClientVersion,
		Auth:             authn,
		World:            world,
		HandshakeTimeout: cfg.HandshakeTimeout,
		Logger:           slog.Default(),
		Metrics:          reg,
	}

	// Game wire protocol: Protobuf over TCP (see docs/protocol.md).
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	}
	defer ln.Close()
	go acceptLoop(ctx, ln, deps)

	<-ctx.Done()
	slog.Info("starraid server shutting down")
}

// acceptLoop accepts game connections and drives each through the handshake
// (version negotiation → auth → authenticated session) on its own goroutine.
func acceptLoop(ctx context.Context, ln net.Listener, deps session.Deps) {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("accept failed", "err", err)
			continue
		}
		go func() {
			defer conn.Close()
			session.Handle(ctx, conn, deps)
		}()
	}
}

// serveControl runs the read-only control/telemetry HTTP surface (health + stats)
// on addr until ctx is cancelled. Not fatal if it fails — the game wire is the
// server's real job.
func serveControl(ctx context.Context, addr string, reg *stats.Registry) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/stats", reg.Handler())
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	slog.Info("control surface listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("control surface failed", "err", err)
	}
}
