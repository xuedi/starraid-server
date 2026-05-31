package session_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/xuedi/starraid-server/internal/auth"
	"github.com/xuedi/starraid-server/internal/game"
	"github.com/xuedi/starraid-server/internal/session"
	"github.com/xuedi/starraid-server/internal/wire"

	pb "github.com/xuedi/starraid-protocol/gen/go/starraid/v1"
)

// quietLogger discards log output so test runs stay clean.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func defaultDeps() session.Deps {
	return session.Deps{
		ProtocolVersion:  1,
		MinClientVersion: 1,
		Auth:             auth.Dev{User: "dev", Secret: "s3cr3t"},
		World:            game.New(),
		HandshakeTimeout: 2 * time.Second,
		Logger:           quietLogger(),
	}
}

// authenticate runs the full handshake+login over conn, asserting success.
func authenticate(t *testing.T, conn net.Conn) {
	t.Helper()
	sendHello(t, conn, 1)
	if vr := readServer(t, conn).GetVersionResult(); vr == nil || !vr.Accepted {
		t.Fatalf("want VersionResult{accepted}, got %+v", vr)
	}
	sendLogin(t, conn, "dev", "s3cr3t")
	if lr := readServer(t, conn).GetLoginResult(); lr == nil || !lr.Ok {
		t.Fatalf("want LoginResult{ok}, got %+v", lr)
	}
}

// serve starts a listener that runs session.Handle for each connection with the
// given deps, and returns the dial address plus a stop func.
func serve(t *testing.T, deps session.Deps) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				session.Handle(ctx, conn, deps)
			}()
		}
	}()
	return ln.Addr().String(), func() { cancel(); _ = ln.Close() }
}

func sendHello(t *testing.T, conn net.Conn, version uint32) {
	t.Helper()
	msg := &pb.ClientMessage{Msg: &pb.ClientMessage_Hello{Hello: &pb.Hello{ProtocolVersion: version}}}
	if err := wire.WriteClientMessage(conn, msg); err != nil {
		t.Fatalf("send Hello: %v", err)
	}
}

func sendLogin(t *testing.T, conn net.Conn, user, secret string) {
	t.Helper()
	msg := &pb.ClientMessage{Msg: &pb.ClientMessage_Login{Login: &pb.LoginRequest{Username: user, Secret: secret}}}
	if err := wire.WriteClientMessage(conn, msg); err != nil {
		t.Fatalf("send LoginRequest: %v", err)
	}
}

func sendMove(t *testing.T, conn net.Conn, x, y int64) {
	t.Helper()
	msg := &pb.ClientMessage{Msg: &pb.ClientMessage_Move{Move: &pb.Move{Target: &pb.Vec2{X: x, Y: y}}}}
	if err := wire.WriteClientMessage(conn, msg); err != nil {
		t.Fatalf("send Move: %v", err)
	}
}

func sendStop(t *testing.T, conn net.Conn) {
	t.Helper()
	msg := &pb.ClientMessage{Msg: &pb.ClientMessage_Stop{Stop: &pb.Stop{}}}
	if err := wire.WriteClientMessage(conn, msg); err != nil {
		t.Fatalf("send Stop: %v", err)
	}
}

func readServer(t *testing.T, conn net.Conn) *pb.ServerMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	m, err := wire.ReadServerMessage(conn)
	if err != nil {
		t.Fatalf("read ServerMessage: %v", err)
	}
	return m
}

func TestHappyPath(t *testing.T) {
	addr, stop := serve(t, defaultDeps())
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	authenticate(t, conn)

	// After auth the server assigns the controlled object and pushes its
	// initial state.
	sa := readServer(t, conn).GetSelfAssign()
	if sa == nil || sa.ObjectId == 0 || sa.Position == nil {
		t.Fatalf("want SelfAssign{object_id>0, position}, got %+v", sa)
	}
	su := readServer(t, conn).GetSelfUpdate()
	if su == nil || su.ObjectId != sa.ObjectId {
		t.Fatalf("want SelfUpdate for object %d, got %+v", sa.ObjectId, su)
	}
}

// TestControlledObjectDespawnsOnDisconnect verifies the assigned object exists
// while the connection is live and is removed once it drops.
func TestControlledObjectDespawnsOnDisconnect(t *testing.T) {
	w := game.New()
	deps := defaultDeps()
	deps.World = w
	addr, stop := serve(t, deps)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	authenticate(t, conn)
	if readServer(t, conn).GetSelfAssign() == nil {
		t.Fatalf("want SelfAssign")
	}

	waitForCount(t, w, 1) // object present while connected
	_ = conn.Close()
	waitForCount(t, w, 0) // despawned after disconnect
}

