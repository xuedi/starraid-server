// Package session drives a single connection through the StarRaid session
// lifecycle: version negotiation, authentication, then spawn/SelfAssign into a
// live session (see docs/protocol.md "Session lifecycle"). The path is identical
// for humans and bots. The live loop applies navigation intent and streams the
// authoritative results back — the controlled object's own position (SelfUpdate)
// plus the interest-managed neighbour set (ObjectEnter/Update/Leave).
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
	Perceived(id uint64) []game.ObjectState
}

// SessionMetrics observes authenticated live sessions for telemetry (the /stats
// surface). *stats.Registry satisfies it structurally; a nil Deps.Metrics
// disables the hook (tests, or a server without the control surface).
type SessionMetrics interface {
	SessionStart()
	SessionEnd()
}

// Deps is the per-connection dependency set Handle needs.
type Deps struct {
	ProtocolVersion  uint32             // version this server speaks
	MinClientVersion uint32             // oldest client version still accepted
	Auth             auth.Authenticator // credential check (dev stub or DB-backed)
	World            World              // controlled-object spawn + navigation
	HandshakeTimeout time.Duration      // read deadline covering hello+login
	Logger           *slog.Logger       // per-connection structured logging
	Metrics          SessionMetrics     // optional: live-session gauge for /stats (nil-safe)
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
	if deps.Metrics != nil {
		deps.Metrics.SessionStart()
		defer deps.Metrics.SessionEnd()
	}
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
	if err := sendServer(conn, &pb.SelfAssign{ObjectId: objID, Position: pos, TypeKey: st.TypeKey}); err != nil {
		log.Warn("write SelfAssign failed", "err", err)
		return
	}
	if err := sendServer(conn, &pb.SelfUpdate{ObjectId: objID, Position: pos}); err != nil {
		log.Warn("write SelfUpdate failed", "err", err)
		return
	}

	// Interest-managed neighbour beacons. seen tracks what this connection was
	// last told about; the first sync populates it (ObjectEnter for everything in
	// perception range), and the tick loop below keeps it reconciled.
	seen := make(map[uint64]game.ObjectState)
	if err := syncPerception(conn, deps.World, objID, seen); err != nil {
		log.Warn("write neighbour beacon failed", "err", err)
		return
	}

	// Read loop: apply the client's navigation intent until it disconnects.
	done := make(chan struct{})
	go func() {
		defer close(done)
		readCommands(conn, deps, objID, log)
	}()

	// Live loop: each tick, push a SelfUpdate when the controlled object has moved,
	// then reconcile the interest-managed neighbour set (enter/update/leave) as
	// objects move in and out of perception range.
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
			if cur.X != lastX || cur.Y != lastY {
				lastX, lastY = cur.X, cur.Y
				if err := sendServer(conn, &pb.SelfUpdate{ObjectId: objID, Position: &pb.Vec2{X: cur.X, Y: cur.Y}}); err != nil {
					log.Warn("write SelfUpdate failed", "err", err)
					return
				}
			}
			if err := syncPerception(conn, deps.World, objID, seen); err != nil {
				log.Warn("write neighbour beacon failed", "err", err)
				return
			}
		}
	}
}

// syncPerception reconciles what this connection was last told about (seen)
// against the object's freshly perceived set, sending an ObjectEnter for each
// newcomer, an ObjectUpdate for each mover, and an ObjectLeave for each object no
// longer perceived. seen is mutated in place. Returns the first write error.
func syncPerception(conn net.Conn, world World, objID uint64, seen map[uint64]game.ObjectState) error {
	enter, update, leave := perceptionDelta(world.Perceived(objID), seen)
	for _, n := range enter {
		if err := sendServer(conn, &pb.ObjectEnter{ObjectId: n.ID, Position: &pb.Vec2{X: n.X, Y: n.Y}, TypeKey: n.TypeKey}); err != nil {
			return err
		}
	}
	for _, n := range update {
		if err := sendServer(conn, &pb.ObjectUpdate{ObjectId: n.ID, Position: &pb.Vec2{X: n.X, Y: n.Y}}); err != nil {
			return err
		}
	}
	for _, id := range leave {
		if err := sendServer(conn, &pb.ObjectLeave{ObjectId: id}); err != nil {
			return err
		}
	}
	return nil
}

// perceptionDelta diffs a freshly perceived set against seen (the last-sent
// state), returning the objects to announce as entering, the moved ones to
// update, and the ids that have left. It updates seen in place to match the new
// set. Pure over its inputs (no I/O) so the diff logic is unit-testable.
func perceptionDelta(perceived []game.ObjectState, seen map[uint64]game.ObjectState) (enter, update []game.ObjectState, leave []uint64) {
	live := make(map[uint64]bool, len(perceived))
	for _, n := range perceived {
		live[n.ID] = true
		switch prev, ok := seen[n.ID]; {
		case !ok:
			enter = append(enter, n)
		case n.X != prev.X || n.Y != prev.Y:
			update = append(update, n)
		}
		seen[n.ID] = n
	}
	for id := range seen {
		if !live[id] {
			leave = append(leave, id)
			delete(seen, id)
		}
	}
	return enter, update, leave
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
