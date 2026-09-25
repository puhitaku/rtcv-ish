package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/puhitaku/rtcv-ish/internal/corrupt"
	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// maxUpload bounds uploaded files (.sks, lists).
const maxUpload = 1 << 30

// api implements gen.StrictServerInterface on top of the session. Errors
// returned here are mapped to Error JSON by writeError.
type api struct {
	s *Server
}

var _ gen.StrictServerInterface = (*api)(nil)

// conv converts between session and API types through their JSON forms,
// which follow the same schema.
func conv[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(b, &out)
}

// respond converts v to the API type T and wraps it with f.
func respond[T, R any](v any, err error, f func(T) R) (R, error) {
	var zero R
	if err != nil {
		return zero, err
	}
	out, err := conv[T](v)
	if err != nil {
		return zero, err
	}
	return f(out), nil
}

func toLayer(l gen.Layer) (*corrupt.Layer, error) {
	out, err := conv[corrupt.Layer](l)
	if err != nil {
		return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "layer: %v", err)
	}
	return &out, nil
}

// ---- Status and emulator ----

func (a *api) GetStatus(ctx context.Context, _ gen.GetStatusRequestObject) (gen.GetStatusResponseObject, error) {
	return respond(a.s.sess.Status(), nil, func(v gen.Status) gen.GetStatusResponseObject { return gen.GetStatus200JSONResponse(v) })
}

func (a *api) ListEmulators(ctx context.Context, _ gen.ListEmulatorsRequestObject) (gen.ListEmulatorsResponseObject, error) {
	return respond(a.s.sess.Emulators(), nil, func(v []gen.BundledEmulator) gen.ListEmulatorsResponseObject {
		return gen.ListEmulators200JSONResponse(v)
	})
}

func (a *api) ConnectEmulator(ctx context.Context, r gen.ConnectEmulatorRequestObject) (gen.ConnectEmulatorResponseObject, error) {
	st, err := a.s.sess.Connect(ctx, r.Body.Address)
	return respond(st, err, func(v gen.Status) gen.ConnectEmulatorResponseObject { return gen.ConnectEmulator200JSONResponse(v) })
}

func (a *api) LaunchEmulator(ctx context.Context, r gen.LaunchEmulatorRequestObject) (gen.LaunchEmulatorResponseObject, error) {
	rom := ""
	if r.Body.Rom != nil {
		rom = *r.Body.Rom
	}
	st, err := a.s.sess.Launch(ctx, r.Body.Name, rom)
	return respond(st, err, func(v gen.Status) gen.LaunchEmulatorResponseObject { return gen.LaunchEmulator200JSONResponse(v) })
}

func (a *api) DisconnectEmulator(ctx context.Context, _ gen.DisconnectEmulatorRequestObject) (gen.DisconnectEmulatorResponseObject, error) {
	return respond(a.s.sess.Disconnect(), nil, func(v gen.Status) gen.DisconnectEmulatorResponseObject {
		return gen.DisconnectEmulator200JSONResponse(v)
	})
}

func (a *api) LoadRom(ctx context.Context, r gen.LoadRomRequestObject) (gen.LoadRomResponseObject, error) {
	gs, err := a.s.sess.LoadRom(ctx, r.Body.Path)
	return respond(gs, err, func(v gen.GameStatus) gen.LoadRomResponseObject { return gen.LoadRom200JSONResponse(v) })
}

func (a *api) CloseRom(ctx context.Context, _ gen.CloseRomRequestObject) (gen.CloseRomResponseObject, error) {
	gs, err := a.s.sess.CloseRom(ctx)
	return respond(gs, err, func(v gen.GameStatus) gen.CloseRomResponseObject { return gen.CloseRom200JSONResponse(v) })
}

func (a *api) PauseEmulator(ctx context.Context, _ gen.PauseEmulatorRequestObject) (gen.PauseEmulatorResponseObject, error) {
	gs, err := a.s.sess.Pause(ctx)
	return respond(gs, err, func(v gen.GameStatus) gen.PauseEmulatorResponseObject { return gen.PauseEmulator200JSONResponse(v) })
}

func (a *api) ResumeEmulator(ctx context.Context, _ gen.ResumeEmulatorRequestObject) (gen.ResumeEmulatorResponseObject, error) {
	gs, err := a.s.sess.Resume(ctx)
	return respond(gs, err, func(v gen.GameStatus) gen.ResumeEmulatorResponseObject { return gen.ResumeEmulator200JSONResponse(v) })
}

