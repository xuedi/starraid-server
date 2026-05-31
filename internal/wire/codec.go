package wire

import (
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/xuedi/starraid-protocol/gen/go/starraid/v1"
)

// ReadClientMessage reads one frame and unmarshals it as a ClientMessage.
// Used by the server to receive client intent.
func ReadClientMessage(r io.Reader) (*pb.ClientMessage, error) {
	buf, err := ReadFrame(r)
	if err != nil {
		return nil, err
	}
	m := &pb.ClientMessage{}
	if err := proto.Unmarshal(buf, m); err != nil {
		return nil, err
	}
	return m, nil
}

// WriteClientMessage marshals m and writes it as one frame.
// Used by clients/bots to send intent.
func WriteClientMessage(w io.Writer, m *pb.ClientMessage) error {
	buf, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	return WriteFrame(w, buf)
}

// ReadServerMessage reads one frame and unmarshals it as a ServerMessage.
// Used by clients/bots to receive authoritative results.
func ReadServerMessage(r io.Reader) (*pb.ServerMessage, error) {
	buf, err := ReadFrame(r)
	if err != nil {
		return nil, err
	}
	m := &pb.ServerMessage{}
	if err := proto.Unmarshal(buf, m); err != nil {
		return nil, err
	}
	return m, nil
}

// WriteServerMessage marshals m and writes it as one frame.
// Used by the server to send authoritative results.
func WriteServerMessage(w io.Writer, m *pb.ServerMessage) error {
	buf, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	return WriteFrame(w, buf)
}
