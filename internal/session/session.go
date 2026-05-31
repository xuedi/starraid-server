// Package session drives a single connection through the StarRaid session
// lifecycle: version negotiation, authentication, then spawn/SelfAssign into a
// live session (see docs/protocol.md "Session lifecycle"). The path is identical
// for humans and bots. The live command/event loop (movement, neighbour beacons)
// is a later slice.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/xuedi/starraid-server/internal/auth"
	"github.com/xuedi/starraid-server/internal/game"
	"github.com/xuedi/starraid-server/internal/wire"

	pb "github.com/xuedi/starraid-protocol/gen/go/starraid/v1"
)

// Spawner assigns a connecting client its controlled object and releases it when
// the connection drops. *game.World satisfies it; a fake is used in tests.
type Spawner interface {
	SpawnFor() game.Object
	Despawn(id uint64)
}

// Deps is the per-connection dependency set Handle needs.
type Deps struct {
	ProtocolVersion  uint32             // version this server speaks
	MinClientVersion uint32             // oldest client version still accepted
	Auth             auth.Authenticator // credential check (dev stub or DB-backed)
	World            Spawner            // assigns the controlled object after auth
	HandshakeTimeout time.Duration      // read deadline covering hello+login
	Logger           *slog.Logger       // per-connection structured logging
}

// phase names for structured logging and protocol-error reporting.
const (
	phaseHello  = "await_hello"
	phaseLogin  = "await_login"
	phaseAuthed = "authenticated"
	phaseLive   = "live"
)

// Handle runs the handshake state machine on conn. It returns when the
// handshake fails, the peer disconnects, or ctx is cancelled. It does not close
// conn — the caller owns the connection's lifetime.
//
// State machine: await_hello → (version check) → await_login → (authenticate) →
// authenticated. Any unexpected message for the current phase, or a version/
// credential rejection, ends the session after sending the appropriate result.
func Handle(ctx context.Context, conn net.Conn, deps Deps) {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("remote", conn.RemoteAddr().String())

	// One deadline covers the whole handshake so half-open connections that
	// never speak don't linger. Cancelling ctx also unblocks the reads below.
	if deps.HandshakeTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(deps.HandshakeTimeout))
	}
	stopCtx := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stopCtx()

	id, authedOK := handshake(conn, deps, log)
	if !authedOK {
		return
	}

	// Authenticated. Clear the handshake deadline and move into the live session.
	_ = conn.SetReadDeadline(time.Time{})
	log.Info("authenticated session", "phase", phaseAuthed, "account", id.AccountID)
	liveSession(conn, deps, id, log)
}

// liveSession spawns the client's controlled object, tells it what it controls
// (SelfAssign) and its initial state (SelfUpdate), then holds the connection
// open. The controlled object is despawned when the connection drops (no
// persistence yet). The command/event loop replaces park here in a later slice.
func liveSession(conn net.Conn, deps Deps, id auth.Identity, log *slog.Logger) {
	obj := deps.World.SpawnFor()
	defer deps.World.Despawn(obj.ID)
	log = log.With("phase", phaseLive, "account", id.AccountID, "object_id", obj.ID)
	log.Info("self assigned", "x", obj.X, "y", obj.Y)

	pos := &pb.Vec2{X: obj.X, Y: obj.Y}
	if err := sendServer(conn, &pb.SelfAssign{ObjectId: obj.ID, Position: pos}); err != nil {
		log.Warn("write SelfAssign failed", "err", err)
		return
	}
	if err := sendServer(conn, &pb.SelfUpdate{ObjectId: obj.ID, Position: pos}); err != nil {
		log.Warn("write SelfUpdate failed", "err", err)
		return
	}
	park(conn)
}