func (a *api) ResetEmulator(ctx context.Context, _ gen.ResetEmulatorRequestObject) (gen.ResetEmulatorResponseObject, error) {
	gs, err := a.s.sess.Reset(ctx)
	return respond(gs, err, func(v gen.GameStatus) gen.ResetEmulatorResponseObject { return gen.ResetEmulator200JSONResponse(v) })
}

func (a *api) StepEmulator(ctx context.Context, r gen.StepEmulatorRequestObject) (gen.StepEmulatorResponseObject, error) {
	gs, err := a.s.sess.Step(ctx, r.Body.Frames)
	return respond(gs, err, func(v gen.GameStatus) gen.StepEmulatorResponseObject { return gen.StepEmulator200JSONResponse(v) })
}

func (a *api) QuitEmulator(ctx context.Context, _ gen.QuitEmulatorRequestObject) (gen.QuitEmulatorResponseObject, error) {
	if err := a.s.sess.Quit(ctx); err != nil {
		return nil, err
	}
	return gen.QuitEmulator204Response{}, nil
}

func (a *api) GetScreenshot(ctx context.Context, _ gen.GetScreenshotRequestObject) (gen.GetScreenshotResponseObject, error) {
	b, err := a.s.sess.Screenshot(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetScreenshot200ImagepngResponse{Body: bytes.NewReader(b), ContentLength: int64(len(b))}, nil
}

// ---- Domains and memory ----

func (a *api) ListDomains(ctx context.Context, _ gen.ListDomainsRequestObject) (gen.ListDomainsResponseObject, error) {
	ds, err := a.s.sess.Domains()
	return respond(ds, err, func(v []gen.Domain) gen.ListDomainsResponseObject { return gen.ListDomains200JSONResponse(v) })
}

func (a *api) SetSelectedDomains(ctx context.Context, r gen.SetSelectedDomainsRequestObject) (gen.SetSelectedDomainsResponseObject, error) {
	ds, err := a.s.sess.SelectDomains(r.Body.Names)
	return respond(ds, err, func(v []gen.Domain) gen.SetSelectedDomainsResponseObject {
		return gen.SetSelectedDomains200JSONResponse(v)
	})
}

func (a *api) AutoSelectDomains(ctx context.Context, _ gen.AutoSelectDomainsRequestObject) (gen.AutoSelectDomainsResponseObject, error) {
	ds, err := a.s.sess.AutoSelectDomains()
	return respond(ds, err, func(v []gen.Domain) gen.AutoSelectDomainsResponseObject {
		return gen.AutoSelectDomains200JSONResponse(v)
	})
}

func (a *api) ReadMemory(ctx context.Context, r gen.ReadMemoryRequestObject) (gen.ReadMemoryResponseObject, error) {
	if r.Params.Address < 0 || r.Params.Size < 1 || r.Params.Size > 64<<10 {
		return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "address must be >= 0 and size in 1..65536")
	}
	b, err := a.s.sess.ReadMemory(ctx, r.Domain, uint64(r.Params.Address), int(r.Params.Size))
	if err != nil {
		return nil, err
	}
	return gen.ReadMemory200JSONResponse{Domain: r.Domain, Address: r.Params.Address, Data: hex.EncodeToString(b)}, nil
}

func (a *api) WriteMemory(ctx context.Context, r gen.WriteMemoryRequestObject) (gen.WriteMemoryResponseObject, error) {
	data, err := hex.DecodeString(r.Body.Data)
	if err != nil || len(data) == 0 || len(data) > 64<<10 || r.Body.Address < 0 {
		return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "data must be 1..65536 bytes of hex and address >= 0")
	}
	if err := a.s.sess.WriteMemory(ctx, r.Domain, uint64(r.Body.Address), data); err != nil {
		return nil, err
	}
	return gen.WriteMemory204Response{}, nil
}

// ---- Settings ----

func (a *api) GetSettings(ctx context.Context, _ gen.GetSettingsRequestObject) (gen.GetSettingsResponseObject, error) {
	return respond(a.s.sess.Settings(), nil, func(v gen.Settings) gen.GetSettingsResponseObject { return gen.GetSettings200JSONResponse(v) })
}

