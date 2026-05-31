package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	pb "github.com/xuedi/starraid-protocol/gen/go/starraid/v1"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		[]byte("hello"),
		bytes.Repeat([]byte{0xAB}, 4096),
	}
	for _, payload := range cases {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, payload); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(payload))
		}
	}
}

func TestReadFrameCleanEOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF on empty stream, got %v", err)
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 10)
	r := bytes.NewReader(append(hdr[:], []byte("short")...)) // claims 10, supplies 5
	_, err := ReadFrame(r)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], MaxFrameSize+1)
	_, err := ReadFrame(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestWriteFrameTooLarge(t *testing.T) {
	err := WriteFrame(io.Discard, make([]byte, MaxFrameSize+1))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestClientMessageCodecRoundTrip(t *testing.T) {
	msg := &pb.ClientMessage{Msg: &pb.ClientMessage_Login{
		Login: &pb.LoginRequest{Username: "ada", Secret: "lovelace"},
	}}
	var buf bytes.Buffer
	if err := WriteClientMessage(&buf, msg); err != nil {
		t.Fatalf("WriteClientMessage: %v", err)
	}
	got, err := ReadClientMessage(&buf)
	if err != nil {
		t.Fatalf("ReadClientMessage: %v", err)
	}
	login := got.GetLogin()
	if login == nil || login.Username != "ada" || login.Secret != "lovelace" {
		t.Fatalf("decoded login = %+v", login)
	}
}

func TestServerMessageCodecRoundTrip(t *testing.T) {
	msg := &pb.ServerMessage{Msg: &pb.ServerMessage_VersionResult{
		VersionResult: &pb.VersionResult{Accepted: false, MinSupported: 7},
	}}
	var buf bytes.Buffer
	if err := WriteServerMessage(&buf, msg); err != nil {
		t.Fatalf("WriteServerMessage: %v", err)
	}
	got, err := ReadServerMessage(&buf)
	if err != nil {
		t.Fatalf("ReadServerMessage: %v", err)
	}
	vr := got.GetVersionResult()
	if vr == nil || vr.Accepted || vr.MinSupported != 7 {
		t.Fatalf("decoded version result = %+v", vr)
	}
}
