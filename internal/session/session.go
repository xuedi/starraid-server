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

// World is the session's view of the authoritative world: it assigns a
// connecting client its controlled object, applies that client's navigation
// intent, exposes the object's current state, and releases it on disconnect.
// *game.World satisfies it; a fake is used in tests.
type World interface {
	SpawnFor() game.ObjectState
	Despawn(id uint64)
	SetTarget(id uint64, tx, ty int64)
	Stop(id uint64)
	Get(id uint64) (game.ObjectState, bool)
	Neighbours(exclude uint64) []game.ObjectState
}

// Deps is the per-connection dependency set Handle needs.
type Deps struct {
	ProtocolVersion  uint32             // version this server speaks
	MinClientVersion uint32             // oldest client version still accepted
	Auth             auth.Authenticator // credential check (dev stub or DB-backed)
	World            World              // controlled-object spawn + navigation
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

// selfUpdateInterval is how often the live session samples the controlled
// object's position and pushes a SelfUpdate when it has changed. Matches the
// world tick rate (see game.World.Run).
const selfUpdateInterval = 100 * time.Millisecond

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
	liveSession(ctx, conn, deps, id, log)
}

// liveSession resolves the client's controlled object — the one assigned at
// login (loaded from the DB), or a freshly spawned one when the identity carries
// none (dev/offline auth) — tells the client what it controls (SelfAssign) and
// its initial state (SelfUpdate), pushes ObjectEnter beacons for the neighbours
// it can perceive, then runs the live loop: navigation intent in (read loop) and
// authoritative position out (SelfUpdate on change). A spawned object is
// despawned on disconnect; a DB-loaded object persists (the DB owns it).
func liveSession(ctx context.Context, conn net.Conn, deps Deps, id auth.Identity, log *slog.Logger) {
	objID := id.ObjectID
	st, ok := deps.World.Get(objID)
	spawned := false
	if objID == 0 || !ok {
		st = deps.World.SpawnFor()
		objID = st.ID
		spawned = true
		defer deps.World.Despawn(objID)
	}

	log = log.With("phase", phaseLive, "account", id.AccountID, "object_id", objID)
	log.Info("self assigned", "x", st.X, "y", st.Y, "spawned", spawned)

	pos := &pb.Vec2{X: st.X, Y: st.Y}
	if err := sendServer(conn, &pb.SelfAssign{ObjectId: objID, Position: pos}); err != nil {
		log.Warn("write SelfAssign failed", "err", err)
		return
	}
	if err := sendServer(conn, &pb.SelfUpdate{ObjectId: objID, Position: pos}); err != nil {
		log.Warn("write SelfUpdate failed", "err", err)
		return
	}

	// Neighbour beacons: announce every other object the client can perceive
	// (naive whole-sector set for now; distance/sensor gating is a later slice).
	for _, n := range deps.World.Neighbours(objID) {
		if err := sendServer(conn, &pb.ObjectEnter{ObjectId: n.ID, Position: &pb.Vec2{X: n.X, Y: n.Y}}); err != nil {
			log.Warn("write ObjectEnter failed", "err", err)
			return
		}
	}

	// Read loop: apply the client's navigation intent until it disconnects.
	done := make(chan struct{})
	go func() {
		defer close(done)
		readCommands(conn, deps, objID, log)
	}()

	// Update loop: push a SelfUpdate whenever the object's position changes.
	ticker := time.NewTicker(selfUpdateInterval)
	defer ticker.Stop()
	lastX, lastY := st.X, st.Y
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			cur, ok := deps.World.Get(objID)
			if !ok {
				return
			}
			if cur.X == lastX && cur.Y == lastY {
				continue
			}
			lastX, lastY = cur.X, cur.Y
			if err := sendServer(conn, &pb.SelfUpdate{ObjectId: objID, Position: &pb.Vec2{X: cur.X, Y: cur.Y}}); err != nil {
				log.Warn("write SelfUpdate failed", "err", err)
				return
			}
		}
	}
}

// readCommands decodes client navigation intent (Move/Stop) and applies it to
// the controlled object, until a read error (typically disconnect) ends it. An
// unexpected message is logged and ignored rather than tearing down the session.
func readCommands(conn net.Conn, deps Deps, objID uint64, log *slog.Logger) {
	for {
		msg, err := wire.ReadClientMessage(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrDeadlineExceeded) {
				log.Info("live read ended", "err", err)
			}
			return
		}
		switch m := msg.Msg.(type) {
		case *pb.ClientMessage_Move:
			if t := m.Move.GetTarget(); t != nil {
				deps.World.SetTarget(objID, t.GetX(), t.GetY())
			}
		case *pb.ClientMessage_Stop:
			deps.World.Stop(objID)
		default:
			log.Warn("unexpected message in live session", "got", msgKind(msg))
		}
	}
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
	case *pb.ObjectEnter:
		msg.Msg = &pb.ServerMessage_ObjectEnter{ObjectEnter: p}
	case *pb.ObjectUpdate:
		msg.Msg = &pb.ServerMessage_ObjectUpdate{ObjectUpdate: p}
	case *pb.ObjectLeave:
		msg.Msg = &pb.ServerMessage_ObjectLeave{ObjectLeave: p}
	default:
		return fmt.Errorf("session: unknown server payload %T", payload)
	}
	return wire.WriteServerMessage(conn, msg)
}

// msgKind returns a short description of which oneof arm is set, for logging.
func msgKind(m *pb.ClientMessage) string {
	switch m.Msg.(type) {
	case *pb.ClientMessage_Hello:
		return "Hello"
	case *pb.ClientMessage_Login:
		return "LoginRequest"
	case *pb.ClientMessage_Move:
		return "Move"
	case *pb.ClientMessage_Stop:
		return "Stop"
	case nil:
		return "empty"
	default:
		return "unknown"
	}
}