func (a *api) PatchSettings(ctx context.Context, r gen.PatchSettingsRequestObject) (gen.PatchSettingsResponseObject, error) {
	patch, err := json.Marshal(r.Body)
	if err != nil {
		return nil, err
	}
	st, err := a.s.sess.PatchSettings(patch)
	return respond(st, err, func(v gen.Settings) gen.PatchSettingsResponseObject { return gen.PatchSettings200JSONResponse(v) })
}

// ---- Blasting ----

func (a *api) ManualBlast(ctx context.Context, _ gen.ManualBlastRequestObject) (gen.ManualBlastResponseObject, error) {
	l, err := a.s.sess.Blast(ctx)
	return respond(l, err, func(v gen.Layer) gen.ManualBlastResponseObject { return gen.ManualBlast200JSONResponse(v) })
}

func (a *api) ApplyLayer(ctx context.Context, r gen.ApplyLayerRequestObject) (gen.ApplyLayerResponseObject, error) {
	l, err := toLayer(r.Body.Layer)
	if err != nil {
		return nil, err
	}
	if err := a.s.sess.ApplyLayer(ctx, l, r.Body.Backup); err != nil {
		return nil, err
	}
	return gen.ApplyLayer204Response{}, nil
}

func (a *api) ListUnits(ctx context.Context, _ gen.ListUnitsRequestObject) (gen.ListUnitsResponseObject, error) {
	us, err := a.s.sess.Units(ctx)
	return respond(us, err, func(v []gen.EmuUnit) gen.ListUnitsResponseObject { return gen.ListUnits200JSONResponse(v) })
}

func (a *api) ClearUnits(ctx context.Context, _ gen.ClearUnitsRequestObject) (gen.ClearUnitsResponseObject, error) {
	if err := a.s.sess.ClearUnits(ctx); err != nil {
		return nil, err
	}
	return gen.ClearUnits204Response{}, nil
}

func (a *api) ToggleLayer(ctx context.Context, r gen.ToggleLayerRequestObject) (gen.ToggleLayerResponseObject, error) {
	if err := a.s.sess.Toggle(ctx, r.Body.On); err != nil {
		return nil, err
	}
	return gen.ToggleLayer204Response{}, nil
}

func (a *api) RerollLayer(ctx context.Context, r gen.RerollLayerRequestObject) (gen.RerollLayerResponseObject, error) {
	l, err := toLayer(r.Body.Layer)
	if err != nil {
		return nil, err
	}
	out, err := a.s.sess.Reroll(l)
	return respond(out, err, func(v gen.Layer) gen.RerollLayerResponseObject { return gen.RerollLayer200JSONResponse(v) })
}

// ---- Savestates ----

func (a *api) ListSavestates(ctx context.Context, _ gen.ListSavestatesRequestObject) (gen.ListSavestatesResponseObject, error) {
	return respond(a.s.sess.Savestates(), nil, func(v []gen.SavestateSlot) gen.ListSavestatesResponseObject {
		return gen.ListSavestates200JSONResponse(v)
	})
}

func (a *api) SaveSavestate(ctx context.Context, r gen.SaveSavestateRequestObject) (gen.SaveSavestateResponseObject, error) {
	si, err := a.s.sess.SaveSlot(ctx, r.Slot)
	return respond(si, err, func(v gen.SavestateSlot) gen.SaveSavestateResponseObject { return gen.SaveSavestate200JSONResponse(v) })
}

func (a *api) LoadSavestate(ctx context.Context, r gen.LoadSavestateRequestObject) (gen.LoadSavestateResponseObject, error) {
	if err := a.s.sess.LoadSlot(ctx, r.Slot); err != nil {
		return nil, err
	}
	return gen.LoadSavestate204Response{}, nil
}

func (a *api) PatchSavestate(ctx context.Context, r gen.PatchSavestateRequestObject) (gen.PatchSavestateResponseObject, error) {
	si, err := a.s.sess.LabelSlot(r.Slot, r.Body.Label)
	return respond(si, err, func(v gen.SavestateSlot) gen.PatchSavestateResponseObject {
		return gen.PatchSavestate200JSONResponse(v)
	})
}

func (a *api) DeleteSavestate(ctx context.Context, r gen.DeleteSavestateRequestObject) (gen.DeleteSavestateResponseObject, error) {
	if err := a.s.sess.ClearSlot(r.Slot); err != nil {
		return nil, err
	}
	return gen.DeleteSavestate204Response{}, nil
}