// handshake performs version negotiation then login. It returns the
// authenticated identity and ok=true on success; on any failure it logs the
// outcome, sends the appropriate result message, and returns ok=false.
func handshake(conn net.Conn, deps Deps, log *slog.Logger) (auth.Identity, bool) {
	// --- await_hello → version check ---
	hello, ok := readClient(conn, log, phaseHello)
	if !ok {
		return auth.Identity{}, false
	}
	h := hello.GetHello()
	if h == nil {
		log.Warn("protocol error: expected Hello", "phase", phaseHello, "got", msgKind(hello))
		return auth.Identity{}, false
	}
	if h.ProtocolVersion < deps.MinClientVersion {
		log.Info("version rejected", "phase", phaseHello,
			"client_version", h.ProtocolVersion, "min_supported", deps.MinClientVersion)
		_ = sendServer(conn, &pb.VersionResult{Accepted: false, MinSupported: deps.MinClientVersion})
		return auth.Identity{}, false
	}
	if err := sendServer(conn, &pb.VersionResult{Accepted: true}); err != nil {
		log.Warn("write VersionResult failed", "phase", phaseHello, "err", err)
		return auth.Identity{}, false
	}

	// --- await_login → authenticate ---
	loginMsg, ok := readClient(conn, log, phaseLogin)
	if !ok {
		return auth.Identity{}, false
	}
	login := loginMsg.GetLogin()
	if login == nil {
		log.Warn("protocol error: expected LoginRequest", "phase", phaseLogin, "got", msgKind(loginMsg))
		return auth.Identity{}, false
	}
	id, err := deps.Auth.Authenticate(context.Background(), login.Username, login.Secret)
	if err != nil {
		reason := "invalid credentials"
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			reason = "authentication unavailable"
			log.Error("authenticate failed", "phase", phaseLogin, "err", err)
		} else {
			log.Info("login rejected", "phase", phaseLogin, "username", login.Username)
		}
		_ = sendServer(conn, &pb.LoginResult{Ok: false, Reason: reason})
		return auth.Identity{}, false
	}
	if err := sendServer(conn, &pb.LoginResult{Ok: true}); err != nil {
		log.Warn("write LoginResult failed", "phase", phaseLogin, "err", err)
		return auth.Identity{}, false
	}
	return id, true
}

// readClient reads one ClientMessage, logging the cause on any read failure.
func readClient(conn net.Conn, log *slog.Logger, phase string) (*pb.ClientMessage, bool) {
	m, err := wire.ReadClientMessage(conn)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrDeadlineExceeded):
			log.Info("handshake timeout", "phase", phase)
		case errors.Is(err, io.EOF):
			log.Info("peer closed during handshake", "phase", phase)
		default:
			log.Warn("read failed", "phase", phase, "err", err)
		}
		return nil, false
	}
	return m, true
}

// sendServer wraps a server payload in a ServerMessage envelope and writes it.
func sendServer(conn net.Conn, payload any) error {
	msg := &pb.ServerMessage{}
	switch p := payload.(type) {
	case *pb.VersionResult:
		msg.Msg = &pb.ServerMessage_VersionResult{VersionResult: p}
	case *pb.LoginResult:
		msg.Msg = &pb.ServerMessage_LoginResult{LoginResult: p}
	case *pb.SelfAssign:
		msg.Msg = &pb.ServerMessage_SelfAssign{SelfAssign: p}
	case *pb.SelfUpdate:
		msg.Msg = &pb.ServerMessage_SelfUpdate{SelfUpdate: p}
	default:
		return fmt.Errorf("session: unknown server payload %T", payload)
	}
	return wire.WriteServerMessage(conn, msg)
}

// park holds an authenticated connection open until the peer disconnects (the
// live session loop replaces this in a later slice). Reads are discarded.
func park(conn net.Conn) {
	buf := make([]byte, 512)
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}

// msgKind returns a short description of which oneof arm is set, for logging.
func msgKind(m *pb.ClientMessage) string {
	switch m.Msg.(type) {
	case *pb.ClientMessage_Hello:
		return "Hello"
	case *pb.ClientMessage_Login:
		return "LoginRequest"
	case nil:
		return "empty"
	default:
		return "unknown"
	}
}
