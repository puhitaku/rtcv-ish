package fake

import (
	"net"
	"testing"
	"time"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
	"github.com/puhitaku/rtcv-ish/internal/emu/wire"
)

func rawConn(t *testing.T) net.Conn {
	t.Helper()
	s, err := New(Options{Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}

func roundTrip(t *testing.T, c net.Conn, req *emulatorv1.Request) *emulatorv1.Response {
	t.Helper()
	if err := wire.Write(c, &emulatorv1.Message{Body: &emulatorv1.Message_Request{Request: req}}); err != nil {
		t.Fatal(err)
	}
	m, err := wire.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if m.GetResponse().GetId() != req.GetId() {
		t.Fatalf("response id %d, want %d", m.GetResponse().GetId(), req.GetId())
	}
	return m.GetResponse()
}

func TestHelloRequired(t *testing.T) {
	c := rawConn(t)
	resp := roundTrip(t, c, &emulatorv1.Request{Id: 1, Body: &emulatorv1.Request_Ping{Ping: &emulatorv1.PingRequest{}}})
	if resp.GetError().GetCode() != emulatorv1.Error_INVALID_ARGUMENT {
		t.Errorf("Ping before Hello = %v", resp)
	}
	resp = roundTrip(t, c, &emulatorv1.Request{Id: 2, Body: &emulatorv1.Request_Hello{Hello: &emulatorv1.HelloRequest{ProtocolVersion: 2}}})
	if resp.GetError().GetCode() != emulatorv1.Error_UNSUPPORTED {
		t.Errorf("Hello v2 = %v", resp)
	}
	resp = roundTrip(t, c, &emulatorv1.Request{Id: 3, Body: &emulatorv1.Request_Hello{Hello: &emulatorv1.HelloRequest{ProtocolVersion: 1}}})
	if resp.GetHello() == nil {
		t.Errorf("Hello v1 = %v", resp)
	}
	resp = roundTrip(t, c, &emulatorv1.Request{Id: 4, Body: &emulatorv1.Request_Ping{Ping: &emulatorv1.PingRequest{}}})
	if resp.GetPing() == nil {
		t.Errorf("Ping = %v", resp)
	}
}

func TestMalformedFrameClosesConnection(t *testing.T) {
	c := rawConn(t)
	if _, err := c.Write([]byte{2, 0, 0, 0, 0xff, 0xff}); err != nil {
		t.Fatal(err)
	}
	if _, err := wire.Read(c); err == nil {
		t.Fatal("connection still open after a malformed frame")
	}
}

func TestTilt(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		tilt int64
		big  bool
		want []byte
	}{
		{[]byte{0xff}, 1, false, []byte{0x00}},
		{[]byte{0x00, 0x00}, -1, false, []byte{0xff, 0xff}},
		{[]byte{0xff, 0x00}, 1, false, []byte{0x00, 0x01}},
		{[]byte{0x00, 0xff}, 1, true, []byte{0x01, 0x00}},
		{[]byte{0xff, 0xff, 0xff, 0xff}, 2, false, []byte{0x01, 0, 0, 0}},
		{[]byte{0, 0, 0, 0, 0, 0, 0, 0x80}, -1, false, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}},
	} {
		b := append([]byte(nil), tc.in...)
		putUint(b, getUint(b, tc.big)+uint64(tc.tilt), tc.big)
		if string(b) != string(tc.want) {
			t.Errorf("% x %+d (big=%v) = % x, want % x", tc.in, tc.tilt, tc.big, b, tc.want)
		}
	}
}
