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
		HandshakeTimeout: 2 * time.Second,
		Logger:           quietLogger(),
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

	sendHello(t, conn, 1)
	if vr := readServer(t, conn).GetVersionResult(); vr == nil || !vr.Accepted {
		t.Fatalf("want VersionResult{accepted}, got %+v", vr)
	}

	sendLogin(t, conn, "dev", "s3cr3t")
	if lr := readServer(t, conn).GetLoginResult(); lr == nil || !lr.Ok {
		t.Fatalf("want LoginResult{ok}, got %+v", lr)
	}
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
