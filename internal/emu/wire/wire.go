// Package wire implements the emulator API framing: a uint32 little-endian
// length followed by one serialized Message.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

// MaxMessageSize is the largest encoded Message accepted on the wire.
const MaxMessageSize = 64 << 20

var ErrTooLarge = errors.New("wire: message too large")

// Write encodes m and writes it as one frame.
func Write(w io.Writer, m *emulatorv1.Message) error {
	body, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("wire: marshal: %w", err)
	}
	if len(body) > MaxMessageSize {
		return fmt.Errorf("%w: %d bytes", ErrTooLarge, len(body))
	}
	buf := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(buf, uint32(len(body)))
	copy(buf[4:], body)
	_, err = w.Write(buf)
	return err
}

// Read reads and decodes one frame. It returns io.EOF only when the stream
// ends cleanly between frames.
func Read(r io.Reader) (*emulatorv1.Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > MaxMessageSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	m := new(emulatorv1.Message)
	if err := proto.Unmarshal(body, m); err != nil {
		return nil, fmt.Errorf("wire: unmarshal: %w", err)
	}
	return m, nil
}