// TestMovement drives the controlled object with Move/Stop and verifies the
// SelfUpdate stream advances toward the target then halts after Stop. The world
// tick loop must run for motion to happen.
func TestMovement(t *testing.T) {
	w := game.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	deps := defaultDeps()
	deps.World = w
	addr, stop := serve(t, deps)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	authenticate(t, conn)
	if readServer(t, conn).GetSelfAssign() == nil {
		t.Fatalf("want SelfAssign")
	}
	if su := readServer(t, conn).GetSelfUpdate(); su == nil || su.Position.GetX() != 0 {
		t.Fatalf("want initial SelfUpdate at origin, got %+v", su)
	}

	// Move far along +x so it keeps moving; updates should advance X, hold Y=0.
	sendMove(t, conn, 1_000_000, 0)
	var prevX int64 = -1
	for i := 0; i < 3; i++ {
		su := readServer(t, conn).GetSelfUpdate()
		if su == nil {
			t.Fatalf("want SelfUpdate while moving, got non-self message")
		}
		if su.Position.GetX() <= prevX {
			t.Fatalf("position not advancing: prevX=%d, got X=%d", prevX, su.Position.GetX())
		}
		if su.Position.GetY() != 0 {
			t.Fatalf("want Y=0 moving along +x, got %d", su.Position.GetY())
		}
		prevX = su.Position.GetX()
	}

	// Stop, then drain any in-flight update; updates must then cease entirely.
	sendStop(t, conn)
	drainUntilQuiet(t, conn)
	_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, err := wire.ReadServerMessage(conn); err == nil {
		t.Fatalf("received a SelfUpdate after Stop — object still moving")
	} else if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("want read deadline after Stop, got %v", err)
	}
}

// drainUntilQuiet reads SelfUpdates until a read times out (no more arrive),
// absorbing any updates already in flight when Stop was sent.
func drainUntilQuiet(t *testing.T, conn net.Conn) {
	t.Helper()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		if _, err := wire.ReadServerMessage(conn); err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return
			}
			t.Fatalf("drain read: %v", err)
		}
	}
}

// waitForCount polls w.Count() until it equals want or a deadline passes.
func waitForCount(t *testing.T, w *game.World, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Count() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("world object count = %d, want %d", w.Count(), want)
}

func TestVersionTooOld(t *testing.T) {
	addr, stop := serve(t, defaultDeps())
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sendHello(t, conn, 0) // below MinClientVersion of 1
	vr := readServer(t, conn).GetVersionResult()
	if vr == nil || vr.Accepted {
		t.Fatalf("want VersionResult{accepted:false}, got %+v", vr)
	}
	if vr.MinSupported != 1 {
		t.Fatalf("want min_supported=1, got %d", vr.MinSupported)
	}
	// Connection must close without ever accepting a login.
	expectClosed(t, conn)
}

func TestBadCredentials(t *testing.T) {
	addr, stop := serve(t, defaultDeps())
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	sendHello(t, conn, 1)
	if vr := readServer(t, conn).GetVersionResult(); vr == nil || !vr.Accepted {
		t.Fatalf("want VersionResult{accepted}, got %+v", vr)
	}

	sendLogin(t, conn, "dev", "wrong")
	lr := readServer(t, conn).GetLoginResult()
	if lr == nil || lr.Ok {
		t.Fatalf("want LoginResult{ok:false}, got %+v", lr)
	}
	if lr.Reason == "" {
		t.Fatalf("want a non-empty rejection reason")
	}
	expectClosed(t, conn)
}

func TestHandshakeTimeout(t *testing.T) {
	deps := defaultDeps()
	deps.HandshakeTimeout = 150 * time.Millisecond
	addr, stop := serve(t, deps)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send no Hello. The server must close the connection after the timeout.
	expectClosed(t, conn)
}

// expectClosed asserts the server closes its side: a read returns EOF (or a
// connection-reset error) rather than data, within a generous deadline.
func expectClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := wire.ReadServerMessage(conn)
	if err == nil {
		t.Fatalf("expected connection to be closed, but read a message")
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection not closed within deadline")
	}
}
