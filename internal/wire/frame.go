// Package wire implements the StarRaid frame format and envelope codec: a
// length-prefixed Protobuf stream (see docs/protocol.md, protocol/README.md).
//
// Each frame is a 4-byte big-endian uint32 byte length followed by exactly that
// many bytes of marshalled envelope. A max frame size bounds per-connection
// memory against a hostile or buggy peer.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameSize is the largest payload, in bytes, that ReadFrame will accept (and
// WriteFrame will emit). The handshake/login messages are tiny; this is a
// generous ceiling that still bounds memory. Revisit when bulk payloads appear.
const MaxFrameSize = 1 << 20 // 1 MiB

// ErrFrameTooLarge is returned when a frame's declared length exceeds
// MaxFrameSize. The caller should treat it as a protocol error and close.
var ErrFrameTooLarge = errors.New("wire: frame exceeds max size")

// ReadFrame reads one length-prefixed frame from r and returns its payload.
// A clean EOF before any byte is reported as io.EOF; an EOF mid-frame becomes
// io.ErrUnexpectedEOF. An over-size length yields ErrFrameTooLarge.
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err // io.EOF on a clean boundary; ErrUnexpectedEOF mid-header
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > MaxFrameSize {
		return nil, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, MaxFrameSize)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf, nil
}

// WriteFrame writes payload to w as a single length-prefixed frame. It rejects
// payloads larger than MaxFrameSize with ErrFrameTooLarge.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameSize {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(payload), MaxFrameSize)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