// ---- Stash ----

func (a *api) ListStash(ctx context.Context, _ gen.ListStashRequestObject) (gen.ListStashResponseObject, error) {
	return respond(a.s.sess.Stash(), nil, func(v []gen.StashKey) gen.ListStashResponseObject { return gen.ListStash200JSONResponse(v) })
}

func (a *api) ClearStash(ctx context.Context, _ gen.ClearStashRequestObject) (gen.ClearStashResponseObject, error) {
	a.s.sess.ClearKeys(false)
	return gen.ClearStash204Response{}, nil
}

func (a *api) CorruptStash(ctx context.Context, r gen.CorruptStashRequestObject) (gen.CorruptStashResponseObject, error) {
	k, err := a.s.sess.Corrupt(ctx, r.Body.Slot, r.Body.LoadBefore)
	return respond(k, err, func(v gen.StashKey) gen.CorruptStashResponseObject { return gen.CorruptStash200JSONResponse(v) })
}

func (a *api) InjectStash(ctx context.Context, r gen.InjectStashRequestObject) (gen.InjectStashResponseObject, error) {
	k, err := a.s.sess.Inject(ctx, r.Body.Key, r.Body.Slot)
	return respond(k, err, func(v gen.StashKey) gen.InjectStashResponseObject { return gen.InjectStash200JSONResponse(v) })
}

func (a *api) MergeStash(ctx context.Context, r gen.MergeStashRequestObject) (gen.MergeStashResponseObject, error) {
	k, err := a.s.sess.Merge(ctx, r.Body.Keys)
	return respond(k, err, func(v gen.StashKey) gen.MergeStashResponseObject { return gen.MergeStash200JSONResponse(v) })
}

func (a *api) PatchStashKey(ctx context.Context, r gen.PatchStashKeyRequestObject) (gen.PatchStashKeyResponseObject, error) {
	k, err := a.s.sess.PatchKey(false, r.Key, r.Body.Alias, r.Body.Note)
	return respond(k, err, func(v gen.StashKey) gen.PatchStashKeyResponseObject { return gen.PatchStashKey200JSONResponse(v) })
}

func (a *api) DeleteStashKey(ctx context.Context, r gen.DeleteStashKeyRequestObject) (gen.DeleteStashKeyResponseObject, error) {
	if err := a.s.sess.DeleteKey(false, r.Key); err != nil {
		return nil, err
	}
	return gen.DeleteStashKey204Response{}, nil
}

func (a *api) RunStashKey(ctx context.Context, r gen.RunStashKeyRequestObject) (gen.RunStashKeyResponseObject, error) {
	if err := a.s.sess.Run(ctx, false, r.Key); err != nil {
		return nil, err
	}
	return gen.RunStashKey204Response{}, nil
}

func (a *api) OriginalStashKey(ctx context.Context, r gen.OriginalStashKeyRequestObject) (gen.OriginalStashKeyResponseObject, error) {
	if err := a.s.sess.Original(ctx, r.Key); err != nil {
		return nil, err
	}
	return gen.OriginalStashKey204Response{}, nil
}

func (a *api) RerollStashKey(ctx context.Context, r gen.RerollStashKeyRequestObject) (gen.RerollStashKeyResponseObject, error) {
	k, err := a.s.sess.RerollKey(ctx, r.Key)
	return respond(k, err, func(v gen.StashKey) gen.RerollStashKeyResponseObject { return gen.RerollStashKey200JSONResponse(v) })
}

func (a *api) StashKeyToStockpile(ctx context.Context, r gen.StashKeyToStockpileRequestObject) (gen.StashKeyToStockpileResponseObject, error) {
	var alias *string
	if r.Body != nil {
		alias = r.Body.Alias
	}
	k, err := a.s.sess.ToStockpile(r.Key, alias)
	return respond(k, err, func(v gen.StashKey) gen.StashKeyToStockpileResponseObject {
		return gen.StashKeyToStockpile200JSONResponse(v)
	})
}

func (a *api) GetStashLayer(ctx context.Context, r gen.GetStashLayerRequestObject) (gen.GetStashLayerResponseObject, error) {
	l, err := a.s.sess.KeyLayer(false, r.Key)
	return respond(l, err, func(v gen.Layer) gen.GetStashLayerResponseObject { return gen.GetStashLayer200JSONResponse(v) })
}

