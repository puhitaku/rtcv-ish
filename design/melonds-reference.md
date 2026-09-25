# melonDS reference notes: RTCV Vanguard fork vs current upstream fork

Scope: what the RTCV Vanguard fork of melonDS did (A), and where to hook an IP-based RPC server into the current upstream-based fork (B).

- A = `references/melonds50x-vanguard` (melonDS 0.8.3, libui/SDL frontend, `MELONDS_VERSION "0.8.3-Vanguard"` in `src/version.h`). Last commit 2021-05-05.
- B = `emulators/melonds` (current upstream, Qt6/SDL2 frontend in `src/frontend/qt_sdl`, HEAD `906e9ebb`).

Note: in both trees, `src/RTC.cpp` is the DS real-time clock. It has nothing to do with RTCV.

---

## Part 1: Vanguard fork (A)

### 1.1 Files added or modified for Vanguard

Commits by NarryG / Dan B / Scott Walters (`git log --author=...`). Files changed:

| File | Change |
|---|---|
| `src/Vanguard/VanguardClient.cpp` (999 lines, new) | The entire integration. It is C++/CLI (`/clr`) that references RTCV .NET assemblies. It contains the managed `ref class VanguardClient` (NetCore receiver/connector, spec registration, message handling, savestate/ROM commands), the memory-domain `ref class`es, `VanguardClientInitializer`, and the unmanaged hook bodies (`VanguardClientUnmanaged::*`). |
| `src/Vanguard/VanguardClient.h` (new) | Unmanaged facade `class VanguardClientUnmanaged`: `CORE_STEP()`, `LOAD_GAME_START(std::string)`, `LOAD_GAME_DONE()`, `GAME_CLOSED()`, `CLOSE_GAME()` (declared, no body), `static int GAME_NAME`, `InvokeEmuThread()` (declared, no body), `RTC_OSD_ENABLED()`. Native code includes only this header. |
| `src/Vanguard/VanguardClientInitializer.h` (new) | `VanguardClientInitializer::Initialize()`, `StartVanguardClient()`, `ConfigureVisualStyles()`. |
| `src/Vanguard/Helpers.hpp` (new) | UTF-8 `std::string` <-> `System::String^` conversions, plus `Helpers::is<T>()` (the C# `is` operator). |
| `src/Vanguard/MelonMemoryDomains.{h,cpp}` | Added, then deleted. They are still listed in the `file(GLOB CLR_FILES ...)` in `src/CMakeLists.txt`. The domains were moved into `VanguardClient.cpp` ("if we do these in another class, melon won't build"). |
| `src/NDS.cpp` | `#include "Vanguard/VanguardClient.h"`. Adds `VanguardClientUnmanaged::CORE_STEP()` near the top of `NDS::RunFrame()` (line ~786). Changes `ARM9Write8` so that byte writes to `0x06xxxxxx` VRAM work (upstream ignored them): `GPU::WriteVRAM_{ABG,BBG,AOBJ,BOBJ,LCDC}<u8>`. **Side effect:** `case 0x05000000` now falls through into that VRAM switch, so ARM9 byte writes to palette go to VRAM. Commit `28d0c3a` also leaves a bug in `ARM9Read8`: `return *(u8*)3000000&SWRAM_ARM9[...]` dereferences absolute address 3000000 on any ARM9 8-bit shared-WRAM read. |
| `src/NDS.h` | Exposes `extern u8 SharedWRAM[0x8000]; extern u8 ARM7WRAM[0x10000];` for the WRAM domains. |
| `src/NDSCart.cpp` / `.h` | `LoadROM()` sets `VanguardClientUnmanaged::GAME_NAME = gamecode;`. Exposes `extern bool CartInserted`. |
| `src/libui_sdl/main.cpp` | Makes `EmuRunning` / `EmuStatus` non-static globals. Adds hooks in `TryLoadROM()` (`LOAD_GAME_START` before `NDS::LoadROM`, `LOAD_GAME_DONE` on success and on failure) and `Stop()` (`GAME_CLOSED`). Adds `Main::LoadState(const char*, bool resumeAfter=true)` and `Main::SaveState(const char*)` for file-path savestates. Calls `VanguardClientInitializer::Initialize()` in `main()` right after `CreateMainWindow()` (moved there to fix a race). |
| `src/libui_sdl/main.h` (new) | "RTC_Hijack" header that exports `Main`, `EmuRunning`, `EmuStatus`, `TryLoadROM`, `Stop`, `CloseAllDialogs`, `ScreenRotation`, `MainWindow`, `OnSetScreenRotation`, `ApplyNewSettings`. |
| `src/libui_sdl/OSD.cpp` | `RenderText()` returns early when `!VanguardClientUnmanaged::RTC_OSD_ENABLED()`. |
| `src/libui_sdl/libui/windows/window.cpp` | WndProc ignores `WM_COMMAND` with `LOWORD(wParam) <= IDCANCEL`. Related to window-position handling. |
| `src/OpenGLSupport.h`, `src/GPU2D.h`, `src/libui_sdl/{DlgInputConfig,LAN_PCap,OSD,Platform}.cpp` | MSVC portability only: `alignas` instead of `__attribute__`, `<GL/gl.h>` + bundled `thirdparty/gl/glext.h`, local `"SDL2/SDL.h"` includes. |
| `CMakeLists.txt`, `src/CMakeLists.txt`, `src/libui_sdl/CMakeLists.txt`, `CMakeSettings.json`, libui CMake files | MSVC-only build. Details in 1.6. |
| `src/thirdparty/sdl/SDL2.lib`, `src/libui_sdl/SDL2/*.h`, `src/thirdparty/gl/*` | Vendored Windows SDL2 and GL headers. |
| `.github/workflows/build.yml` | windows-2019 build that first builds RTCV (`redscientistlabs/RTCV@506v2`), then builds melonDS with msbuild. |

**Summary of `VanguardClient.cpp`:**
- **Spec.** `getDefaultPartial()` sets `NAME="melonDS"`, `SUPPORTS_SAVESTATES`, `SUPPORTS_REALTIME`, `SUPPORTS_KILLSWITCH`, `SUPPORTS_CONFIG_MANAGEMENT/HANDOFF`, `SUPPORTS_REFERENCES`, `SUPPORTS_MIXED_STOCKPILE`, `SUPPORTS_RENDERING=false`, `OVERRIDE_DEFAULTMAXINTENSITY=100000`, `LOADSTATE_USES_CALLBACKS=true`, `CONFIG_PATHS={<emuDir>/melonDS.ini}`, `EMUDIR`, and `MEMORYDOMAINS_BLACKLISTEDDOMAINS = {"CartROM","SharedWRAM","ARM7WRAM"}`. `RegisterVanguardSpec()` pushes the spec to the CorruptCore and UI endpoints. `SpecUpdated` forwards partial updates to them.
- **Game-loaded spec.** `LOAD_GAME_DONE()` sets `SYSTEM="melonDS"`, `SYSTEMPREFIX="melonDS"`, `SYSTEMCORE="DS"`, `CORE_DISKBASED=false`, and `GAMENAME = MakeSafeFilename(GAME_NAME.ToString())`. That is the **decimal** string of the 32-bit gamecode, not the title. It then calls `RefreshDomains()`. If the game changed, it routes `ResetGameProtectionIfRunning`.
- **Other.** `CurrentDomain_AssemblyResolve` loads RTCV DLLs from `<exe>/../RTCV`. Command-line flags: `-ATTACHED` (attached-mode NetCore) and `-DISABLERTC` (all hooks become no-ops). A window-position timer (`MELON_LOCATION` NetCore param, every 5 min). Sync settings JSON `{ScreenRotation}` (`GetSyncSettings` / `SetSyncSettings`). `ReinitRendererTimer` (150 ms, one-shot) calls `ApplyNewSettings(3)` to force a renderer reinit.

### 1.2 Memory domains exposed to RTC

`GetInterfaces()` returns an empty array when `VSPEC::OPENROMFILENAME` is empty. Otherwise it returns 5 `MemoryDomainProxy` objects, in this order. All domains share these constants:

```
#define WORD_SIZE 4
#define BIG_ENDIAN false
#define MAIN_RAM_OFFSET 0x02000000
#define VRAM_OFFSET 0x06000000
#define VRAM_SIZE 0x00800000
#define SHAREDWRAM_SIZE 0x8000
#define ARM7WRAM_SIZE 0x10000
MAIN_RAM_SIZE = 0x400000   (src/NDS.h)
```

| # | Name | Size | WordSize | BigEndian | PeekByte / PokeByte implementation | Blacklisted by default |
|---|---|---|---|---|---|---|
| 0 | `MainRAM` | `MAIN_RAM_SIZE` = 0x400000 (4 MiB) | 4 | false | `NDS::ARM9Read8(addr + 0x02000000)` / `NDS::ARM9Write8(addr + 0x02000000, v)` | no |
| 1 | `SharedWRAM` | 0x8000 | 4 | false | direct `NDS::SharedWRAM[addr]` | yes |
| 2 | `ARM7WRAM` | 0x10000 | 4 | false | direct `NDS::ARM7WRAM[addr]` | yes |
| 3 | `VRAM` | 0x800000 | 4 | false | `NDS::ARM9Read8(addr + 0x06000000)` / `NDS::ARM9Write8(...)`. This goes through the ARM9 VRAM mapping: ABG `0x000000`, BBG `0x200000`, AOBJ `0x400000`, BOBJ `0x600000` (see `VRAM_*_OFFSET` defines). Unmapped areas read 0. LCDC (`0x06800000+`) is outside the domain. | no |
| 4 | `CartROM` | `NDSCart::CartROMSize` | 4 | false | direct `NDSCart::CartROM[addr]` (returns 0 or ignores the write if `!NDSCart::CartInserted`) | yes |

- `PeekBytes(addr, len)` loops over `PeekByte`. There is no `PokeBytes`. Out-of-range reads return 0 and out-of-range writes are ignored.
- Addresses are bounds-checked with `addr < Size` only.
- Nothing else is exposed: no palette, OAM, TCM, or DSi memory. A is DS-only; it predates DSi support.
- No JIT exists in 0.8.3, so no invalidation is needed.

### 1.3 Frame hook

- **Location.** `NDS::RunFrame()` (`references/melonds50x-vanguard/src/NDS.cpp` ~l.780-787). The call is placed after the `if (!Running) return 263; if (CPUStop & 0x40000000) return 263;` early-outs and before `GPU::StartFrame()`:
  ```cpp
  VanguardClientUnmanaged::CORE_STEP();
  GPU::StartFrame();
  ```
- **Thread.** `RunFrame` is called only from `EmuThreadFunc()` (`src/libui_sdl/main.cpp` ~l.902, an SDL thread) when `EmuRunning == 1`. So the hook runs **on the emulation thread** once per emulated frame, at the frame start.
- **What it does.** `CORE_STEP()` → `STEP_CORRUPT()` → `RtcClock::StepCorrupt(true, true)`. All per-frame work lives inside RTCV's managed CorruptCore, not in melonDS: step actions and blast-unit execution (including freeze/"infinite" units), autocorrupt ticks, and the clock counter. `LOAD_GAME_START` / `LoadState` reset it via `StepActions::ClearStepBlastUnits()` and `RtcClock::ResetCount()`.
- There is no savestate polling in the hook. Savestates are request-driven (1.4).

### 1.4 Savestates and emulation control

Commands are handled in `VanguardClient::OnMessageReceived`, which runs on the NetCore receiver thread:

| NetCore command | Action |
|---|---|
| `Basic::SaveSavestate` (arg: key string) | Stores sync settings in the spec. Builds `<RtcCore::workingDir>/SESSION/<safe(GAME_NAME)>.<Key>.timejump.State`, creates the directory, then `VanguardClient::SaveState(path)` → `Main::SaveState(path)`. `Main::SaveState` pauses (see 1.5), does `new Savestate(filename, true)` + `NDS::DoSavestate`, optionally relocates SRAM to `<file>.sav` if `Config::SavestateRelocSRAM`, shows an OSD message, and restores `EmuRunning`. Returns the path. |
| `Basic::LoadSavestate` (arg: `object[]{path,...}`) | Applies `VSPEC::SYNCSETTINGS` (screen rotation), clears step blast units, resets the RtcClock count, then `Main::LoadState(path, resumeAfter=false)`. That function pauses, backs up the current state to `timewarp.mln` (CWD-relative), loads the file (restoring the backup on error), and **leaves the emulator paused**. RTC later sends `Remote::ResumeEmulation`, which sets `EmuRunning = 1`. This is why `LOADSTATE_USES_CALLBACKS=true`. Returns true. |
| `Remote::LoadROM` (filename) | Marshalled to the main (form) thread via `SyncObjectSingleton::FormExecute`. `VanguardClient::LoadRom` sets `loading=true`, pauses (`EmuRunning=2`, spin on `EmuStatus==2`), calls `TryLoadROM(path, prevstatus)`, then spins with `Thread::Sleep(20)` + `Application::DoEvents()` until `LOAD_GAME_DONE` clears `loading`. |
| `Remote::CloseGame` | FormExecute → `Stop(false)`, which triggers the `GAME_CLOSED` hook: clears `OPENROMFILENAME`, calls `RefreshDomains`, `RtcCore::InvokeGameClosed`. |
| `Remote::DomainGetDomains` | `RefreshDomains()` |
| `Remote::KeySetSyncSettings` | Stores the string in the spec. |
| `Remote::IsNormalAdvance` | Returns true (fast-forward is not detected). |
| `Remote::PostCorruptAction` | If `Config::ScreenUseGL || Config::_3DRenderer != 0`, starts `ReinitRendererTimer` to reinit the GL renderer after a corruption. |
| `Remote::ResumeEmulation` | `EmuRunning = 1` |
| `Remote::EventEmuMainFormClose` / `EventCloseEmulator` | Under `Monitor::Enter(GenericLockObject)`: `SaveWindowPosition()`, `uiQuit()`, `Environment::Exit(0)`. |
| `Remote::AllSpecSent` | FormExecute → `LoadWindowPosition()` |
| `KeySetSystemCore`, `EventEmuStarted` | no-op |

Game name is `GAME_NAME` (the gamecode as a decimal int). The open ROM path is `OPENROMFILENAME`, set in `LOAD_GAME_START`. There is no explicit pause, reset, or frame-step command from RTC.

### 1.5 Threading and synchronization

- **Threads.**
  - Main/UI thread: libui plus the dummy WinForms `Form` created in `StartVanguardClient()` and used as `SyncObjectSingleton::SyncObject`.
  - Emu thread: `EmuThreadFunc`, an SDL thread.
  - NetCore receiver thread(s): these call `OnMessageReceived`.
  - `System::Timers` threads for the settings and renderer timers.
- **Emu-thread invoke.** `SyncObjectSingleton::EmuInvokeDelegate = EmuThreadExecute`. `EmuThreadExecute` does **not** run the callback on the emu thread. It "parks" the emu thread instead:
  ```cpp
  int prevstatus = EmuRunning; EmuRunning = 2; while (EmuStatus != 2); callback(); EmuRunning = prevstatus;
  ```
  The callback runs on the caller's thread while the emu loop sits in its paused branch (`EmuStatus = EmuRunning; SDL_Delay(100)`).
- **Primitives.** `Main::LoadState` / `SaveState` / `LoadRom` use the same busy-wait handshake. Its only primitives are `int EmuRunning` and `volatile int EmuStatus`, with no mutex and no memory barriers.
- **Where RTCV touches memory.** Memory-domain peeks and pokes from the per-frame hook run on the emu thread inside `RunFrame`. Other peeks and pokes (UI blasts, hex editor) go through RTCV's `EmuInvokeDelegate` pause.
- **Busy-wait latency.** The paused loop polls every 100 ms. Each emu-thread invoke can therefore spin for up to about 100 ms.

### 1.6 Windows-specific aspects

- C++/CLI: `set_source_files_properties(... COMPILE_FLAGS "/clr /EHa /Zc:twoPhase-")`, `DOTNET_TARGET_FRAMEWORK_VERSION v4.7.1`. `VS_DOTNET_REFERENCE_*` point to `../../RTCV/Build/{CorruptCore,NetCore,RTCV.Common,Vanguard}.dll`, and `System.Windows.Forms` is referenced. `/RTC*` flags are stripped (they are incompatible with /clr). `/CLRTHREADATTRIBUTE:STA` and `/MANIFEST:NO` are set.
- MSVC-only top-level flags: `/Dstrncasecmp=_strnicmp`, `/DWIN32`, `/GL /LTCG`. Links `ole32 gdi32 user32 shell32 dwrite d2d1 usp10 ws2_32 iphlpapi SDL2 uxtheme comctl32 opengl32`. `add_executable(melonDS WIN32 ...)`. RTCV DLLs/PDBs are copied into the build dir.
- libui Windows backend; WinForms visual styles and a dummy `Form` for thread marshalling; `msclr/marshal_cppstd.h`.
- CI: `windows-2019` + `microsoft/setup-msbuild`. It builds RTCV `506v2` first, then runs `cmake ..\..\..\ -DENABLE_LTO=false -DDONT_COPY_FIRMWARE=true -DDONT_BUILD_EXTRA_SDL_CODE=true -DCI_BUILD_SHARED_LIBS=true -DCI_INCLUDE_EXTRA_LIBUI_LIBS=true` and `msbuild melonDS.sln`. No artifact upload.
- Nothing in the design is inherently Windows-only except the .NET/NetCore transport. Our replacement is a TCP RPC server.

---

## Part 2: Current upstream-based fork (B)

### 2.1 Frontend architecture

**`main.cpp`** (`src/frontend/qt_sdl/main.cpp`)
- `main()` runs these steps in order:
  1. `MelonApplication` (a `QApplication`)
  2. `pathInit()` sets `emuDirectory`: a `portable/` dir next to the exe, else `QStandardPaths::ConfigLocation/melonDS`, or the exe dir on Windows with `WIN32_PORTABLE`.
  3. `CLI::ManageArgs()`
  4. SDL init (haptic, joystick, sensor, audio, video)
  5. `Config::Load()`
  6. `setMPInterface(MPInterface_Local)`
  7. `NetInit()` (slirp or pcap)
  8. `createEmuInstance()`, which does `new EmuInstance(0)`
  9. `win->preloadROMs(dsfile, gbafile, options->boot)`
  10. `melon.exec()`
- Globals: `EmuInstance* emuInstances[kMaxEmuInstances=16]`, `Net net`.

**`EmuInstance`** (`EmuInstance.h/.cpp`)
- One per emulated console. The constructor reads config, then runs `audioInit()`, `inputInit()`, `emuThread = new EmuThread(this)`, **`createWindow()` (always creates a `MainWindow`)**, and `emuThread->start()`.
- Owns `melonDS::NDS* nds` (`getNDS()`). **`nds` is deleted and recreated** by `updateConsole()` (~l.1224-1390) when the console type changes. It is null until the first boot. Never cache it across emu-thread messages.
- Private (friend `EmuThread`, `MainWindow`): `loadState`, `saveState`, `undoStateLoad`, `reset`, `loadROM`, `bootToMenu`, `inputMask`, `touchX/Y`, `isTouching`, `hotkey*`, `enableCheats`.

**`EmuThread`** (`EmuThread.h/.cpp`, a `QThread`)
- Message queue: `sendMessage(Message{type, QVariant param})` enqueues under `msgMutex`. `waitMessage(n)` does `msgSemaphore.acquire(n)` (no-op on the emu thread itself). `waitAllMessages()` spins acquiring until the queue is empty.
- `handleMessages()` (l.469-667) drains the queue once per loop iteration and releases the semaphore once per message.
- Results come back via **shared** `msgResult` / `msgError` members.
- Message types: `msg_Exit`, `msg_EmuRun`, `msg_EmuPause`, `msg_EmuUnpause`, `msg_EmuStop`, `msg_EmuFrameStep`, `msg_EmuReset`, `msg_InitGL`, `msg_DeInitGL`, `msg_BorrowGL`, `msg_BootROM`, `msg_BootFirmware`, `msg_InsertCart`, `msg_EjectCart`, `msg_InsertGBACart`, `msg_InsertGBAAddon`, `msg_EjectGBACart`, `msg_LoadState`, `msg_SaveState`, `msg_UndoStateLoad`, `msg_ImportSavefile`, `msg_EnableCheats`.
- UI-side wrappers (l.699-872):
  - `emuRun()`
  - `emuPause(bool broadcast=true)` / `emuUnpause(...)`. These use a pause stack (`emuPauseStack`, threshold 1), so nested pauses are safe. `broadcast` also pauses other instances via `broadcastCommand(InstCmd_Pause)`.
  - `emuTogglePause()`
  - `emuStop(bool external)`
  - `emuExit()`
  - `emuFrameStep()`: sends `msg_EmuPause` if not already paused, then `msg_EmuFrameStep`, then `waitAllMessages()`. It returns when the messages have been *handled*, **not when the frame has been emulated**.
  - `emuReset()`
  - `bootROM(QStringList, QString& err)` (`msg_BootROM` then `msg_EmuRun`)
  - `bootFirmware()`
  - `insertCart()`, `ejectCart()`
  - `saveState(QString)`, `loadState(QString)`, `undoStateLoad()`
  - `importSavefile()`
  - `enableCheats()`
  - `emuIsRunning()`, `emuIsActive()`
- Status: `emuStatus_{Exit,Running,Paused,FrameStep}`.
- **Main loop `EmuThread::run()` (l.106-447)** runs `while (emuStatus != emuStatus_Exit)`:
  1. l.155-169: `MPInterface::Get().Process()` (instance 0 only), `emuInstance->inputProcess()` (**recomputes `inputMask = keyInputMask & joyInputMask`**, EmuInstanceInput.cpp l.446), then hotkeys.
  2. l.171: `if (emuStatus == Running || emuStatus == FrameStep)`. l.173: FrameStep is turned back into Paused *before* running, so exactly one frame runs.
  3. l.192-227: DSi power/volume buttons. l.229-252: GL make-current and renderer update under `renderLock`.
  4. l.255-260: `nds->SetKeyMask(inputMask)`, then `TouchScreen(touchX, touchY)` or `ReleaseScreen()`. l.262 lid.
  5. l.269-296: auto screen layout. l.299: `syncRTC()`.
  6. **l.302-312: emulate.** Runs `nlines = emuInstance->nds->RunFrame();`, or `compileShaders()` while the GL renderer `NeedsShaderCompile()`. In that case the frame is *not* emulated but `nlines = 1`.
  7. l.314-321: `ndsSave/gbaSave/firmwareSave->CheckFlush()`. l.323: `emuInstance->drawScreen()`.
  8. l.329-426: window update signal, fast-forward/slowmo, DSi volume, `audioSync()`, frame limiter (`SDL_Delay`), FPS title.
  9. l.428-443 (paused branch): `emit windowUpdate()`, **`SDL_Delay(75)`**, `drawScreen()`.
  10. **l.445: `handleMessages()`**, reached every iteration in both running and paused states.

**`Window.cpp`**
- `MainWindow::preloadROMs(file, gbafile, boot)` (l.1065). It calls `verifySetup()` (BIOS/firmware checks; errors raise a `QMessageBox`), then `emuThread->insertCart` / `bootROM`.
- `onSaveState` / `onLoadState` (~l.1517-1597) wrap `emuThread->saveState/loadState` in `emuPause()` / `emuUnpause()` around the file dialog. Slot names come from `getSavestateName(slot)` → `<SavestatePath or ROM dir>/<rom base>.ml<slot>`.

**CLI** (`CLI.cpp`, `CLI.h`)
- Uses `QCommandLineParser`. Positional arguments: `nds` and `gba` (ROM or archive).
- Options: `-b/--boot auto|always|never` (default `auto`, which boots if an NDS ROM is given), `-f/--fullscreen`, `-a/--archive-file`, `-A/--archive-file-gba` (only with `ARCHIVE_SUPPORT_ENABLED`). The result is a `CommandLineOptions{dsRomPath, dsRomArchivePath, gbaRomPath, gbaRomArchivePath, fullscreen, boot}`.
- New flags (e.g. `--rpc-port`) are added here.

**Config** (`Config.cpp`)
- TOML file `melonDS.toml` (`kConfigFile`) at `Platform::GetLocalFilePath` → `emuDirectory/melonDS.toml`. Loaded with `toml::parse`. A legacy `.ini` is imported if the TOML file is absent.
- Access: `Config::GetGlobalTable()`, `Config::GetLocalTable(inst)` (key `Instance<N>`), `Table::GetInt/GetBool/GetString/GetDouble`. Defaults come from `DefaultInts` / `DefaultBools` etc.
- Relevant keys:

| Key | Default |
|---|---|
| `Emu.ConsoleType` | 0 = DS, 1 = DSi |
| `Emu.DirectBoot` | true |
| `Emu.ExternalBIOSEnable` | false → FreeBIOS |
| `3D.Renderer` | 0 = software, 1 = GL, 2 = GL compute |
| `Screen.UseGL` | — |
| `JIT.Enable` | **false** |
| `JIT.FastMemory` | true, except on Apple |
| `LimitFPS` | true |
| `AudioSync` | — |
| `TargetFPS` | — |
| `Instance*.RTC.SyncToHost` | true |

- A dedicated config dir can be forced by placing a `portable/` directory next to the executable.

**Headless**
- There is no no-GUI mode and no test harness in this tree. The `test` dirs belong to vendored libslirp / teakra / gdb.
- `EmuInstance` always creates a `MainWindow`. `preloadROMs` / `verifySetup` use `QMessageBox`. `EmuThread::run` calls `mainWindow` / GL paths when `usesOpenGL()`.
- Minimal practical options:
  - (a) Run the normal app with the software renderer (`3D.Renderer=0`, `Screen.UseGL=false`), possibly with `QT_QPA_PLATFORM=offscreen` on Linux CI. Untested; audio needs an SDL audio device or the `SDL_AUDIODRIVER=dummy` env var.
  - (b) Write a separate tiny runner that links `core` and constructs `melonDS::NDS` from `NDSArgs` directly. It must implement all `melonDS::Platform::*` functions declared in `src/Platform.h` (see `qt_sdl/Platform.cpp` for the reference implementation). Higher effort.

### 2.2 Memory access

**Core members**
- `NDS` (`src/NDS.h`; all listed members are `public`):
  - `u8* MainRAM`, with `u32 MainRAMMask` = `0x3FFFFF` for DS or `0xFFFFFF` for DSi (set in `NDS::Reset`, NDS.cpp l.450-461). The allocation is always `MainRAMMaxSize=0x1000000`.
  - `u8* SharedWRAM` (0x8000), `u8* ARM7WRAM` (0x10000). These point into `JIT.Memory` (`NDS::NDS` l.132-134: `JIT.Memory.GetMainRAM()` etc.).
  - `MemRegion SWRAM_ARM9, SWRAM_ARM7`: the current WRAMCNT mapping.
  - `ARM9.ITCM[0x8000]` and `u8* ARM9.DTCM` (0x4000) in `ARMv5` (`src/ARM.h` l.338-339).
  - `u32 NumFrames, NumLagFrames; bool LagFrameFlag`.
  - `u32 KeyInput`, `u16 PowerControl9`, `int ConsoleType`.
  - `melonDS::GPU GPU`, `ARMJIT JIT`, `AREngine AREngine`, `NDSCartSlot`.
- `GPU` (`src/GPU.h`, public up to l.707):
  - `u8 Palette[2*1024]`, `u8 OAM[2*1024]`.
  - `u8 VRAM_A..VRAM_D[128K]`, `VRAM_E[64K]`, `VRAM_F[16K]`, `VRAM_G[16K]`, `VRAM_H[32K]`, `VRAM_I[16K]`.
  - `u8* const VRAM[9]`, `u32 const VRAMMask[9]`, `VRAMMap_*`, `VRAMCNT[9]`.
  - `NonStupidBitField<...> VRAMDirty[9]` (granularity `VRAMDirtyGranularity`=512), `u32 OAMDirty`, `u32 PaletteDirty`.
  - Accessors: `ReadVRAM_{ABG,BBG,AOBJ,BOBJ,LCDC}<T>(addr)`, `WriteVRAM_*<T>(addr,val)` (they set `VRAMDirty`), `SyncVRAM_*(addr, write)` (needed for GL display-capture coherency), `ReadPalette<T>` / `WritePalette<T>` (set `PaletteDirty`), `ReadOAM<T>` / `WriteOAM<T>` (set `OAMDirty`).
- `DSi` (`src/DSi.h`, `class DSi final : public NDS`):
  - `u8* NWRAM_A, NWRAM_B, NWRAM_C` (each `NWRAMSize=0x40000`), `NWRAMMap_*`.
  - `std::array<u8,0x10000> ARM9iBIOS, ARM7iBIOS`, `SCFG_*`, `NDSCartSlot2`.
  - The bus handlers `ARM9Read*/Write*/ARM7*` are `virtual` and overridden for DSi.
- Cart: `nds->NDSCartSlot.GetCart()` returns `CartCommon*`.
  - `GetROM()` is **const-only**; `GetROMLength()`.
  - `GetHeader()` → `NDSHeader{GameTitle[12], GameCode[4], ...}`.
  - `NDSCartSlot.GetSaveMemory()` / `GetSaveMemoryLength()`.

**Bus accessors**
- `NDS::ARM9Read8/16/32(u32)`, `ARM9Write8/16/32(u32,val)`, `ARM7Read*/ARM7Write*` (virtual).
- They emulate the bus: I/O side effects on `0x04xxxxxx`. Palette and OAM are gated by `PowerControl9`. **`ARM9Write8` ignores `0x05`/`0x06`/`0x07` (hardware behaviour, unlike fork A).** They do not see ITCM/DTCM, which live in `ARMv5::DataRead*`.
- Writes to RAM/WRAM/VRAM call `JIT.CheckAndInvalidate<cpu, region>(addr)` (e.g. NDS.cpp l.2163-2226, l.2559-2727).

**RAMInfoDialog** (`RAMInfoDialog.cpp/.h`) is the existing pattern for external memory access:
- `RAMSearchThread::run()` (a separate `QThread`) calls `emuInstance->getEmuThread()->emuPause()`. It then scans `nds.MainRAM + (addr & nds.MainRAMMask)` directly for `0x02000000..0x02000000+MainRAMMaxSize` via `GetMainRAMValue()`, and finally calls `emuUnpause()`. This is safe because the paused emu thread does not run `RunFrame`.
- `ShowRowsInTable` (a `QTimer` on the UI thread) calls `ramInfo_RowData::Update()`, reading `MainRAM` **without pausing** (racy, benign).
- `ramInfo_RowData::SetValue()` writes `nds.MainRAM[Address & MainRAMMask]` directly from the UI thread, **without a pause and without JIT invalidation**.

**AREngine** (`src/AREngine.cpp`)
- `AREngine::RunCheats()` is called from `ARMv4::TriggerIRQ` (ARM.cpp l.541-547) when the ARM7 takes a VBlank IRQ. That means it runs **on the emu thread inside `RunFrame`**.
- It uses `NDS.ARM7Read*/ARM7Write*`, which include JIT invalidation.
- Codes are swapped in by the frontend via `nds->AREngine.Cheats = ...` inside `msg_EnableCheats` / `loadCheats()`, i.e. on the emu thread.

**Proposed domain table** for the new server. Little-endian throughout. Sizes match fork A where the domain exists there.

| Name | Size | Word | Endian | Backing / accessor | Write notes |
|---|---|---|---|---|---|
| `MainRAM` | `MainRAMMask+1` (DS 0x400000 = A's size; DSi 0x1000000) | 4 | LE | `nds->MainRAM[off]` | Afterwards `JIT.CheckAndInvalidate<0, memregion_MainRAM>(0x02000000+off)`. The local address is CPU-independent for MainRAM, so one call covers both CPUs. |
| `SharedWRAM` | 0x8000 | 4 | LE | `nds->SharedWRAM[off]` (physical, ignores WRAMCNT mapping) | invalidate `memregion_SharedWRAM` with local addr `(memregion_SharedWRAM<<27)|off` (helper, see 3.3) |
| `ARM7WRAM` | 0x10000 | 4 | LE | `nds->ARM7WRAM[off]` | `JIT.CheckAndInvalidate<1, memregion_WRAM7>(off)` |
| `VRAM` (A-compatible) | 0x800000 | 4 | LE | ARM9 view at `0x06000000+off`. Read: `nds->ARM9Read8` (goes through `GPU.ReadVRAM_*`). Write: `GPU.SyncVRAM_*(addr,true); GPU.WriteVRAM_*<u8>(addr,v)`, selected by `addr & 0x00E00000` exactly as in `ARM9Write16`. Do not use `ARM9Write8`, which drops the write. | `JIT.CheckAndInvalidate<0, memregion_VRAM>`. Mirrors and unmapped areas behave as on the bus. |
| `VRAMBanks` (optional, raw) | 0xA4000 (A-D 4×0x20000, E 0x10000, F 0x4000, G 0x4000, H 0x8000, I 0x4000, concatenated) | 4 | LE | `GPU.VRAM[bank][off]` | Set `GPU.VRAMDirty[bank][off/512] = true`. Mapping-independent, so better for corruption. |
| `Palette` | 0x800 | 2 | LE | `GPU.Palette` via `ReadPalette<u8>` / `WritePalette<u8>` | `WritePalette` sets `PaletteDirty` |
| `OAM` | 0x800 | 2 | LE | `GPU.OAM` via `ReadOAM<u8>` / `WriteOAM<u8>` | `WriteOAM` sets `OAMDirty` |
| `ITCM` | 0x8000 | 4 | LE | `nds->ARM9.ITCM[off]` | `JIT.CheckAndInvalidateITCM()` (or `InvalidateByAddr((memregion_ITCM<<27)|off)`) |
| `DTCM` | 0x4000 | 4 | LE | `nds->ARM9.DTCM[off]` | data only |
| `CartROM` | `GetCart()->GetROMLength()` | 4 | LE | `const_cast<u8*>(GetCart()->GetROM())[off]` | Blacklisted in A. Affects later cart reads only. |
| `CartSave` (optional) | `NDSCartSlot.GetSaveMemoryLength()` | 1 | LE | `NDSCartSlot.GetSaveMemory()` | flush goes via SaveManager |
| DSi only: `NWRAM_A`/`NWRAM_B`/`NWRAM_C` | 0x40000 each | 4 | LE | `static_cast<DSi*>(nds)->NWRAM_A[off]` etc. | invalidate `memregion_NewSharedWRAM_{A,B,C}` (local addr = `(region<<27)|off`) |

Expose `SharedWRAM`, `ARM7WRAM` and `CartROM` but default-blacklist them, as A did.

### 2.3 Savestates

**Savestate API** (`src/Savestate.h/.cpp`)
- `SAVESTATE_MAJOR 14`, `SAVESTATE_MINOR 0`, magic `"MELN"`.
- `Savestate(u32 initial_size = DEFAULT_SIZE /*32 MiB*/)`: an owned, growable save buffer.
- `Savestate(void* buffer, u32 size, bool save)`: wraps an external buffer. For loading it validates the magic, the major version (must be equal), the minor version (must be `<=`), and that **the stored length equals `size` exactly**. Otherwise it sets `Error`.
- Members: `Error`, `Saving`, `Buffer()`, `Length()` (bytes written), `BufferLength()`, `Rewind(bool save)`, `Finish()`.

**`NDS::DoSavestate(Savestate*)`** (NDS.cpp l.631-773)
- Checks the config word `GetSavestateConfig()` (`SC_Console_DSi` bit). A DS state is therefore rejected on DSi and vice versa.
- Always stores `MainRAMMaxSize` (16 MiB), so states are larger than 16 MiB. It includes `NumFrames`, the cart slot, and GPU state.
- On load it calls `JIT.Reset()`, so no manual JIT invalidation is needed.
- The ROM image is not included, so the same ROM must be loaded.

**EmuInstance**
- `saveState(filename)` (l.775) serializes into an in-memory `Savestate`, then writes `state.Buffer()` / `state.Length()` to the file.
- `loadState(filename)` (l.718) takes a backup into `backupState` (a `unique_ptr<Savestate>`), reads the whole file into a `std::vector<u8>`, then calls `Savestate(buf, size, false)` + `nds->DoSavestate`. On success it keeps the backup and sets `savestateLoaded = true`.
- `undoStateLoad()` does `backupState->Rewind(false)` + `DoSavestate`. `clearBackupState()` also exists.
- All of this runs on the emu thread via `msg_SaveState` / `msg_LoadState`.

**In-memory states are trivially available** (run on the emu thread):
```cpp
melonDS::Savestate s;                  // save
nds->DoSavestate(&s);                  // then send s.Buffer(), s.Length()
melonDS::Savestate l(buf.data(), (u32)buf.size(), false);   // load
if (!l.Error) nds->DoSavestate(&l);
```
There is no need to go through files. The server can still accept file paths for RTCV-style `SESSION/*.State` compatibility.

### 2.4 Frame count and timing

- **Counter.** `nds->NumFrames` is incremented at the end of `NDS::RunFrame<>()` (NDS.cpp ~l.1067), together with `NumLagFrames` / `LagFrameFlag`. It is part of savestates, so it jumps on load. Keep our own monotonic counter in the hook if needed.
- **Frame step.**
  - `emuFrameStep()` runs exactly one frame on the next loop iteration, but returns before that frame runs.
  - To run N frames while paused, add a new message such as `msg_RunFrames(n)` / a counter `framesToRun`. The loop runs while `framesToRun > 0` and decrements it at l.173, where it currently does `if (emuStatus == emuStatus_FrameStep) emuStatus = emuStatus_Paused;`. The request completes (future/promise) when the counter hits 0 after the post-frame hook.
  - While paused, the loop sleeps `SDL_Delay(75)` per iteration (l.440). RPC ops marshalled to the emu thread therefore take up to about 75 ms. Replace the sleep with a wait on a condition variable signalled by `sendMessage`, or shorten it.
- **Frame-boundary hook points in `EmuThread::run()`:**
  - **Pre-frame:** after `SetKeyMask` / `TouchScreen` (l.255-260) and `syncRTC()` (l.299), just before `// emulate` (l.302). Apply input overrides and pre-frame freezes/pokes here.
  - **Post-frame:** right after `nlines = emuInstance->nds->RunFrame();` (l.311), before `CheckFlush` / `drawScreen` (l.314-323). Apply per-frame freezes and step corruptions, capture the framebuffer, complete frame-step futures. Skip this when `compileShaders()` ran instead of `RunFrame` (`nlines=1` and no frame emulated).
  - **Always (running or paused):** at `handleMessages()` (l.445). Drain the RPC request queue here too.
  - A to B mapping: A's hook was *inside* `NDS::RunFrame` at the frame start. The pre-frame point is equivalent, and B needs no core change.

### 2.5 Input

- `NDS::SetKeyMask(u32 mask)` (NDS.cpp l.1206). The mask is 12 bits, **active-low** (0 = pressed). Bit order follows `EmuInstance::buttonNames`: A, B, Select, Start, Right, Left, Up, Down, R, L, X, Y. Bits 0-9 go into `KeyInput` bits 0-9 and bits 10-11 (X, Y) into bits 16-17. It raises the keypad IRQ via `CheckKeyIRQ`.
- `NDS::TouchScreen(u16 x, u16 y)`, `NDS::ReleaseScreen()`, `SetLidClosed(bool)`.
- The frontend recomputes `emuInstance->inputMask` every iteration in `inputProcess()` and applies it at l.255. To inject input, AND or override the mask at the pre-frame hook (e.g. `nds->SetKeyMask(inputMask & rpcMask)`), or add an override field consulted at l.255-260. `EmuInstance::touchScreen(x, y)` / `releaseScreen()` set `touchX/touchY/isTouching`.

### 2.6 Framebuffer

- `nds->GPU.GetFramebuffers(void** top, void** bottom)` (GPU.cpp l.347) delegates to the renderer:
  - **Software** (`SoftRenderer::GetFramebuffers`, GPU_Soft.cpp l.447): returns `true`. Pointers are `Framebuffer[BackBuffer^1][0/1]`, each 256×192 `u32`. The format is `0xAARRGGBB` in memory order B, G, R, A, uploaded by the frontend as `GL_BGRA/GL_UNSIGNED_BYTE`. Read it on the emu thread right after `RunFrame`, or copy it under `renderLock` as `ScreenPanelNative::paintEvent` does.
  - **OpenGL / compute** (`GLRenderer::GetFramebuffers`, GPU_OpenGL.cpp l.915): returns `false`, and `*top` points to a `GLuint` (`FPOutputTex[frontbuf]`), a `GL_TEXTURE_2D_ARRAY` with two layers scaled by `3D.GL.ScaleFactor`. Reading it requires `glGetTexImage` / FBO + `glReadPixels` on the emu thread with its context current (`emuInstance->makeCurrentGL()`).
- Recommendation: force the software renderer when under RPC control, or implement the GL readback path separately.

### 2.7 Networking already present

**Existing networking**
- **net-utils** (`src/net/CMakeLists.txt`, `add_library(net-utils STATIC ...)`): `Net.cpp`, `Net_Slirp.cpp` (libslirp, bundled or `USE_SYSTEM_LIBSLIRP`), `Net_PCap.cpp`, `LocalMP.cpp`, `LAN.cpp` / `Netplay.cpp` (enet; `enet_initialize()` in `LAN.cpp` l.105), `MPInterface.cpp`. Linked with `target_link_libraries(melonDS PRIVATE net-utils)`.
- **GDB stub** (`src/debug/GdbStub.cpp`, `ENABLE_GDBSTUB` ON by default, `-DGDBSTUB_ENABLED`): a plain BSD-socket TCP server. It is the best template for our server:
  - `#ifdef _WIN32`: `WSADATA wsa; WSAStartup(MAKEWORD(2,2), &wsa)`.
  - `#else`: `signal(SIGPIPE, SIG_IGN)`.
  - `socket(AF_INET, SOCK_STREAM, 0)`, `SO_REUSEADDR`, non-blocking on Linux.
  - It is polled from the emu thread.
- **Qt Network** is linked: `find_package(Qt6 COMPONENTS ... Network ...)`, and `QT_LINK_LIBS` includes `Qt6::Network`. It is used only by `TitleManagerDialog.cpp` (`QNetworkAccessManager`). `QTcpServer` is therefore available at no extra dependency cost. It needs a thread with a Qt event loop. vcpkg `qtbase` is built with `default-features: false` + the listed features, and the CI build succeeds with `Qt6::Network` REQUIRED.
- **Windows libs:** `core` links `ole32 comctl32 wsock32 ws2_32` (`src/CMakeLists.txt`). `melonDS` links `ws2_32 iphlpapi` (qt_sdl CMakeLists l.218). libslirp links `ws2_32 iphlpapi`.

**CMake structure**
- Top-level `CMakeLists.txt`:
  - options `ENABLE_JIT` (x86_64/ARM64; **OFF on x86_64 macOS**), `ENABLE_OGLRENDERER`, `ENABLE_GDBSTUB`, `BUILD_QT_SDL`, `USE_VCPKG`, `BUILD_STATIC`
  - `add_subdirectory(src)` → `add_library(core STATIC ...)` (core sources listed explicitly, C++17)
  - `add_subdirectory(src/frontend/qt_sdl)` → `set(SOURCES_QT_SDL ...)` + `add_executable(melonDS ${SOURCES_QT_SDL})`. It `add_subdirectory("../../net" ${CMAKE_BINARY_DIR}/net)` for net-utils and links `core`, SDL2, LibArchive, Zstd, Faad, and Qt.
- Adding `rtcvish`: create `src/frontend/qt_sdl/rtcvish/` (it needs `EmuInstance` / `EmuThread`, so it belongs to the frontend, not `core`). Either append its `.cpp` files to `SOURCES_QT_SDL`, or do `add_library(rtcvish STATIC ...)` in a subdir, link it `PRIVATE` into `melonDS`, and give it `target_link_libraries(rtcvish PRIVATE core Qt6::Core Qt6::Network)` plus `ws2_32` on WIN32. Frontend include dirs are `${CMAKE_CURRENT_SOURCE_DIR}` and `..`.

### 2.8 Upstream CI workflows

All three trigger on push to `master`/`ci/*` and PRs to `master`. They set `MELONDS_GIT_BRANCH`, `MELONDS_GIT_HASH`, `MELONDS_BUILD_PROVIDER` and pass `-DMELONDS_EMBED_BUILD_INFO=ON`, which requires those env vars.

| Workflow | Runner(s) | Dependencies | Configure / build | Artifact |
|---|---|---|---|---|
| `build-ubuntu.yml` | `ubuntu-22.04` (x86_64), `ubuntu-22.04-arm` (aarch64) | `apt install cmake ninja-build extra-cmake-modules libpcap0.8-dev libsdl2-dev libenet-dev qt6-{base,base-private,multimedia}-dev qt6-wayland libqt6svg6-dev libarchive-dev libzstd-dev libfuse2 libfaad-dev` | `cmake -B build -G Ninja -DCMAKE_INSTALL_PREFIX=/usr -DMELONDS_EMBED_BUILD_INFO=ON`; `cmake --build build`; `DESTDIR=AppDir cmake --install build` | Raw binary `AppDir/usr/bin/melonDS` (`melonDS-ubuntu-<arch>`), plus an AppImage built by `linuxdeploy` + `linuxdeploy-plugin-qt` (`QMAKE=/usr/lib/qt6/bin/qmake`, Wayland plugins) → `melonDS*.AppImage` (`melonDS-appimage-<arch>`) |
| `build-macos.yml` | `macos-26`, matrix x86_64 / arm64 | `brew install autoconf automake autoconf-archive libtool python-setuptools`; `lukka/get-cmake`; vcpkg via `lukka/run-vcpkg@v11` (`vcpkg.json`, cache in `vcpkg_cache`) | `lukka/run-cmake@v10`, preset `release-mac-<arch>` (Ninja, Release, `USE_VCPKG=ON`, `BUILD_STATIC=ON`, `CMAKE_OSX_ARCHITECTURES`) | `build/release-mac-<arch>/melonDS.app` zipped → `macOS-<arch>.zip`. A second job `lipo`s both into a universal `melonDS.app`, runs `codesign -s - --deep`, and produces `macOS-universal.zip`. |
| `build-windows.yml` | `windows-2025-vs2026` (x86_64), `windows-11-arm` (arm64) | vcpkg via `lukka/run-vcpkg@v11` (same cache scheme) | `lukka/run-cmake@v10`, preset `release-windows-<arch>` (Ninja, clang/clang++/llvm-rc, vcpkg, static), extra `-DENABLE_DEBUG_DEPS=OFF -DMELONDS_EMBED_BUILD_INFO=ON` | Single static `build\release-windows-<arch>\melonDS.exe` (`melonDS-windows-<arch>`) |

There is also `build-bsd.yml` (NetBSD, not needed). Presets are in `CMakePresets.json`. `release-mingw-x86_64` exists for MSYS2 but is not used by CI.

---

## Part 3: Recommendations

**Placement**
- Put the server in `src/frontend/qt_sdl/rtcvish/`:
  - `RpcServer.{h,cpp}`: socket thread
  - `RpcDispatcher`: command → emu-thread job
  - `MemoryDomains.{h,cpp}`
- Start it from `main()` after `createEmuInstance()` when a CLI flag such as `--rpc-port <port>` / `--rpc-host` is given (add it to `CLI::CommandLineOptions`). It should bind to 127.0.0.1 by default.
- Run the listener on its own `std::thread`: plain sockets, with the `WSAStartup` / `SIGPIPE` handling copied from `GdbStub.cpp`. A `QThread` + `QTcpServer` is the alternative. The server thread only parses and serialises; it **never touches `nds`**.

**Marshalling onto the emu thread**
- Don't reuse `sendMessage` / `waitMessage` for RPC as-is. `msgResult` / `msgError` are shared, and the `msgSemaphore` count is not per-message, so concurrent UI and RPC waiters can wake each other.
- Instead, add a dedicated `msg_RpcJob` (or a separate lock-protected `std::deque<std::function<void()>>` in `EmuThread`) that carries a `std::packaged_task` / promise per request. Drain it inside `handleMessages()` (l.445), and optionally at the pre-/post-frame hook points (2.4).
- Everything then executes on the emu thread: domain peek/poke, savestate to/from buffer, per-frame freezes and step actions, frame-N-then-pause, input injection, framebuffer copy.
- Always resolve `emuInstance->getNDS()` on the emu thread; it can be recreated by `updateConsole()`. Reject memory ops when `!emuThread->emuIsActive()` or `nds == nullptr`.
- Touching memory from another thread is *only* safe while the emu thread is parked in the paused branch (the `RAMSearchThread` pattern: `emuPause(false)` … `emuUnpause(false)`). Pass `broadcast=false` to avoid pausing other instances. Even then, `drawScreen()` / `inputProcess()` keep running. Prefer the job queue.
- Replace `SDL_Delay(75)` in the paused branch with a timed wait on a condition variable signalled on enqueue, so paused RPC calls are low-latency.

**JIT gotchas**
- JIT is compiled in by default (except x86_64 macOS) but `JIT.Enable` defaults to false.
- When enabled, writing to `nds->MainRAM` etc. directly (as `RAMInfoDialog` does) does **not** invalidate compiled blocks. Stale code keeps running after code-region corruption.
- With `JIT.FastMemory`, only the fastmem views are write-protected. `nds->MainRAM` is the backing `MemoryBase` view, so direct writes don't fault.
- After each raw write, call `nds->JIT.CheckAndInvalidate<cpu, ARMJIT_Memory::memregion_X>(guestAddr)` (public template, ARMJIT.h l.58-64). For raw offsets (SharedWRAM, NWRAM), replicate its body with a local address `(region << 27) | offset`, using the public `JIT.CodeMemRegions` + `JIT.InvalidateByAddr(localAddr)`. For ITCM use `JIT.CheckAndInvalidateITCM()`. For bulk corruption, `JIT.ResetBlockCache()` is the simple heavy hammer.
- Guard all of this with `#ifdef JIT_ENABLED`; the non-JIT stubs are no-ops anyway.
- Loading a state already calls `JIT.Reset()`.
- AREngine avoids the problem by using `ARM7Write*`, which invalidates. Bus writes are acceptable for MainRAM/WRAM, but not for VRAM/palette/OAM byte writes, which the bus ignores.
- `NDS::Current` is `thread_local` and is set only in `RunFrame`. The JIT slow-path helpers and the fault handler depend on it, which is another reason to stay on the emu thread.

**DSi mode**
- `ConsoleType==1` creates a `DSi` object (`static_cast<DSi*>(nds)`), MainRAM is 16 MiB (`MainRAMMask+1`), and the NWRAM A/B/C domains exist.
- Savestates are not interchangeable between modes (config word check).
- DSi needs a real DSi BIOS, firmware, and NAND (`verifySetup`).
- The GBA slot is ejected in DSi mode.
- Report the domain list per console type and refresh it after boot/reset (as A did with `RefreshDomains` on `LOAD_GAME_DONE`).

**Renderers**
- For screenshots and deterministic tests, force `3D.Renderer=0` (software) and `Screen.UseGL=false`. `GetFramebuffers` then returns CPU pointers.
- With GL or compute, readback must happen on the emu thread with `makeCurrentGL()`, and resolution follows `ScaleFactor`.
- The GL renderer caches textures: A re-initialised the renderer after corruption (`PostCorruptAction` → `ApplyNewSettings(3)`). In B, VRAM writes through `WriteVRAM_*` / `VRAMDirty` / `PaletteDirty` / `OAMDirty` keep both renderers coherent. Raw writes that skip the dirty flags will not be picked up by `MakeVRAMFlat_*Coherent`.
- While `NeedsShaderCompile()` is true (GL first run), frames are not emulated. Frame-step and frame-count logic must ignore those iterations.

**Other**
- `emuFrameStep()` is asynchronous with respect to the frame. Implement "run N frames and reply" with a counter completed from the post-frame hook.
- Input injection must survive `inputProcess()` overwriting `inputMask` each iteration.
- Game identity: use `GetCart()->GetHeader().GameCode` / `GameTitle`, not A's decimal gamecode.
