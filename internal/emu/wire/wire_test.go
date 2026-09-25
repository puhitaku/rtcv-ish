package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msgs := []*emulatorv1.Message{
		{Body: &emulatorv1.Message_Request{Request: &emulatorv1.Request{Id: 7, Body: &emulatorv1.Request_Ping{Ping: &emulatorv1.PingRequest{}}}}},
		{Body: &emulatorv1.Message_Event{Event: &emulatorv1.Event{Body: &emulatorv1.Event_Frame{Frame: &emulatorv1.FrameEvent{Frame: 42}}}}},
	}
	for _, m := range msgs {
		if err := Write(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range msgs {
		got, err := Read(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if _, err := Read(&buf); err != io.EOF {
		t.Errorf("err = %v, want EOF", err)
	}
}

func TestReadErrors(t *testing.T) {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], MaxMessageSize+1)
	if _, err := Read(bytes.NewReader(hdr[:])); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}

	binary.LittleEndian.PutUint32(hdr[:], 10)
	if _, err := Read(bytes.NewReader(append(hdr[:], 1, 2))); err != io.ErrUnexpectedEOF {
		t.Errorf("err = %v, want ErrUnexpectedEOF", err)
	}

	binary.LittleEndian.PutUint32(hdr[:], 2)
	if _, err := Read(bytes.NewReader(append(hdr[:], 0xff, 0xff))); err == nil {
		t.Error("want unmarshal error")
	}
}