func (a *api) PutStashLayer(ctx context.Context, r gen.PutStashLayerRequestObject) (gen.PutStashLayerResponseObject, error) {
	l, err := toLayer(*r.Body)
	if err != nil {
		return nil, err
	}
	out, err := a.s.sess.SetKeyLayer(false, r.Key, l)
	return respond(out, err, func(v gen.Layer) gen.PutStashLayerResponseObject { return gen.PutStashLayer200JSONResponse(v) })
}

// ---- Stockpile ----

func (a *api) ListStockpile(ctx context.Context, _ gen.ListStockpileRequestObject) (gen.ListStockpileResponseObject, error) {
	return respond(a.s.sess.Stockpile(), nil, func(v []gen.StashKey) gen.ListStockpileResponseObject {
		return gen.ListStockpile200JSONResponse(v)
	})
}

func (a *api) ClearStockpile(ctx context.Context, _ gen.ClearStockpileRequestObject) (gen.ClearStockpileResponseObject, error) {
	a.s.sess.ClearKeys(true)
	return gen.ClearStockpile204Response{}, nil
}

func (a *api) PatchStockpileKey(ctx context.Context, r gen.PatchStockpileKeyRequestObject) (gen.PatchStockpileKeyResponseObject, error) {
	k, err := a.s.sess.PatchKey(true, r.Key, r.Body.Alias, r.Body.Note)
	return respond(k, err, func(v gen.StashKey) gen.PatchStockpileKeyResponseObject {
		return gen.PatchStockpileKey200JSONResponse(v)
	})
}

func (a *api) DeleteStockpileKey(ctx context.Context, r gen.DeleteStockpileKeyRequestObject) (gen.DeleteStockpileKeyResponseObject, error) {
	if err := a.s.sess.DeleteKey(true, r.Key); err != nil {
		return nil, err
	}
	return gen.DeleteStockpileKey204Response{}, nil
}

func (a *api) RunStockpileKey(ctx context.Context, r gen.RunStockpileKeyRequestObject) (gen.RunStockpileKeyResponseObject, error) {
	if err := a.s.sess.Run(ctx, true, r.Key); err != nil {
		return nil, err
	}
	return gen.RunStockpileKey204Response{}, nil
}

func (a *api) GetStockpileLayer(ctx context.Context, r gen.GetStockpileLayerRequestObject) (gen.GetStockpileLayerResponseObject, error) {
	l, err := a.s.sess.KeyLayer(true, r.Key)
	return respond(l, err, func(v gen.Layer) gen.GetStockpileLayerResponseObject {
		return gen.GetStockpileLayer200JSONResponse(v)
	})
}

func (a *api) PutStockpileLayer(ctx context.Context, r gen.PutStockpileLayerRequestObject) (gen.PutStockpileLayerResponseObject, error) {
	l, err := toLayer(*r.Body)
	if err != nil {
		return nil, err
	}
	out, err := a.s.sess.SetKeyLayer(true, r.Key, l)
	return respond(out, err, func(v gen.Layer) gen.PutStockpileLayerResponseObject {
		return gen.PutStockpileLayer200JSONResponse(v)
	})
}

func (a *api) ReorderStockpile(ctx context.Context, r gen.ReorderStockpileRequestObject) (gen.ReorderStockpileResponseObject, error) {
	ks, err := a.s.sess.Reorder(r.Body.Keys)
	return respond(ks, err, func(v []gen.StashKey) gen.ReorderStockpileResponseObject {
		return gen.ReorderStockpile200JSONResponse(v)
	})
}

func (a *api) ExportStockpile(ctx context.Context, _ gen.ExportStockpileRequestObject) (gen.ExportStockpileResponseObject, error) {
	b, err := a.s.sess.ExportStockpile()
	if err != nil {
		return nil, err
	}
	cd := `attachment; filename="stockpile.sks"`
	return gen.ExportStockpile200ApplicationzipResponse{
		Body:          bytes.NewReader(b),
		ContentLength: int64(len(b)),
		Headers:       gen.ExportStockpile200ResponseHeaders{ContentDisposition: &cd},
	}, nil
}

