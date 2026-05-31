// Command server is the StarRaid authoritative game server (see docs/server.md).
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/xuedi/starraid-server/internal/config"
	"github.com/xuedi/starraid-server/internal/game"
)

func main() {
	cfg := config.Load()
	slog.Info("starraid server starting", "listen", cfg.ListenAddr, "admin", cfg.AdminAddr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The authoritative in-memory world (active slice). Real startup loads the
	// world setting + active-area objects from PostgreSQL.
	world := game.New()
	go world.Run(ctx)

	// Game wire protocol: Protobuf over TCP (see docs/protocol.md). Listener stub.
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		slog.Error("listen failed", "err", err)
		os.Exit(1)
	}
	defer ln.Close()
	go acceptLoop(ctx, ln)

	<-ctx.Done()
	slog.Info("starraid server shutting down")
}

// acceptLoop accepts game connections. TODO: version handshake → auth → session.
func acceptLoop(ctx context.Context, ln net.Listener) {
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
		slog.Info("connection received (stub: closing)", "remote", conn.RemoteAddr())
		_ = conn.Close()
	}
}
