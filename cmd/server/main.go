// Command server is the StarRaid authoritative game server (see docs/server.md).
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/xuedi/starraid-server/internal/auth"
	"github.com/xuedi/starraid-server/internal/config"
	"github.com/xuedi/starraid-server/internal/game"
	"github.com/xuedi/starraid-server/internal/session"
)

func main() {
	cfg := config.Load()
	slog.Info("starraid server starting", "listen", cfg.ListenAddr, "admin", cfg.AdminAddr,
		"protocol_version", config.ProtocolVersion, "min_client_version", config.MinClientVersion)
	if cfg.DevSecret == "" {
		slog.Warn("dev auth disabled: STARRAID_DEV_SECRET is empty, all logins will be rejected")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The authoritative in-memory world (active slice). Real startup loads the
	// world setting + active-area objects from PostgreSQL.
	world := game.New()
	go world.Run(ctx)

	// Per-connection handshake dependencies (version negotiation + auth). The dev
	// auth stub stands in until DB-backed credentials land (see docs/server.md).
	deps := session.Deps{
		ProtocolVersion:  config.ProtocolVersion,
		MinClientVersion: config.MinClientVersion,
		Auth:             auth.Dev{User: cfg.DevUser, Secret: cfg.DevSecret},
		World:            world,
		HandshakeTimeout: cfg.HandshakeTimeout,
		Logger:           slog.Default(),
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
