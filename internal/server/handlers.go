package server

import (
	"context"

	"github.com/puhitaku/rtcv-ish/internal/server/gen"
)

// api implements gen.StrictServerInterface. Every method is a stub for now;
// errors returned here are mapped to Error JSON by writeError.
type api struct {
	s *Server
}

var _ gen.StrictServerInterface = (*api)(nil)

func (a *api) ManualBlast(ctx context.Context, request gen.ManualBlastRequestObject) (gen.ManualBlastResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ApplyLayer(ctx context.Context, request gen.ApplyLayerRequestObject) (gen.ApplyLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) RerollLayer(ctx context.Context, request gen.RerollLayerRequestObject) (gen.RerollLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ToggleLayer(ctx context.Context, request gen.ToggleLayerRequestObject) (gen.ToggleLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ClearUnits(ctx context.Context, request gen.ClearUnitsRequestObject) (gen.ClearUnitsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListUnits(ctx context.Context, request gen.ListUnitsRequestObject) (gen.ListUnitsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListDomains(ctx context.Context, request gen.ListDomainsRequestObject) (gen.ListDomainsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) AutoSelectDomains(ctx context.Context, request gen.AutoSelectDomainsRequestObject) (gen.AutoSelectDomainsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) SetSelectedDomains(ctx context.Context, request gen.SetSelectedDomainsRequestObject) (gen.SetSelectedDomainsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) CloseRom(ctx context.Context, request gen.CloseRomRequestObject) (gen.CloseRomResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ConnectEmulator(ctx context.Context, request gen.ConnectEmulatorRequestObject) (gen.ConnectEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) DisconnectEmulator(ctx context.Context, request gen.DisconnectEmulatorRequestObject) (gen.DisconnectEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) LaunchEmulator(ctx context.Context, request gen.LaunchEmulatorRequestObject) (gen.LaunchEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PauseEmulator(ctx context.Context, request gen.PauseEmulatorRequestObject) (gen.PauseEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) QuitEmulator(ctx context.Context, request gen.QuitEmulatorRequestObject) (gen.QuitEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ResetEmulator(ctx context.Context, request gen.ResetEmulatorRequestObject) (gen.ResetEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ResumeEmulator(ctx context.Context, request gen.ResumeEmulatorRequestObject) (gen.ResumeEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) LoadRom(ctx context.Context, request gen.LoadRomRequestObject) (gen.LoadRomResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) GetScreenshot(ctx context.Context, request gen.GetScreenshotRequestObject) (gen.GetScreenshotResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) StepEmulator(ctx context.Context, request gen.StepEmulatorRequestObject) (gen.StepEmulatorResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListEmulators(ctx context.Context, request gen.ListEmulatorsRequestObject) (gen.ListEmulatorsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListLists(ctx context.Context, request gen.ListListsRequestObject) (gen.ListListsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) UploadList(ctx context.Context, request gen.UploadListRequestObject) (gen.UploadListResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) DeleteList(ctx context.Context, request gen.DeleteListRequestObject) (gen.DeleteListResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ReadMemory(ctx context.Context, request gen.ReadMemoryRequestObject) (gen.ReadMemoryResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) WriteMemory(ctx context.Context, request gen.WriteMemoryRequestObject) (gen.WriteMemoryResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ProtectionBack(ctx context.Context, request gen.ProtectionBackRequestObject) (gen.ProtectionBackResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ProtectionBackup(ctx context.Context, request gen.ProtectionBackupRequestObject) (gen.ProtectionBackupResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListSavestates(ctx context.Context, request gen.ListSavestatesRequestObject) (gen.ListSavestatesResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) DeleteSavestate(ctx context.Context, request gen.DeleteSavestateRequestObject) (gen.DeleteSavestateResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PatchSavestate(ctx context.Context, request gen.PatchSavestateRequestObject) (gen.PatchSavestateResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) SaveSavestate(ctx context.Context, request gen.SaveSavestateRequestObject) (gen.SaveSavestateResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) LoadSavestate(ctx context.Context, request gen.LoadSavestateRequestObject) (gen.LoadSavestateResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) GetSettings(ctx context.Context, request gen.GetSettingsRequestObject) (gen.GetSettingsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PatchSettings(ctx context.Context, request gen.PatchSettingsRequestObject) (gen.PatchSettingsResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ClearStash(ctx context.Context, request gen.ClearStashRequestObject) (gen.ClearStashResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListStash(ctx context.Context, request gen.ListStashRequestObject) (gen.ListStashResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) CorruptStash(ctx context.Context, request gen.CorruptStashRequestObject) (gen.CorruptStashResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) InjectStash(ctx context.Context, request gen.InjectStashRequestObject) (gen.InjectStashResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) MergeStash(ctx context.Context, request gen.MergeStashRequestObject) (gen.MergeStashResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) DeleteStashKey(ctx context.Context, request gen.DeleteStashKeyRequestObject) (gen.DeleteStashKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PatchStashKey(ctx context.Context, request gen.PatchStashKeyRequestObject) (gen.PatchStashKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) GetStashLayer(ctx context.Context, request gen.GetStashLayerRequestObject) (gen.GetStashLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PutStashLayer(ctx context.Context, request gen.PutStashLayerRequestObject) (gen.PutStashLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) OriginalStashKey(ctx context.Context, request gen.OriginalStashKeyRequestObject) (gen.OriginalStashKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) RerollStashKey(ctx context.Context, request gen.RerollStashKeyRequestObject) (gen.RerollStashKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) RunStashKey(ctx context.Context, request gen.RunStashKeyRequestObject) (gen.RunStashKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) StashKeyToStockpile(ctx context.Context, request gen.StashKeyToStockpileRequestObject) (gen.StashKeyToStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) GetStatus(ctx context.Context, request gen.GetStatusRequestObject) (gen.GetStatusResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ClearStockpile(ctx context.Context, request gen.ClearStockpileRequestObject) (gen.ClearStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ListStockpile(ctx context.Context, request gen.ListStockpileRequestObject) (gen.ListStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ExportStockpile(ctx context.Context, request gen.ExportStockpileRequestObject) (gen.ExportStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ImportStockpile(ctx context.Context, request gen.ImportStockpileRequestObject) (gen.ImportStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) LoadStockpile(ctx context.Context, request gen.LoadStockpileRequestObject) (gen.LoadStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) ReorderStockpile(ctx context.Context, request gen.ReorderStockpileRequestObject) (gen.ReorderStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) SaveStockpile(ctx context.Context, request gen.SaveStockpileRequestObject) (gen.SaveStockpileResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) DeleteStockpileKey(ctx context.Context, request gen.DeleteStockpileKeyRequestObject) (gen.DeleteStockpileKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PatchStockpileKey(ctx context.Context, request gen.PatchStockpileKeyRequestObject) (gen.PatchStockpileKeyResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) GetStockpileLayer(ctx context.Context, request gen.GetStockpileLayerRequestObject) (gen.GetStockpileLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) PutStockpileLayer(ctx context.Context, request gen.PutStockpileLayerRequestObject) (gen.PutStockpileLayerResponseObject, error) {
	return nil, errNotImplemented
}

func (a *api) RunStockpileKey(ctx context.Context, request gen.RunStockpileKeyRequestObject) (gen.RunStockpileKeyResponseObject, error) {
	return nil, errNotImplemented
}