func (a *api) ImportStockpile(ctx context.Context, r gen.ImportStockpileRequestObject) (gen.ImportStockpileResponseObject, error) {
	form, err := readForm(r.Body)
	if err != nil {
		return nil, err
	}
	merge := r.Params.Merge != nil && *r.Params.Merge
	ks, err := a.s.sess.ImportStockpile(form.file, merge)
	return respond(ks, err, func(v []gen.StashKey) gen.ImportStockpileResponseObject {
		return gen.ImportStockpile200JSONResponse(v)
	})
}

func (a *api) SaveStockpile(ctx context.Context, r gen.SaveStockpileRequestObject) (gen.SaveStockpileResponseObject, error) {
	if err := a.s.sess.SaveStockpile(r.Body.Path); err != nil {
		return nil, err
	}
	return gen.SaveStockpile204Response{}, nil
}

func (a *api) LoadStockpile(ctx context.Context, r gen.LoadStockpileRequestObject) (gen.LoadStockpileResponseObject, error) {
	ks, err := a.s.sess.LoadStockpile(r.Body.Path)
	return respond(ks, err, func(v []gen.StashKey) gen.LoadStockpileResponseObject { return gen.LoadStockpile200JSONResponse(v) })
}

// ---- Game protection ----

func (a *api) ProtectionBackup(ctx context.Context, _ gen.ProtectionBackupRequestObject) (gen.ProtectionBackupResponseObject, error) {
	if err := a.s.sess.ProtectionBackup(ctx); err != nil {
		return nil, err
	}
	return gen.ProtectionBackup204Response{}, nil
}

func (a *api) ProtectionBack(ctx context.Context, _ gen.ProtectionBackRequestObject) (gen.ProtectionBackResponseObject, error) {
	if err := a.s.sess.ProtectionBack(ctx); err != nil {
		return nil, err
	}
	return gen.ProtectionBack204Response{}, nil
}

func (a *api) ProtectionLast(ctx context.Context, _ gen.ProtectionLastRequestObject) (gen.ProtectionLastResponseObject, error) {
	if err := a.s.sess.ProtectionLast(ctx); err != nil {
		return nil, err
	}
	return gen.ProtectionLast204Response{}, nil
}

// ---- Lists ----

func (a *api) ListLists(ctx context.Context, _ gen.ListListsRequestObject) (gen.ListListsResponseObject, error) {
	return respond(a.s.sess.Lists(), nil, func(v []gen.ListInfo) gen.ListListsResponseObject { return gen.ListLists200JSONResponse(v) })
}

func (a *api) UploadList(ctx context.Context, r gen.UploadListRequestObject) (gen.UploadListResponseObject, error) {
	form, err := readForm(r.Body)
	if err != nil {
		return nil, err
	}
	name := form.fields["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(form.filename), filepath.Ext(form.filename))
	}
	li, err := a.s.sess.UploadList(name, form.file)
	return respond(li, err, func(v gen.ListInfo) gen.UploadListResponseObject { return gen.UploadList200JSONResponse(v) })
}

func (a *api) DeleteList(ctx context.Context, r gen.DeleteListRequestObject) (gen.DeleteListResponseObject, error) {
	if err := a.s.sess.DeleteList(r.Name); err != nil {
		return nil, err
	}
	return gen.DeleteList204Response{}, nil
}

type form struct {
	file     []byte
	filename string
	hasFile  bool
	fields   map[string]string
}

// readForm reads a multipart body with a "file" part and small fields.
func readForm(mr *multipart.Reader) (*form, error) {
	f := &form{fields: map[string]string{}}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "multipart body: %v", err)
		}
		limit := int64(64 << 10)
		if p.FormName() == "file" {
			limit = maxUpload
		}
		b, err := io.ReadAll(io.LimitReader(p, limit+1))
		p.Close()
		if err != nil {
			return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "multipart body: %v", err)
		}
		if int64(len(b)) > limit {
			return nil, newError(http.StatusRequestEntityTooLarge, CodeInvalidArgument, "part %q is too large", p.FormName())
		}
		if p.FormName() == "file" {
			f.file, f.filename, f.hasFile = b, p.FileName(), true
		} else {
			f.fields[p.FormName()] = string(b)
		}
	}
	if !f.hasFile {
		return nil, newError(http.StatusBadRequest, CodeInvalidArgument, "no file part")
	}
	return f, nil
}
