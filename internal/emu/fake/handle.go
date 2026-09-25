package fake

import (
	"bytes"
	"encoding/gob"

	"google.golang.org/protobuf/proto"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

const stateMagic = "rtcv-ish fake savestate v1"

type savestate struct {
	Magic  string
	ROM    string
	Frame  uint64
	Memory map[string][]byte
}

func (s *Server) handle(c *conn, req *emulatorv1.Request) *emulatorv1.Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req.GetHello() == nil && !c.hello {
		return errResp(errorf(emulatorv1.Error_INVALID_ARGUMENT, "the first request must be Hello"))
	}

	switch b := req.GetBody().(type) {
	case *emulatorv1.Request_Hello:
		if v := b.Hello.GetProtocolVersion(); v != 1 {
			return errResp(errorf(emulatorv1.Error_UNSUPPORTED, "protocol version %d is not supported", v))
		}
		c.hello = true
		return &emulatorv1.Response{Body: &emulatorv1.Response_Hello{Hello: &emulatorv1.HelloResponse{
			ProtocolVersion: 1,
			Emulator:        "fake",
			Version:         "0",
			System:          "nds",
			Capabilities: &emulatorv1.Capabilities{
				Savestates: true,
				Screenshot: true,
				Input:      true,
				LoadRom:    true,
				Reset_:     true,
				MaxPayload: MaxPayload,
				// SCANLINE runs as FRAME (there are no scanlines); HARD
				// masks the game's and the client's writes.
				ScanlineUnits: true,
				HardUnits:     true,
			},
		}}}

	case *emulatorv1.Request_GetStatus:
		return &emulatorv1.Response{Body: &emulatorv1.Response_GetStatus{GetStatus: &emulatorv1.GetStatusResponse{Status: s.status()}}}

	case *emulatorv1.Request_ListDomains:
		var ds []*emulatorv1.Domain
		if s.state != emulatorv1.Status_NO_ROM {
			for _, d := range s.opts.Domains {
				ds = append(ds, &emulatorv1.Domain{
					Name:      d.Name,
					Size:      d.Size,
					WordSize:  d.WordSize,
					BigEndian: d.BigEndian,
					Writable:  !d.ReadOnly,
					Hidden:    d.Hidden,
				})
			}
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_ListDomains{ListDomains: &emulatorv1.ListDomainsResponse{Domains: ds}}}

	case *emulatorv1.Request_Read:
		var total uint64
		for _, r := range b.Read.GetRanges() {
			total += uint64(r.GetSize())
		}
		if total > MaxPayload {
			return errResp(errorf(emulatorv1.Error_INVALID_ARGUMENT, "read of %d bytes exceeds max payload", total))
		}
		data := make([][]byte, 0, len(b.Read.GetRanges()))
		for _, r := range b.Read.GetRanges() {
			_, m, e := s.checkRange(r.GetDomain(), r.GetAddress(), uint64(r.GetSize()), false)
			if e != nil {
				return errResp(e)
			}
			data = append(data, append([]byte(nil), m[r.GetAddress():r.GetAddress()+uint64(r.GetSize())]...))
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Read{Read: &emulatorv1.ReadResponse{Data: data}}}

	case *emulatorv1.Request_Write:
		for _, w := range b.Write.GetChunks() {
			if _, _, e := s.checkRange(w.GetDomain(), w.GetAddress(), uint64(len(w.GetData())), true); e != nil {
				return errResp(e)
			}
		}
		for _, w := range b.Write.GetChunks() {
			data := append([]byte(nil), w.GetData()...)
			s.maskWrite(w.GetDomain(), w.GetAddress(), data)
			copy(s.mem[w.GetDomain()][w.GetAddress():], data)
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Write{Write: &emulatorv1.WriteResponse{}}}

	case *emulatorv1.Request_SaveState:
		if s.state == emulatorv1.Status_NO_ROM {
			return errResp(errorf(emulatorv1.Error_NO_ROM, "no ROM loaded"))
		}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(savestate{Magic: stateMagic, ROM: s.romPath, Frame: s.frame, Memory: s.mem}); err != nil {
			return errResp(errorf(emulatorv1.Error_FAILED, "encode savestate: %v", err))
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_SaveState{SaveState: &emulatorv1.SaveStateResponse{Data: buf.Bytes()}}}

	case *emulatorv1.Request_LoadState:
		if s.state == emulatorv1.Status_NO_ROM {
			return errResp(errorf(emulatorv1.Error_NO_ROM, "no ROM loaded"))
		}
		var st savestate
		if err := gob.NewDecoder(bytes.NewReader(b.LoadState.GetData())).Decode(&st); err != nil || st.Magic != stateMagic {
			return errResp(errorf(emulatorv1.Error_INVALID_ARGUMENT, "not a savestate"))
		}
		for _, d := range s.opts.Domains {
			if uint64(len(st.Memory[d.Name])) != d.Size {
				return errResp(errorf(emulatorv1.Error_INVALID_ARGUMENT, "savestate does not match domain %s", d.Name))
			}
		}
		// The frame counter is monotonic and not restored.
		for name, m := range st.Memory {
			copy(s.mem[name], m)
		}
		s.rewriteHard()
		return &emulatorv1.Response{Body: &emulatorv1.Response_LoadState{LoadState: &emulatorv1.LoadStateResponse{}}}

	case *emulatorv1.Request_LoadRom:
		path := b.LoadRom.GetPath()
		if path == "" || (s.opts.ROMExists != nil && !s.opts.ROMExists(path)) {
			return errResp(errorf(emulatorv1.Error_NOT_FOUND, "ROM %q not found", path))
		}
		s.loadROM(path)
		s.emitStatus()
		return &emulatorv1.Response{Body: &emulatorv1.Response_LoadRom{LoadRom: &emulatorv1.LoadRomResponse{Status: s.status()}}}

	case *emulatorv1.Request_CloseRom:
		if s.state != emulatorv1.Status_NO_ROM {
			s.state = emulatorv1.Status_NO_ROM
			s.romPath = ""
			s.mem = nil
			s.units = nil
			s.frame = 0
			s.emitStatus()
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_CloseRom{CloseRom: &emulatorv1.CloseRomResponse{}}}

	case *emulatorv1.Request_Reset_:
		if s.state == emulatorv1.Status_NO_ROM {
			return errResp(errorf(emulatorv1.Error_NO_ROM, "no ROM loaded"))
		}
		for _, m := range s.mem {
			clear(m)
		}
		s.frame = 0
		s.units = nil
		s.emitStatus()
		return &emulatorv1.Response{Body: &emulatorv1.Response_Reset_{Reset_: &emulatorv1.ResetResponse{}}}

	case *emulatorv1.Request_Pause:
		if e := s.setState(emulatorv1.Status_PAUSED); e != nil {
			return errResp(e)
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Pause{Pause: &emulatorv1.PauseResponse{}}}

	case *emulatorv1.Request_Resume:
		if e := s.setState(emulatorv1.Status_RUNNING); e != nil {
			return errResp(e)
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Resume{Resume: &emulatorv1.ResumeResponse{}}}

	case *emulatorv1.Request_Step:
		n := b.Step.GetFrames()
		if n == 0 {
			return errResp(errorf(emulatorv1.Error_INVALID_ARGUMENT, "frames must be positive"))
		}
		if e := s.setState(emulatorv1.Status_PAUSED); e != nil {
			return errResp(e)
		}
		for range n {
			s.runFrame()
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Step{Step: &emulatorv1.StepResponse{Frame: s.frame}}}

	case *emulatorv1.Request_ApplyUnits:
		if e := s.applyUnits(b.ApplyUnits.GetUnits()); e != nil {
			return errResp(e)
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_ApplyUnits{ApplyUnits: &emulatorv1.ApplyUnitsResponse{}}}

	case *emulatorv1.Request_RemoveUnits:
		s.removeUnits(b.RemoveUnits.GetIds())
		return &emulatorv1.Response{Body: &emulatorv1.Response_RemoveUnits{RemoveUnits: &emulatorv1.RemoveUnitsResponse{}}}

	case *emulatorv1.Request_ClearUnits:
		s.units = nil
		return &emulatorv1.Response{Body: &emulatorv1.Response_ClearUnits{ClearUnits: &emulatorv1.ClearUnitsResponse{}}}

	case *emulatorv1.Request_ListUnits:
		units := make([]*emulatorv1.Unit, 0, len(s.units))
		for _, u := range s.units {
			units = append(units, proto.Clone(u.spec).(*emulatorv1.Unit))
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_ListUnits{ListUnits: &emulatorv1.ListUnitsResponse{Units: units}}}

	case *emulatorv1.Request_SetInput:
		if b.SetInput.GetClear() {
			s.input = nil
		} else {
			s.input = proto.Clone(b.SetInput).(*emulatorv1.SetInputRequest)
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_SetInput{SetInput: &emulatorv1.SetInputResponse{}}}

	case *emulatorv1.Request_Screenshot:
		if s.state == emulatorv1.Status_NO_ROM {
			return errResp(errorf(emulatorv1.Error_NO_ROM, "no ROM loaded"))
		}
		return &emulatorv1.Response{Body: &emulatorv1.Response_Screenshot{Screenshot: &emulatorv1.ScreenshotResponse{
			Screens: []*emulatorv1.Image{s.screen(0), s.screen(1)},
		}}}

	case *emulatorv1.Request_Subscribe:
		c.interval = b.Subscribe.GetFrameInterval()
		return &emulatorv1.Response{Body: &emulatorv1.Response_Subscribe{Subscribe: &emulatorv1.SubscribeResponse{}}}

	case *emulatorv1.Request_Ping:
		return &emulatorv1.Response{Body: &emulatorv1.Response_Ping{Ping: &emulatorv1.PingResponse{}}}

	case *emulatorv1.Request_Quit:
		return &emulatorv1.Response{Body: &emulatorv1.Response_Quit{Quit: &emulatorv1.QuitResponse{}}}

	default:
		return errResp(errorf(emulatorv1.Error_UNSUPPORTED, "unsupported request %T", b))
	}
}

// setState switches between RUNNING and PAUSED. s.mu must be held.
func (s *Server) setState(state emulatorv1.Status_State) *emulatorv1.Error {
	if s.state == emulatorv1.Status_NO_ROM {
		return errorf(emulatorv1.Error_NO_ROM, "no ROM loaded")
	}
	if s.state != state {
		s.state = state
		s.emitStatus()
	}
	return nil
}

func (s *Server) screen(index int) *emulatorv1.Image {
	rgba := make([]byte, 4*ScreenWidth*ScreenHeight)
	for y := range ScreenHeight {
		for x := range ScreenWidth {
			p := rgba[4*(y*ScreenWidth+x):]
			p[0] = byte(x + int(s.frame))
			p[1] = byte(y)
			p[2] = byte(128 * index)
			p[3] = 0xff
		}
	}
	return &emulatorv1.Image{Width: ScreenWidth, Height: ScreenHeight, Rgba: rgba}
}

func errResp(e *emulatorv1.Error) *emulatorv1.Response {
	return &emulatorv1.Response{Body: &emulatorv1.Response_Error{Error: e}}
}
