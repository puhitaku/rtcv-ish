package emu

import (
	"context"
	"fmt"

	emulatorv1 "github.com/puhitaku/rtcv-ish/api/emulator/v1"
)

func (c *Client) Status(ctx context.Context) (*emulatorv1.Status, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_GetStatus{GetStatus: &emulatorv1.GetStatusRequest{}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetGetStatus())
	return r.GetStatus(), err
}

func (c *Client) ListDomains(ctx context.Context) ([]*emulatorv1.Domain, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_ListDomains{ListDomains: &emulatorv1.ListDomainsRequest{}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetListDomains())
	return r.GetDomains(), err
}

// Read returns one byte slice per range, in order.
func (c *Client) Read(ctx context.Context, ranges []*emulatorv1.Range) ([][]byte, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Read{Read: &emulatorv1.ReadRequest{Ranges: ranges}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetRead())
	if err != nil {
		return nil, err
	}
	if len(r.GetData()) != len(ranges) {
		return nil, fmt.Errorf("%w: read returned %d ranges, want %d", ErrProtocol, len(r.GetData()), len(ranges))
	}
	for i, d := range r.GetData() {
		if len(d) != int(ranges[i].GetSize()) {
			return nil, fmt.Errorf("%w: read range %d returned %d bytes, want %d", ErrProtocol, i, len(d), ranges[i].GetSize())
		}
	}
	return r.GetData(), nil
}

func (c *Client) ReadOne(ctx context.Context, domain string, addr uint64, size uint32) ([]byte, error) {
	data, err := c.Read(ctx, []*emulatorv1.Range{{Domain: domain, Address: addr, Size: size}})
	if err != nil {
		return nil, err
	}
	return data[0], nil
}

func (c *Client) Write(ctx context.Context, chunks []*emulatorv1.WriteChunk) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Write{Write: &emulatorv1.WriteRequest{Chunks: chunks}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetWrite())
	return err
}

func (c *Client) WriteOne(ctx context.Context, domain string, addr uint64, data []byte) error {
	return c.Write(ctx, []*emulatorv1.WriteChunk{{Domain: domain, Address: addr, Data: data}})
}

func (c *Client) SaveState(ctx context.Context) ([]byte, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_SaveState{SaveState: &emulatorv1.SaveStateRequest{}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetSaveState())
	return r.GetData(), err
}

func (c *Client) LoadState(ctx context.Context, data []byte) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_LoadState{LoadState: &emulatorv1.LoadStateRequest{Data: data}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetLoadState())
	return err
}

// LoadRom loads a ROM by its path on the emulator host and starts it.
func (c *Client) LoadRom(ctx context.Context, path string) (*emulatorv1.Status, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_LoadRom{LoadRom: &emulatorv1.LoadRomRequest{Path: path}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetLoadRom())
	return r.GetStatus(), err
}

func (c *Client) CloseRom(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_CloseRom{CloseRom: &emulatorv1.CloseRomRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetCloseRom())
	return err
}

func (c *Client) Reset(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Reset_{Reset_: &emulatorv1.ResetRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetReset_())
	return err
}

func (c *Client) Pause(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Pause{Pause: &emulatorv1.PauseRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetPause())
	return err
}

func (c *Client) Resume(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Resume{Resume: &emulatorv1.ResumeRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetResume())
	return err
}

// Step pauses, emulates n frames and returns the frame counter.
func (c *Client) Step(ctx context.Context, n uint32) (uint64, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Step{Step: &emulatorv1.StepRequest{Frames: n}}})
	if err != nil {
		return 0, err
	}
	r, err := expect(resp, resp.GetStep())
	return r.GetFrame(), err
}

func (c *Client) ApplyUnits(ctx context.Context, units []*emulatorv1.Unit) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_ApplyUnits{ApplyUnits: &emulatorv1.ApplyUnitsRequest{Units: units}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetApplyUnits())
	return err
}

func (c *Client) RemoveUnits(ctx context.Context, ids []uint64) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_RemoveUnits{RemoveUnits: &emulatorv1.RemoveUnitsRequest{Ids: ids}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetRemoveUnits())
	return err
}

func (c *Client) ClearUnits(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_ClearUnits{ClearUnits: &emulatorv1.ClearUnitsRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetClearUnits())
	return err
}

func (c *Client) ListUnits(ctx context.Context) ([]*emulatorv1.Unit, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_ListUnits{ListUnits: &emulatorv1.ListUnitsRequest{}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetListUnits())
	return r.GetUnits(), err
}

func (c *Client) SetInput(ctx context.Context, in *emulatorv1.SetInputRequest) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_SetInput{SetInput: in}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetSetInput())
	return err
}

func (c *Client) Screenshot(ctx context.Context) ([]*emulatorv1.Image, error) {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Screenshot{Screenshot: &emulatorv1.ScreenshotRequest{}}})
	if err != nil {
		return nil, err
	}
	r, err := expect(resp, resp.GetScreenshot())
	return r.GetScreens(), err
}

// Subscribe asks for a FrameEvent every interval frames; 0 disables them.
func (c *Client) Subscribe(ctx context.Context, interval uint32) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Subscribe{Subscribe: &emulatorv1.SubscribeRequest{FrameInterval: interval}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetSubscribe())
	return err
}

func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Ping{Ping: &emulatorv1.PingRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetPing())
	return err
}

// Quit asks the emulator process to exit. The connection closes afterwards.
func (c *Client) Quit(ctx context.Context) error {
	resp, err := c.call(ctx, &emulatorv1.Request{Body: &emulatorv1.Request_Quit{Quit: &emulatorv1.QuitRequest{}}})
	if err != nil {
		return err
	}
	_, err = expect(resp, resp.GetQuit())
	return err
}
