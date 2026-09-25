# RTCV reference for rtcv-ish

Source analysed: `references/rtcv` (RTCV 5.1.0, `RtcCore.RtcVersion = "5.1.0"`), plus the melonDS Vanguard client in `references/melonds50x-vanguard/src/Vanguard/VanguardClient.cpp` as a concrete example of the emulator side. Paths below are relative to the rtcv-ish repo root. Abbreviations: `CC` = `references/rtcv/Source/Libraries/CorruptCore`, `NC` = `references/rtcv/Source/Libraries/NetCore`, `UI` = `references/rtcv/Source/Frontend/UI`.

## 1. Repository layout

| Project | Path | Contents |
|---|---|---|
| RTCV.Common | `references/rtcv/Source/Libraries/Common` | NLog logging setup, `RandomExtensions` (`NextULong`/`NextLong` with min/max), `S` form/object registry (`S.GET<T>()`, `S.BLINDMAKE(name)`), log console form. |
| NetCore | `NC` | IPC: `TCPLink`, `UDPLink`, `MessageHub`, `ReturnWatch`, `NetCoreConnector`, `LocalNetCoreRouter`, spec system (`FullSpec`/`PartialSpec`/`AllSpec`), command-name constants (`NetcoreCommands.cs`), `SyncObjectSingleton` (thread marshalling), `Params` (flag files in `RTC/PARAMS`), `CloudDebug` error dialog. |
| CorruptCore | `CC` | Everything about corruption: `BlastUnit`, `BlastLayer`, `StepActions`, `RtcCore` (generation loop, RNG, settings), engines (`Corruption Engines/`), Blast Generator engines, memory abstraction (`Memory/`, `Memory_Virtual/`, `Memory_RPC/`), `StashKey`, `Stockpile/`, value/limiter lists (`Filtering.cs`, `ListFilters.cs`, `BitlogicListFilter.cs`), `CorruptCoreConnector` (the emulator-side command handler), `FileMemory/` (file corruption for FileStub), `Vault.cs` (file backups for file targets), `Render.cs`. |
| Vanguard | `references/rtcv/Source/Libraries/Vanguard` | `VanguardConnector`: the library an emulator links against. Creates the NetCore client and registers the `VANGUARD` and `CORRUPTCORE` local endpoints. |
| PluginHost | `references/rtcv/Source/Libraries/PluginHost` | MEF-based plugin loading; `IPlugin` with `RTCSide { Server, Client, Both }`. |
| ZombieCeras | `references/rtcv/Source/Libraries/ZombieCeras` | Vendored Ceras binary serializer: the NetCore wire format and deep cloning (`ObjectCopierCeras`). |
| UI | `UI` | WinForms front end (`RTCV.UI`): main `CoreForm`, engine config, Glitch Harvester, Blast Editor, Blast Generator, settings, VMD tools, and `UIConnector`/`VanguardImplementation` (the UI-side message handlers). |
| StandaloneRTC | `references/rtcv/Source/Frontend/StandaloneRTC` | The executable (`StandaloneRTC.exe`). Single-instance mutex, then loads the UI. |
| HexEditor plugin | `references/rtcv/Source/Plugins/HexEditor` | Hex editor plugin, used when the emulator has no integrated hex editor (`VSPEC.USE_INTEGRATED_HEXEDITOR`). |
| Tests | `references/rtcv/Tests/CorruptCore` | Unit tests for CorruptCore. |

Process model (detached mode, the normal one): **two processes**. `StandaloneRTC.exe` runs UI + a UI-side copy of CorruptCore. The emulator process loads CorruptCore + Vanguard (via C++/CLI in melonDS) and does all memory work. There is also an "attached" mode (`-ATTACHED` argument, `RtcCore.Attached`) where both sides run in the emulator process with no network.

## 2. Core concepts and data model

### 2.1 Memory domains

`CC/Memory/IMemoryDomain.cs` is what an emulator implements:

```csharp
string Name; long Size; int WordSize; bool BigEndian;
byte PeekByte(long addr); byte[] PeekBytes(long address, int length); void PokeByte(long addr, byte val);
```

`CC/Memory/MemoryInterface.cs` (abstract, serializable) is what CorruptCore uses: `long Size`, `int WordSize`, `string Name`, `bool BigEndian`, `bool UsingRPC=false`, `bool ReadOnly=false`, `byte[] WholeArray`, `GetDump()`, `PeekBytes(long start, long endExclusive, bool raw)`, `PokeBytes(long start, byte[] value, bool raw=true)`, `PeekByte`, `PokeByte`.

- `MemoryDomainProxy` (`CC/Memory/MemoryDomainProxy.cs`) wraps an `IMemoryDomain MD` (the `MD` field is `[Exclude]`d from serialization). The emulator puts a `MemoryDomainProxy[]` into `VSPEC.MEMORYDOMAINS_INTERFACES`. On the UI side the proxy arrives without `MD`, and any peek/poke is routed back to the emulator over NetCore (`Remote.DomainPeekByte`, `DomainPokeByte`, `DomainPeekBytes`, `DomainPokeBytes`). Out-of-range peeks return 0 and pokes are ignored. `PeekBytes` loops `PeekByte` and flips the bytes when `!raw && !BigEndian`.
- `IRPCMemoryDomain` (`CC/Memory_RPC`) adds `DumpMemory()`, `UpdateMemory()`, `PokeBytes()`. It is for targets where each peek is expensive: the domain is cached before generation/apply and committed afterwards (`BlastLayer.Apply`, `RtcCore.GenerateBlastLayerOnAllThreads`).
- `MemoryDomains` (`CC/Memory/MemoryDomains.cs`) holds `MemoryInterfaces` (a `Dictionary<string, MemoryDomainProxy>` stored in CorruptCoreSpec `"MEMORYINTERFACES"`, rebuilt from VanguardSpec by `RefreshDomains`) and `VmdPool` (`Dictionary<string, VirtualMemoryDomain>`, synced by hand, not through a spec). `GetInterface(name)` checks real domains first, then VMDs.
- **VirtualMemoryDomain (VMD)** (`CC/Memory_Virtual/VirtualMemoryDomain.cs`) maps virtual addresses to (real domain, real address) pairs. Its name always starts with `"[V]"`. Pointer storage: `PointerDomains`/`PointerAddresses`, compacted into `CompactPointerDomains[]` + `CompactPointerAddresses[][]` (sorted per domain). It is built from a `VmdPrototype` (`VmdName`, `GenDomain`, `BigEndian`, `WordSize`, `PointerSpacer=1`, `Padding`, `AddSingles`, `RemoveSingles`, `AddRanges`, `RemoveRanges` (as `long[2]`, end exclusive), `SuppliedBlastLayer`). Only the prototype crosses the network (`Remote.DomainVMDAdd`, `DomainVMDRemove`, `PushVMDProtos`) and each side calls `Generate()` itself. `.vmd` files are a gzip'd BinaryFormatter dump (`ToData`/`FromData`).
- `GetRomParts(system, romFilename)` maps a system to its ROM domain(s) and header skip (BizHawk-specific). It is used to diff corrupted ROM domains into a "raw" blast layer.

### 2.2 BlastUnit (`CC/BlastUnit.cs`)

One corruption instruction. Serialized fields:

| Field | Type | Meaning |
|---|---|---|
| `IsEnabled` | bool (=true) | Disabled units are skipped by Apply/Execute. |
| `IsLocked` | bool | Batch operations (reroll, disable 50%, invert, sanitize duplicates) skip locked units. |
| `BigEndian` | bool | Flip the value bytes before poking. |
| `Domain` | string | Target domain. May be a `[V]` VMD. |
| `Address` | long | Target address. |
| `Precision` | int | Byte width, capped at 16348. Changing it left-pads or truncates `Value`. |
| `Source` | `BlastUnitSource { VALUE, STORE }` | VALUE pokes a fixed byte array. STORE pokes bytes read from `SourceDomain`/`SourceAddress`. |
| `Value` | byte[] | VALUE payload, `Precision` long. JSON uses `ValueString` (hex). |
| `SourceDomain`, `SourceAddress` | string, long | STORE source. |
| `StoreTime` | `StoreTime { IMMEDIATE, PREEXECUTE }` | When the source is sampled: at `Apply()` time, or when the unit enters execution (after `ExecuteFrame` frames). |
| `StoreType` | `StoreType { ONCE, CONTINUOUS }` | ONCE samples once and replays that value. CONTINUOUS samples every frame into a queue and dequeues one per execute, so the destination follows the source live. |
| `TiltValue` | BigInteger | Added to the value (unchecked wraparound, 1/2/4/8-byte integer arithmetic) before poking. For STORE the tilt is added at sample time. |
| `ExecuteFrame` | int | Delay in frames from apply until the unit starts executing. |
| `Lifetime` | int | Number of frames it executes. **0 = infinite.** Default 1. |
| `Loop` | bool | When the lifetime expires, re-apply the unit. |
| `LoopTiming` | int? | If set and not -1, the delay used for each re-apply instead of `ExecuteFrame`. |
| `StoreLimiterSource` | `{ ADDRESS, SOURCEADDRESS, BOTH }` | Which side a STORE unit's limiter check reads. |
| `LimiterTime` | `LimiterTime { NONE, GENERATE, PREEXECUTE, EXECUTE }` | When the limiter list is checked: at generation, once on entering execution, or every frame. |
| `LimiterListHash` | string | The limiter list (by hash). |
| `InvertLimiter` | bool | Apply only if the value does **not** match the list. |
| `GeneratedUsingValueList` | bool | Reroll draws from the value list again. |
| `Note` | string | Free text. |

Working data (`CC/BlastUnitWorkingData.cs`, never serialized): `LastFrame=-1`, `ExecuteFrameQueued`, `ApplyValue` (cached value with tilt applied), `Queue<byte[]> StoreData`.

Key methods:
- `Apply(dontFilter, overrideExecuteFrame)`: creates fresh working data. For STORE+IMMEDIATE it samples now (`ONCE`) or adds the unit to `StepActions.StoreDataPool` (`CONTINUOUS`). It then calls `StepActions.AddBlastUnit` and, unless `dontFilter`, `FilterBuListCollection()`.
- `EnteringExecution()`: runs when the unit leaves the queue. For STORE+PREEXECUTE it samples (ONCE) or joins the StoreDataPool (CONTINUOUS). With `LimiterTime.PREEXECUTE` it checks the limiter, and a failure drops the whole batch.
- `Execute()` (every frame while active): with `LimiterTime.EXECUTE` it checks the limiter first. STORE: peeks `StoreData`, dequeues if CONTINUOUS, and pokes `Precision` bytes (or `PokeBytes` when `UsingRPC`). VALUE: computes `ApplyValue = Value + TiltValue` once (flipped if `BigEndian`) and pokes it every execute. Returns `ExecuteState { EXECUTED, NOTEXECUTED, ERROR, HANDLEDERROR, SILENTERROR }`.
- `GetBakedUnit()` / `GetBackup()`: reads current memory into a VALUE unit (lifetime 1). This is how the "uncorrupt" backup layer is built.
- `Reroll()`: VALUE units get a new random value (full range for the precision, or from the value list if `GeneratedUsingValueList`, or following the Custom engine range/list if `RerollFollowsCustomEngine`). STORE units re-randomize source domain/address and/or domain/address according to `RerollSourceDomain`, `RerollSourceAddress`, `RerollDomain`, `RerollAddress`. Locked units are skipped.
- `GetSubUnit`, `GetBreakdown()`: split into 1-byte units (with tilt baked into VALUE bytes).
- **Rasterization** (`GetRasterizedUnits`, `BlastLayer.RasterizeVMDs`): a unit targeting a VMD is resolved to real domain/address. If the VMD bytes it covers are contiguous in one real domain it stays one unit. Otherwise it is split into 1-byte sub-units, recursively. The source side is resolved the same way.

### 2.3 BlastLayer (`CC/BlastLayer.cs`)

`List<BlastUnit> Layer` plus a shared `Note`. `.bl` files are this object as indented JSON (`BlastTools.SaveBlastLayerToFile`).

`Apply(bool storeUncorruptBackup, bool followMaximums=false, bool mergeWithPrevious=false)`:
1. RPC domains: `DumpMemory()` on the selected domains.
2. If `storeUncorruptBackup`: `StockpileManagerEmuSide.UnCorruptBL = GetBackup()` (current bytes at every unit address) and `CorruptBL = this`, optionally merged with the previous pair. These back the Glitch Harvester "BlastLayer ON/OFF" toggle (`Remote.SetApplyUncorruptBL` / `SetApplyCorruptBL`).
3. `bu.Apply(true)` for each unit, then `StepActions.FilterBuListCollection()` once.
4. If the emulator is not real-time (`VSPEC.SUPPORTS_REALTIME == false`) or uses RPC domains, `StepActions.Execute()` runs immediately. Otherwise units run on the next frame hooks.
5. RPC: `UpdateMemory()`. If `followMaximums`, `StepActions.RemoveExcessInfiniteStepUnits()`.

Other methods: `GetBakedLayer()`, `GetBackup()`, `Reroll()`, `SanitizeDuplicates()` (keeps only the last unlocked unit per (domain, address)).

### 2.4 StepActions: the per-frame execution engine (`CC/StepActions.cs`)

It runs **inside the emulator process** and is called once per frame by the hook (`RtcClock.StepCorrupt(executeActions: true, performStep: true)`).

- Units are batched into `List<BlastUnit>` groups sharing `ExecuteFrameQueued`, `Lifetime`, `Loop` and, for PREEXECUTE limiters, the same limiter hash, inversion and address (`GetBatchedLayer`). There are four collections: `buListCollection` (new batches), `queued` (a LinkedList sorted by `ExecuteFrameQueued`), `appliedLifetime`, and `appliedInfinite`.
- `AddBlastUnit`: `ExecuteFrameQueued = currentFrame + ExecuteFrame` (or `+ LoopTiming` for loop re-applies) and `LastFrame = ExecuteFrameQueued + Lifetime - 1`. Non-real-time emulators force `ExecuteFrameQueued=0, LastFrame=1`.
- `Execute()` per frame, under `executeLock`:
  1. Fire `StepStart`.
  2. If running: `CheckApply()` moves every due batch (`currentFrame >= nextFrame`) through `EnteringExecution`. Batches with `Lifetime==0` go to `appliedInfinite`, the rest to `appliedLifetime`.
  3. `GetStoreBackups()` samples every CONTINUOUS store unit.
  4. Fire `StepPreCorrupt`, execute all `appliedLifetime` and then all `appliedInfinite` units, and fire `StepPostCorrupt`.
  5. `currentFrame++`. Remove lifetime batches whose `LastFrame == currentFrame` (pre-increment value). If a removed batch has `Loop`, re-`Apply(true, loopTiming)` its units and re-filter.
  6. Fire `StepEnd`.
- **Freeze** = STORE unit with Lifetime 0 (Freeze engine) or VALUE unit with Lifetime 0 (Hellgenie): re-poked every frame. **Pipe** = STORE CONTINUOUS with Lifetime 0: copies source to destination every frame.
- `MaxInfiniteBlastUnits` (spec `STEP_MAXINFINITEBLASTUNITS`, default 50): `RemoveExcessInfiniteStepUnits()` drops the oldest entries while `appliedInfinite.Count > max`. It counts **batches**, not units. It is skipped when `LockExecution` (`STEP_LOCKEXECUTION`, default false) is set.
- `ClearStepBlastUnits()` resets everything, including `currentFrame=0`. Called on load state, load game, and `Remote.ClearStepBlastUnits` (before every Glitch Harvester operation). `ClearStepActionsOnRewind` (`STEP_CLEARSTEPACTIONSONREWIND`, default false) is only a flag for emulator implementations that support rewind.
- Also: `GetAppliedInfiniteUnits()`, `GetRawBlastLayer()` (used for "stash raw"), `TryRemoveInfiniteStepUnits(domain,address)`, `InfiniteUnitExists`.

### 2.5 Generation loop (`CC/CorruptCore.cs`, class `RtcCore`)

`GenerateBlastLayer(string[] selectedDomains, long overrideIntensity=-1)`:
- Engine `BLASTGENERATORENGINE` returns `BlastGeneratorEngine.GetBlastLayer()`, which synchronously asks the UI (`Remote.GetBlastGeneratorLayer`) for the Blast Generator's layer. Engine `PLUGIN` returns `SelectedPluginEngine.GetBlastLayer(Intensity)`.
- `intensity = GetIntensity()`. It is capped at `MaxInfiniteBlastUnits` for HELLGENIE, FREEZE, PIPE, and CUSTOM with `CustomEngine.Lifetime == 0`.
- Domain sizes, precision (`CachedPrecision`), alignment and engine are cached. Branching on `Radius` (`BlastRadius { SPREAD, CHUNK, BURST, NORMALIZED, PROPORTIONAL, EVEN, NONE }`, default `SPREAD`):
  - `SPREAD`: `intensity` times, pick a random selected domain and a random address in `[0, size - precision]`.
  - `CHUNK`: pick one random domain, then `intensity` random addresses in it.
  - `BURST`: 10 times: pick a random domain, `intensity/10` addresses.
  - `NORMALIZED`: sort domains by size. For domain i, `intensity / (largestSize / size_i)` addresses (small domains get fewer).
  - `PROPORTIONAL`: domain i gets `intensity * size_i / totalSize` addresses.
  - `EVEN`: each domain gets `intensity / domainCount` addresses.
  - `NONE`: returns null.
- Each (domain, address) goes to `GetBlastUnits(domain, address, precision, alignment, engine)`, which dispatches to the engine's `GenerateUnit`. An engine may return null (e.g. a limiter miss), so the unit count can be below intensity.
- `GenerateBlastLayerOnAllThreads()` splits intensity across `ProcessorCount` tasks when `VSPEC.SUPPORTS_MULTITHREAD` is set.
- Address alignment used by every engine: `safeAddress = address - (address % precision) + alignment`. If `safeAddress > size - precision`, it becomes `size - 2*precision + alignment`. `Alignment` is `CORE_CURRENTALIGNMENT` (default 0), `CurrentPrecision` is `CORE_CURRENTPRECISION` (default 1; UI offers 1/2/4/8 bytes).
- Entry points:
  - **Manual blast**: `RtcCore.GenerateAndBlast()` runs `GenerateBlastLayerOnAllThreads()` and `Apply(false, true)` on the emu thread (on the main thread when `LOADSTATE_USES_CALLBACKS`).
  - **Auto-corrupt**: `RtcClock.StepCorrupt` increments `cpuStepCount` each frame. When `AutoCorrupt && cpuStepCount >= ErrorDelay`, it resets the counter, generates and `Apply(false,false)`. So **ErrorDelay = frames between auto blasts** (`CORE_ERRORDELAY`, default 1) and **Intensity = units per blast** (`CORE_INTENSITY`, default 1).
  - **Glitch Harvester corrupt**: `Basic.GenerateBlastLayer` (below).
- Failure: the error dialog is shown and auto-corrupt is switched off (`Basic.ErrorDisableAutoCorrupt` to the UI).

### 2.6 Corruption engines (`CC/Corruption Engines/`)

`CorruptionEngine { NIGHTMARE, HELLGENIE, DISTORTION, FREEZE, PIPE, VECTOR, CLUSTER, BLASTGENERATORENGINE, CUSTOM, PLUGIN, NONE }`. Default `NIGHTMARE`.

| Engine | Params (spec key, default) | Unit produced |
|---|---|---|
| Nightmare | `NIGHTMARE_ALGO` = `NightmareAlgo.RANDOM` (`RANDOM`, `RANDOMTILT`, `TILT`). Min/max per width: `NIGHTMARE_MIN/MAXVALUE{8,16,32,64}BIT` = 0 / 0xFF, 0xFFFF, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF | Algo picks a `NightmareType`: RANDOM always `SET`; RANDOMTILT picks ADD, SUBTRACT or SET with 1/3 chance each; TILT picks ADD or SUBTRACT at 1/2 each. **SET**: VALUE unit, random in [min,max] inclusive for precision 1/2/4/8 (other precisions: random bytes), lifetime 1, frame 0. **ADD/SUBTRACT**: STORE unit, source = the same address, `ONCE`, `PREEXECUTE`, `TiltValue = +1/-1`, lifetime 1. This reads the current value and writes back value±1. |
| Hellgenie | `HELLGENIE_MIN/MAXVALUE{8..64}BIT`, same defaults | VALUE unit with a random value (as Nightmare SET) and **lifetime 0**: a random cheat re-poked every frame. |
| Distortion | `DISTORTION_DELAY` = 50 | STORE, source = the same address, `ONCE`, **`IMMEDIATE`**, `ExecuteFrame = Delay`, lifetime 1. Samples the byte now and writes the stale value back `Delay` frames later. |
| Freeze | none (uses precision/alignment) | STORE, source = the same address, `ONCE`, `PREEXECUTE`, lifetime 0. Freezes the current value forever. |
| Pipe | none | Source = random `GetBlastTarget()` (any selected domain), destination = the address. STORE `CONTINUOUS`, `PREEXECUTE`, lifetime 0. Copies source to destination every frame. |
| Vector | `VECTOR_LIMITERLISTHASH` = "", `VECTOR_VALUELISTHASH` = "", `VECTOR_UNLOCKPRECISION` = false | Precision = limiter list's item length (or `CachedPrecision` if unlocked). Peeks the address: if the bytes are in the limiter list, emits a VALUE unit with a random value-list entry (`GetRandomConstant(valueHash, precision, matchBytes)`), lifetime 1, `GeneratedUsingValueList=true`. Otherwise null. Designed for float/instruction swapping (e.g. "Extended" float lists). |
| Cluster | `CLUSTER_LIMITERLISTHASH` = "", `CLUSTER_SHUFFLETYPE` = "Random" (`Random`, `Reverse`, `Rotate Forwards`, `Rotate Backwards`, `Overwrite`), `CLUSTER_SHUFFLEAMT` (chunk size) = 3, `CLUSTER_MODIFIER` (rotation count) = 1, `CLUSTER_MULTIOUT` = true, `CLUSTER_FILTERALL` = false, `CLUSTER_DIR` = "Forwards" (`Forwards`, `Backwards`) | Precision = limiter list item length. Takes `chunkSize` consecutive elements from the aligned address. Requires the first element (or the last, for Backwards; or all, if FilterAll) to match the limiter. Reads the elements, then shuffles, reverses, rotates by `Modifier`, or overwrites all with the first/last element. Output: one VALUE unit per element (MULTIOUT) or one unit of `precision*chunkSize`, lifetime 1, never endian-flipped. |
| Blast Generator | none | Uses the Blast Generator form's layer (see 3.5). |
| Custom | see below | Fully parameterised unit. |
| Plugin | n/a | `ICorruptionEngine.GetBlastLayer(intensity)` from a plugin. |

**Custom engine** (`CustomEngine.cs`). Spec keys and defaults are those of the "Nightmare Engine" template:
`CUSTOM_DELAY`=0 (ExecuteFrame), `CUSTOM_LIFETIME`=1, `CUSTOM_LOOP`=false, `CUSTOM_TILTVALUE`=0, `CUSTOM_LIMITERTIME`=`NONE`, `CUSTOM_LIMITERLISTHASH`, `CUSTOM_LIMITERINVERTED`=false, `CUSTOM_STORELIMITERMODE`=`ADDRESS`, `CUSTOM_MIN/MAXVALUE{8,16,32,64}BIT` (full range), `CUSTOM_VALUESOURCE`=`CustomValueSource.RANDOM` (`RANDOM`, `VALUELIST`, `RANGE`), `CUSTOM_VALUELISTHASH`, `CUSTOM_SOURCE`=`BlastUnitSource.VALUE`, `CUSTOM_STOREADDRESS`=`CustomStoreAddress.RANDOM` (`SAME`, `RANDOM`), `CUSTOM_STORETIME`=`IMMEDIATE`, `CUSTOM_STORETYPE`=`ONCE`, plus `CORE_CURRENTPRECISION`, `CORE_CURRENTALIGNMENT`, `CUSTOM_NAME`, `CUSTOM_PATH`.
- VALUE source: RANDOM gives random bytes. RANGE gives [min,max] for 1/2/4/8 bytes. VALUELIST gives `GetRandomConstant(valueList, precision, currentBytes)`. STORE source: StoreType/StoreTime as set, with the source either the same address or a random `GetBlastTarget()`.
- All the other unit fields are copied from the spec. With `LimiterTime.GENERATE`, the limiter check happens right away and a miss drops the unit.
- Built-in templates (`InitTemplates`) express the other engines as Custom settings: Nightmare (VALUE/RANDOM, lifetime 1), Hellgenie (VALUE/RANDOM, lifetime 0), Distortion (STORE SAME IMMEDIATE ONCE, delay 50, lifetime 1), Freeze (STORE SAME PREEXECUTE ONCE, lifetime 0), Pipe (STORE RANDOM PREEXECUTE CONTINUOUS, lifetime 0), Vector (VALUE/VALUELIST, precision 4, limiter GENERATE).
- User templates are `.cet` files: JSON of the PartialSpec dictionary, in `RTC/ENGINETEMPLATES`.

### 2.7 Randomness

- `RtcCore.RND` is a `ThreadLocal<Random>`. Each thread's seed is `Interlocked.Increment(seed)`, and the initial seed is `(int)DateTime.Now.Ticks` (`ResetSeed()` re-reads the clock). **No user-controllable seed** in the core generation path, and blast layers are not reproducible from a seed. The generated `BlastLayer` itself is the reproducible artifact.
- `NextULong(min,max,inclusive)` and `NextLong` are bias-free rejection sampling (`references/rtcv/Source/Libraries/Common/Extensions/RandomExtensions.cs`).
- `GetRandomKey()` makes stash-key IDs by concatenating four `RND.Next(1,9999)` values.
- Blast Generator rows each carry an `int Seed` used as `new Random(Seed)`, so BG output is reproducible. See 3.5.

### 2.8 Lists: limiter and value lists (`CC/Filtering.cs`, `CC/ListFilters.cs`, `CC/BitlogicListFilter.cs`)

- A list is a text file, one hex value per line (e.g. `3F800000`), loaded from `RTC/LISTS` and `<emuDir>/LISTS` (`UICore.LoadLists`, `*.txt`). Every list is registered as **both** a limiter list and a value list.
- The key is `Base64(MD5(concatenated bytes))`. Units reference lists only by this hash (`LimiterListHash`). Registries (CorruptCoreSpec): `FILTERING_HASH2LIMITERDICO`, `FILTERING_HASH2VALUEDICO` (`ConcurrentDictionary<string, IListFilter>`), and `FILTERING_HASH2NAMEDICO` (hash to name).
- Filename starting with `_`: values are stored big-endian and flipped on load. Comparison is done little-endian: memory bytes are flipped if the domain is big-endian.
- Types (`IListFilter`: `GetHash`, `ContainsValue`, `GetRandomValue(hash, precision, passthrough)`, `GetPrecision`, `GetStringList`):
  - `ValueByteArrayList`: exact byte arrays in a HashSet.
  - `NullableByteArrayList`: chosen when any line contains `?` (wildcard nibble or byte).
  - `BitlogicListFilter`: file starts with `@BitlogicListFilter`. Supports hex/binary, `?` wildcard, `#` passthrough (keep the original bits from `passthrough`), `!` exclusions, and header flags `@v1.0` / `@forceflip`.
- `GetRandomValue` left-pads or truncates the chosen entry to `precision`. `GetPrecision()` is the length of the first entry.
- Stockpiles embed their used limiter lists as `<hash>.limiter` files. `StashKey.KnownLists` maps hash to name. Missing lists get registered as `MISSING_<name>`.

### 2.9 StashKey, savestates, stockpiles

`StashKey` (`CC/StashKey.cs`) is one "glitch": a savestate reference + a blast layer + game identity.

| Field | Meaning |
|---|---|
| `Key` | Random ID (`GetRandomKey`). |
| `ParentKey` | Key of the savestate this corruption is based on. The state file is named after it. |
| `Alias` | Display name (defaults to `Key`). |
| `BlastLayer` | The corruption. |
| `RomFilename`, `RomShortFilename`, `RomData` | Game file (RomData is normally unused). |
| `StateFilename`, `StateShortFilename`, `StateData` | Savestate path; `StateData` when embedded. |
| `StateLocation` | `StashKeySavestateLocation { SKS, SSK, MP, SESSION, DEFAULTVALUE }` (default `SESSION`). The subfolder of `RTC/WORKING` where the state file lives. |
| `SystemName`, `SystemDeepName`, `SystemCore`, `GameName`, `SyncSettings` | Copied from VanguardSpec (`SYSTEM`, `SYSTEMCORE`, `GAMENAME`, `SYNCSETTINGS`). |
| `SelectedDomains` | `List<string>`, copied from `UISPEC.SELECTEDDOMAINS`. |
| `KnownLists` | `Dictionary<hash,name>` for limiter lists used. |
| `Note` | Free text. |

- Savestate file path convention: `RTC/WORKING/<StateLocation>/<GameName>.<ParentKey>.timejump.State` (`GetSavestateFullPath`). The **emulator** writes the state (`Basic.SaveSavestate` with the key) into `WORKING/SESSION` and returns the full path. The RTC process and the emulator must share a filesystem (paths are exchanged, not bytes).
- `SaveStateKey { StashKey, Text }` and `SaveStateKeys { VanguardImplementation, List<StashKey> StashKeys, List<string> Text }` hold the savestate manager's boxes. A `.ssk` file is a zip of `keys.json` (SaveStateKeys JSON) + the `.State` files (`UI/Components/Glitch Harvester/SavestateManagerForm.cs`).
- `Stockpile` (`CC/Stockpile/Stockpile.cs`): `List<StashKey> StashKeys`, `Filename`, `RtcVersion`, `VanguardImplementation`, `MissingLimiter`. A **`.sks` file is a zip** (`ZipFile.CreateFromDirectory` of `WORKING/TEMP`, Fastest compression by default) containing:
  - `stockpile.json`: Stockpile as indented Newtonsoft JSON.
  - `<GameName>.<ParentKey>.timejump.State` for each key (only if `SUPPORTS_SAVESTATES`).
  - `<hash>.limiter` lists.
  - Referenced ROMs, if "include referenced files" and `SUPPORTS_REFERENCES` (cue/bin paths fixed to relative).
  - `CONFIGS/*` emulator config files from `VSPEC.CONFIG_PATHS`, if `SUPPORTS_CONFIG_MANAGEMENT`.
  
  Load extracts into `WORKING/SKS` (import: `WORKING/TEMP`, then merged). It checks `VanguardImplementation` (a mismatch is fatal) and `RtcVersion`, rewrites ROM paths, and sets `StateLocation = SKS`.
- Disk-based games (`VSPEC.CORE_DISKBASED`) must be closed before saving or loading (`Remote.CloseGame`).
- `.bl` files: BlastLayer JSON. `.cet`: engine template JSON. `.vmd`: VMD binary. Lists: `.txt`.

### 2.10 Glitch Harvester workflow (`CC/Stockpile/StockpileManagerUISide.cs`, `StockpileManagerEmuSide.cs`)

State kept UI-side: `CurrentSavestateStashKey` (the selected savestate box), `CurrentStashkey`, `LastStashkey`, `StashHistory` (list), `StashAfterOperation=true`, `BackupedState`.

- **Save state** (`SaveState`): `Remote.SaveState` to the emu side, which calls `SaveStateNET` and sends `Basic.SaveSavestate(key)` to the emulator. The returned path goes into a new StashKey (`Key == ParentKey`), which fills the selected savestate box. Stateless emulators use `Remote.SaveStateless` instead, which creates a key with empty state paths.
- **Load state** (`LoadState(sk, reloadRom, applyBlastLayer)`, `Remote.LoadState`): emu side runs `LoadRomNet` (`KeySetSystemCore`, `LoadROM`, and a sync-settings reload if they differ) and then `LoadStateNet`. That sends `Basic.LoadSavestate([path, location])` and, if requested, applies the key's layer with `Apply(true)` (backup stored).
- **Corrupt** (`Corrupt(loadBeforeOperation)`):
  1. `PreApplyStashkey`: `Remote.ClearStepBlastUnits`, then `Remote.PreCorruptAction`.
  2. Build a new StashKey from the savestate key's identity (new Key, same ParentKey).
  3. `Basic.GenerateBlastLayer([sk, loadBeforeOperation, apply=true, backup=true])` to the emu side. Under `LoadLock` this reloads ROM+state, then `GenerateBlastLayerOnAllThreads()`, then `Apply(true)`, and returns the layer. With `LOADSTATE_USES_CALLBACKS` it also sends `ResumeEmulation`.
  4. Add to `StashHistory` if `StashAfterOperation`. Then `PostApplyStashkey`: optional render, `Remote.PostCorruptAction`, UI hooks.
- **Inject** (`InjectFromStashkey`): reuse a stash/stockpile key's BlastLayer on the currently selected savestate (a core mismatch is refused unless `AllowCrossCoreCorruption`).
- **Original** (`OriginalFromStashkey`): load the key's savestate without applying its layer.
- **Merge** (`MergeStashkeys`): concatenate the layers of 2+ keys (distinct units) on the first key's state. Requires the same core/game unless cross-core corruption is allowed.
- **Load (run) a stash/stockpile item**: `StashKey.Run()` → `ApplyStashkey`: load its state and apply its layer. With "load before operation" off, the layer is sent via `Basic.ApplyBlastLayer([bl, true, merge])` onto the running game instead.
- **Reroll**: `Remote.RerollBlastLayer` rerolls a copy emu-side (needs memory access for value lists) and returns it. The UI then makes a new stash key and runs it.
- **BlastLayer toggle ON/OFF**: `Remote.SetApplyUncorruptBL` (apply the backup layer = uncorrupt in place) vs `Remote.SetApplyCorruptBL` (re-apply the cached corrupt layer). `Remote.ClearBlastlayerCache` clears both.
- **Stash raw / get raw layer**: `Remote.KeyGetRawBlastLayer` saves state and takes `StepActions.GetRawBlastLayer()` plus a diff of ROM domains against the ROM file (`BlastTools.GetBlastLayerFromDiff`).
- **Game protection** (UI side, `UI/GameProtection.cs`): every `BackupInterval` seconds (default 5) it sends `Remote.BackupKeyRequest`. The emu side saves a state and sends `Remote.BackupKeyStash` back asynchronously, and the UI keeps a LinkedList of backup keys. "Go back" pops and loads the latest one. Reset on game change (`Basic.ResetGameProtectionIfRunning`).
- **Auto kill switch** (`UI/AutoKillSwitch.cs`): the emulator sends `Basic.KillswitchPulse` over UDP every 250 ms (`RtcCore.StartEmuSide`, when `SUPPORTS_KILLSWITCH`). The UI decrements a counter every 500 ms. After 25 missed pulses (or on connection loss) it restarts NetCore and runs `<emuDir>/RESTARTDETACHEDRTC.bat` to relaunch a frozen or crashed emulator.

### 2.11 Specs: the shared settings model (`NC/UniSpec.cs`, `NC/AllSpec.cs`)

A spec is a named `ConcurrentDictionary<string, object>` with a version. `FullSpec(template, propagationEnabled)` holds state. `Update(PartialSpec | key,value, propagate=true, synced=true)` merges keys (a null value removes a key) and fires `SpecUpdated(partial, synced)`. Handlers ship the **partial** (delta) to the other process, where it is applied with `propagate=false` to avoid echo. There are four specs in `AllSpec`:

| Spec (name) | Owner | Synced to | Contents |
|---|---|---|---|
| `VanguardSpec` ("VanguardSpec") | Emulator | UI + emu-side CC | `VSPEC.*`: `NAME`, `SYSTEM`, `SYSTEMPREFIX`, `SYSTEMCORE`, `GAMENAME`, `OPENROMFILENAME`, `SYNCSETTINGS`, `EMUDIR`, `MEMORYDOMAINS_INTERFACES` (`MemoryDomainProxy[]`), `MEMORYDOMAINS_BLACKLISTEDDOMAINS`, `CORE_DISKBASED`, `CONFIG_PATHS`, capability flags `SUPPORTS_SAVESTATES`, `SUPPORTS_REALTIME`, `SUPPORTS_RENDERING`, `SUPPORTS_REFERENCES`, `SUPPORTS_KILLSWITCH`, `SUPPORTS_GAMEPROTECTION`, `SUPPORTS_MULTITHREAD`, `SUPPORTS_MIXED_STOCKPILE`, `SUPPORTS_CONFIG_MANAGEMENT`, `SUPPORTS_CONFIG_HANDOFF`, `USE_INTEGRATED_HEXEDITOR`, `LOADSTATE_USES_CALLBACKS`, `OVERRIDE_DEFAULTMAXINTENSITY`, `RENAME_SAVESTATE`, `REPLACE_MANUALBLAST_WITH_GHCORRUPT`. |
| `CorruptCoreSpec` ("RTCSpec") | UI (`RtcCore.RegisterCorruptcoreSpec`) | Emu side (full push on connect, then deltas both ways) | All `RTCSPEC.*`: `RTCDIR`, `RTCVERSION`, engine selection and params, `CORE_INTENSITY`, `CORE_ERRORDELAY`, `CORE_RADIUS`, `CORE_CURRENTPRECISION`, `CORE_CURRENTALIGNMENT`, `CORE_AUTOCORRUPT`, reroll flags, `STEP_*`, `FILTERING_*` (the list objects themselves), `MEMORYINTERFACES`, `RENDER_*`, custom-engine keys, `CLUSTER_*`. |
| `UISpec` ("UISpec") | UI | Emu side | `UISPEC.SELECTEDDOMAINS` (`string[]`), `UISPEC.SELECTEDDOMAINS_FORCAVESEARCH`, `Basic.RTCInFocus` (bool; lets the emulator decide whether to handle hotkeys). |
| `PluginSpec` ("PluginSpec") | UI | Emu side | Plugin-defined keys. |

Handshake order is covered in 4.3.

## 3. UI (`UI`, WinForms)

### 3.0 Look and layout
- **Shell**: one main window, `CoreForm` (`UI/Modular/CoreForm.cs`, title "Real-Time Corruptor"). It has a **left sidebar** of flat buttons and a main area that hosts a **tiled grid** (`CanvasGrid`, `CanvasForm`). Tools are `ComponentForm` subforms placed on a 15x12 cell grid (`UI/Modular/DefaultGrids.cs`). A grid can load into the main area or pop out as its own window (the Glitch Harvester opens in a separate window by default). The sidebar also has "Custom Layout", which loads user layouts from `RTC/LAYOUTS`.
- **Theme**: always dark. A single user-selectable "general color" (default RGB 110,150,193, a steel blue; changed via Settings > "Change color theme") is darkened by 50%. Shades are then derived from it: `light2` +45%, `light1` +10%, `normal`, `dark1` -20%, `dark2` -35%, `dark3` -50%, `dark4` -85%. Each control carries a `Tag` like `"color:dark1"` that picks its back color (labels get it as fore color, buttons as border color). Text is white or light gray, the font is Segoe UI 8.25pt, buttons are flat with icons, and panels have drop shadows (`ShadowPanel`). Source: `UI/Colors.cs`.
- **Recurring widgets**:
  - `MultiTrackBar` (`UI/Components/Controls/MultiTrackBar.cs`): a labelled slider plus numeric box. It uses a non-linear slider scale and a numeric box that can be uncapped.
  - Savestate "boxes" (`SavestateHolder`).
  - DataGridViews with numeric-up-down cells.
  - Right-click context menus are used heavily for secondary actions.
  - Hex numeric boxes.

### 3.1 CoreForm sidebar (`UI/Modular/CoreForm.cs`)
| Control | Action |
|---|---|
| Logo button "RTCV 5.1.0" | Shows the Connection Status grid (connecting / connected / timed out). |
| " Easy Start" | Menu: "Switch to Simple Mode" (3.8), or "Start Auto-Corrupt with Recommended Settings for loaded game" (per-system engine/intensity/delay presets, then auto-corrupt on), plus tutorial/wiki links. |
| " Engine Config" | Engine Config grid (3.2). |
| " Glitch Harvester" | GH grid (3.3). Right-click chooses between main-area and separate-window placement. |
| " Stockpile Player" | Stockpile Player grid (3.8). |
| " Settings and tools" | Settings grid (3.7). |
| " Custom Layout" | Load a saved layout. |
| " Manual Blast" | `Basic.ManualBlast`: generate with the current settings and apply to the running game. If the emulator is not real-time, it runs a GH corrupt instead (`REPLACE_MANUALBLAST_WITH_GHCORRUPT`). |
| " Start/Stop Auto-Corrupt" | Toggles `RtcCore.AutoCorrupt` (a spec flag; the emulator side fires a blast every ErrorDelay frames). |
| "Game Protection" toggle + " Back" / "Now " | Enable periodic backup states. "Back" loads the previous backup; "Now" takes a backup immediately. |
| "Auto-KillSwitch" toggle + timeout bar | Enable auto-restart of a hung emulator. The bar shows missed pulses. |

### 3.2 Engine Config grid
| Tile | Controls |
|---|---|
| **General Parameters** (`UI/Components/Engine Config/GeneralParametersForm.cs`) | `Intensity` MultiTrackBar (1–65535, default 1; the max can be overridden by `VSPEC.OVERRIDE_DEFAULTMAXINTENSITY`). `Error Delay` MultiTrackBar (1–65535, default 1). "Blast Radius:" combo with `SPREAD`, `CHUNK`, `BURST`, `EVEN`, `PROPORTIONAL`, `NORMALIZED`. |
| **Corruption Engine** (`CorruptionEngineForm.cs`) | Engine combo: "Nightmare Engine", "Hellgenie Engine", "Distortion Engine", "Freeze Engine", "Pipe Engine", "Vector Engine", "Cluster Engine", "Custom Engine", "Blast Generator", plus plugin engines. "Engine Precision:" combo (8/16/32/64-bit), "Alignment:" numeric. Below these, an engine-specific panel (`EngineControls/*`, next table). |
| **Memory Domains** (`MemoryDomainsForm.cs`) | Multi-select list of real domains and VMDs; the selection is written to `UISPEC.SELECTEDDOMAINS`. Buttons: "Auto-select domains" (all minus `MEMORYDOMAINS_BLACKLISTEDDOMAINS`), "Select all", "Unselect all". Right-click menu: Limiter Profiler, which generates or regenerates VMDs of the addresses matching a limiter list per domain, with "Load GH State on Generate". |
| **Tools** (`UICore.mtForm`, `SelectBoxForm`) | Dropdown switching between the advanced tools of 3.9. Default: "No Tool Selected (Shortcuts)". |

Engine-specific panels (`UI/Components/Engine Config/EngineControls/`):
| Engine | Controls |
|---|---|
| Nightmare | "Blast type:" combo (RANDOM / RANDOMTILT / TILT), "Minimum Value" / "Maximum Value" (for the current precision). |
| Hellgenie | Min / Max value, "Max ∞ Units" (MaxInfiniteBlastUnits), "Clear all cheats" (ClearStepBlastUnits), "Clear step units on Rewind". |
| Distortion | "Distortion delay:" numeric (default 50), "Resync Distortion Engine" (clears step units). |
| Freeze | "Max ∞ Units", "Clear all freezes", "Clear step units on Rewind". |
| Pipe | "Max ∞ Units", "Clear Pipes", "Lock step units" (`LockExecution`), "Clear step units on Rewind". |
| Vector | "Limiter list:" combo ("Comparison values"), "Value list:" combo ("Replacement values"), "Unlock" (precision). |
| Cluster | "Limiter list:", "Cluster Chunk Size:", "Method:" (Random / Reverse / Rotate Forwards / Rotate Backwards / Overwrite), "Rotate Amount:", "Cluster Direction:", "Split Blast Units" (= MULTIOUT), "Filter All". |
| Custom | "Open Custom Engine" button (3.6). |
| Blast Generator | "Open Blast Generator" button (3.5). |

### 3.3 Glitch Harvester grid (20x12)
| Tile | Controls |
|---|---|
| **Blast Tools** (`UI/Components/Glitch Harvester/GlitchHarvesterBlastForm.cs`) | Main button "Corrupt". Its label follows the mode (Corrupt / Inject / Original), and it becomes "Merge" when several stockpile rows are selected. "Reroll Selected" (right-click: "Configure Reroll", which opens the reroll settings). "BlastLayer : ON/OFF" toggles uncorrupt/recorrupt in place (`SetApplyUncorruptBL` / `SetApplyCorruptBL`). "Raw to Stash" (`KeyGetRawBlastLayer`; right-click: "Blast + Send RAW To Stash"). A settings (gear) menu has: "Glitch Harvester Mode" (Corrupt / Inject / Original), and "Behaviors": "Auto-Load State" (= `loadBeforeOperation`), "Load on select", "Stash results" (= `StashAfterOperation`). A render menu has "Start/Stop rendering", "Open RENDEROUTPUT Folder", type WAV / AVI / MPEG, and "Render file at load". |
| **Generator Control** (`GlitchHarvesterIntensityForm`) | A second Intensity slider bound to the same spec value. It shows "Parameters unavailable with current engine" when the engine does not use it. |
| **Savestate Manager** (`SavestateManagerForm`, `UI/Components/Controls/SavestateList.cs`) | A paged column of savestate boxes (number of boxes set by panel height, with back/forward paging). Each box is a numbered button plus an editable text label. Clicking a box selects it (= `CurrentSavestateStashKey`). The "SAVE/LOAD" mode toggle decides whether a click saves into or loads from the box. "Load state on click" checkbox. Right-click a box: "New Savestate", "Delete entry", "Import State from File", "Import State from selected Stockpile Item", "New Blastlayer from this Savestate (Blast Editor)". Buttons: " Load" (right-click: "Import SSK", "Clear Savestate List") and " Save" (to a `.ssk`). |
| **Stash History** (`StashHistoryForm`) | List of the StashKeys produced by operations. Clicking one runs it (loads state + applies layer) when "Load on select" is on. Buttons: "▲"/"▼" (move the selection, wrapping), " To Stockpile" (asks for a name, rasterizes VMDs, moves the item into the stockpile), "  Clear". Right-click: "Open Selected Item in Blast Editor", "Sanitize", "Generate VMD from Selected Item", "Merge Selected Stashkeys", "Rename selected item", "[Multiplayer] Pull State from peer". |
| **Stockpile Manager** (`StockpileManagerForm`) | Grid with columns "Item Name", "Game", "System", "Core", "Note" (the Note cell opens the note editor). Clicking a row runs it; multi-select switches the GH to Merge. Buttons: " Load" (`.sks`), " Save", " Save as", " Import" (merge another `.sks`), " Clear stockpile", " Remove Item(s)", " Rename Item", "▲"/"▼" (move selection), "▲▲"/"▼▼" (move the selected items). Right-click: "Open Selected Item in Blast Editor", "Sanitize", "Manual Inject", "Generate VMD from Selected Item", "Merge Selected Stashkeys", "Replace associated ROM", multiplayer sends. Settings menu: "Include referenced files", "Compress Stockpiles", and show/hide per column. |

### 3.4 Blast Editor (`UI/Forms/BlastEditorForm.cs`, separate window)
Opened for one StashKey (from the stash, stockpile or a savestate). It edits a copy of the key's BlastLayer.
- **Grid**: one row per BlastUnit, with columns `Enabled`, `Locked`, `Domain`, `Address`, `Precision`, `Value` (hex), `Source`, `Execute Frame`, `Lifetime`, `Loop`, `Loop Timing`, `Limiter Time`, `Limiter List`, `Invert Limiter`, `Store Time`, `Store Comparison` (=StoreLimiterSource), `Store Type`, `Source Domain`, `Source Address`, `Note`. The visible columns are chosen via "Select columns to show". A filter bar (column combo + text box) filters rows. Row right-click: "Open Selected Address in Hex Editor", "Bake Selected Unit(s) to VALUE", "Break Down Selected Unit(s)", "Re-roll Selected Row(s)".
- **Side panel**: property editors for the selected row(s): Domain, Address, Precision, Source, Value, Tilt Value, Execute Frame, Lifetime, Loop, Loop Timing, Store Time, Store Type, Source Domain/Address, Limiter Time, Limiter List, Store Comparison, and checkboxes Enabled / Locked / Big Endian / Invert Limiter. Multi-row edits are batch updates.
- **Buttons**: "Disable 50%" (randomly disable half of the enabled unlocked units, for bisecting which units cause the glitch), "Invert Disabled", "Remove Disabled", "Enable Everything", "Disable Everything", "Remove Selected", "Duplicate Selected", "Add New Row", "Shift Selected Rows" ▲/▼ (adds/subtracts an amount to a chosen field: Address, Source Address, Value, Lifetime or Execute Frame), "Load + Corrupt" (load the key's state, then apply), "Apply Corruption" (apply to the current game), "Send To Stash", " To Stockpile", "Sanitize Tool", "Open Note Editor". The label "Layer size:" shows the unit count.
- **Menus**:
  - File: "&New", "&Load From File (.bl)", "&Save to File (.bl)", "&Save As to File (.bl)", "&Import Blastlayer (.bl)", "Import Blastlayer From Corrupted &File" (diff), "&Export to CSV".
  - SaveState: "Run Original Savestate", "Replace Savestate from GH/File", "Save Savestate to".
  - ROM: "Run Rom Without Blastlayer", "Replace Rom from GH/File", "Bake ROM VALUE BlastUnits to File".
  - Tools: "Sanitize Duplicates", "Rasterize VMDs", "Bake All Blastunits to VALUE", "Break Down All Blastunits", "Open Blast Generator".
- **Sanitize Tool** (`UI/Components/Blast Editor/SanitizeToolForm.cs`): guided bisection. Repeatedly disable half of the units and reload; the user answers "Yes effect" / "No effect" until the responsible units remain. "Reroll" and "Leave with changes" / "Leave subtract changes" end the session (commands `Remote.SanitizeTool*`).

### 3.5 Blast Generator (`UI/Forms/BlastGeneratorForm.cs`) and `BlastGeneratorProto`
A grid of "generator rows", classic ROM-corruptor style. Each row becomes a `BlastGeneratorProto` (`CC/BlastGeneratorProto.cs`) with fields `BlastType` ("Value" | "Store"), `Domain`, `Precision`, `StepSize` (column "Interval"), `StartAddress`, `EndAddress`, `Param1` (ulong), `Param2` (ulong), `Mode` (string enum name), `Note`, `Lifetime`, `ExecuteFrame`, `Loop`, `Seed` (int), and the result `bl`.
- **Columns**: ✓ (enabled), Type, Domain, Precision, Mode, Interval, Start Address, End Address, Param 1, Param 2, Lifetime, Execute Frame, Loop, Seed, Note. A side panel has nudge ▲/▼ buttons for Start/End/Param1/Param2 and "Shift Selected Rows".
- **Buttons**: "Add Row", "Refresh Domains", "Load + Corrupt", "Apply Corruption", "Send To Stash", "Units inheret note" checkbox. File menu: load/save/import `.bg` (generation params).
- Generation runs emu-side (`Basic.BlastGeneratorBlast`). For each row: `Random rand = new Random(Seed)` (the row's seed makes Random/RandomRange/SourceRandom/DestRandom reproducible). The address loop is `for (a = Start; a < End; a += StepSize + Precision - 1)`.
- **`BGValueMode`** (VALUE units, `CC/Blast Generator Engines/ValueGenerator.cs`); Param1/Param2 are encoded as `Precision`-byte values:
  - `Set` = Param1.
  - `Add` / `Subtract`: a unit with zero Value and `TiltValue = ±Param1`. As coded, this writes 0±Param1, not memory±Param1.
  - `Random` = random bytes (`rand.Next(0,255)`, so 0–254).
  - `RandomRange` = uniform [Param1, Param2).
  - `ReplaceXWithY`: only where memory == Param1, write Param2.
  - `ShiftLeft` / `ShiftRight`: copy the current value to address ∓/± Param1.
  - `BitwiseAnd` / `BitwiseOr` / `BitwiseXOr` = memory op Param1. `BitwiseComplement` is coded identically to AND.
  - `BitwiseShiftLeft` / `BitwiseShiftRight` / `BitwiseRotateLeft` / `BitwiseRotateRight`: shift or rotate the value Param1 times.
- **`BGStoreMode`** (STORE `CONTINUOUS`, `PREEXECUTE` units, `StoreGenerator.cs`):
  - `Chained`: dest = addr + StepSize, source = addr.
  - `SourceSet`: source = Param1, dest = addr.
  - `SourceRandom`: source random in the domain.
  - `DestRandom`: dest random.
  - `SELF`: source = dest = addr.

### 3.6 Custom Engine config (`UI/Forms/CustomEngineConfigForm.cs`)
Edits every `CUSTOM_*` key from 2.6.
- Template: "Selected Template:" combo (built-ins + `.cet`), "Load", "Save", "Save as".
- "Unit Source": Value / Store.
- "Value Settings": "Value Source" (Random / Range / Value List), "Min Value" / "Max Value", "Value List".
- "Store Settings": "Store Source" (Same / Random), "Start Storing" (Immediate / First Execute), "Store Type" (Once / Continuous).
- "Modifiers": "Tilt".
- "Step Settings": "Delay", "Lifetime", "Loop Generated Units", "Max ∞ Units", "Clear Units on Rewind", "Clear all active units".
- Limiter: "Limiter List", "Limiter Time" (None / Generate / First Execute / Execute), "Inverted", "Store Compare" (Address / Source Address / Both).
- "Engine Precision:" and "Alignment:".

### 3.7 Settings and tools (`UI/Forms/SettingsForm.cs`, `UI/Components/Settings/*`)
- **General**: "Allow Cross-Core / Cross-Game corruption", "Disable the emulator OSD system", "Don't clean savestates at quit", "Uncap intensity box value", "Change color theme", "Reset random seed", "Refresh Input Devices", wiki/tutorial links.
- **Corrupt**: "Reroll Settings" (Reroll Address / Source Address / Domain / Source Domain, "Reroll Follows Custom Engine", "Ignore Unit Origin Mode"), "StepActions Settings" ("Max Infinite Units:", "Lock Units", "Clear Step Units on Rewind").
- **Hotkeys**: rebindable keys/gamepad. Groups:
  - RTC: Manual Blast, Auto-Corrupt, Error Delay±, Intensity±, Induce KS Crash, BlastLayer Toggle, BlastLayer Re-Blast, Game Protect Back/Now.
  - Glitch Harvester: Load and Corrupt, Just Corrupt, Undo and Corrupt, Load Prev State, New Savestate, Reroll, Load, Save, Stash->Stockpile, Blast+RawStash, Send Raw to Stash, Reload Corruption.
  - Blast Editor: Disable 50, Remove Disabled, Invert Disabled, Shift Up/Down, Load Corrupt, Apply, Send Stash.
  
  Hotkeys only fire while the RTC or the emulator has focus.
- **NetCore**: "Game Protection: Backups the game state every N seconds" (1–60, default 5; about 20 backups kept, and the oldest state files are deleted), "Crash sound effect:".
- **My Lists / My VMDs / My Plugins**: file managers for `RTC/LISTS` (`.txt`), `RTC/VMDS` (`.vmd`), `RTC/PLUGINS` (import, rename, remove, disable, open folder, refresh).
- **About**. Buttons: " Toggle Console", " Show Debug Info", "  RTC Factory Clean".

### 3.8 Simple Mode, Stockpile Player, others
- **Simple Mode** (`UI/Forms/SimpleModeForm.cs`): a wizard for novices. "Target Type" radio options:
  - "Classic Platforms (8bit/16bit, 2d games)"
  - "Modern Platforms (32bit/64bit, 3d games)" (Vector engine)
  - "Shuffle algorithm" (Cluster)
  
  Then Manual Blast, Start Auto-Corrupt, "Clear infinite units", and a "Simple Glitch Harvester" section: "Create and select a Glitch Harvester savestate", "Load Savestate", "Load and Corrupt", the BlastLayer toggle, and "Switch to Normal Mode".
- **Stockpile Player** (`UI/Forms/StockpilePlayerForm.cs`): for streaming. Load a `.sks`, then a grid (Item Name, Game, System, Core, Note). Clicking a row runs it. "Previous" / "Next", BlastLayer toggle, and a note display.
- **Connection Status**: status text and the link to the emulator.
- **Hex Editor plugin** (`references/rtcv/Source/Plugins/HexEditor/HexEditor.cs`): domain hex view with 1/2/4-byte grouping, "Go to Address", "&Freeze"/"Un&freeze" (adds or removes an infinite STORE ONCE/IMMEDIATE self-unit, `lifetime 0`), and poking by typing.
- **Note Editor**, **Intro** (disclaimer), **Analytics tool**: minor.
- There is no "multiple blast" window. Repeated blasting is Auto-Corrupt plus Intensity/ErrorDelay.

### 3.9 Advanced tools (Tools tile, `UI/Components/Advanced Tools/`)
| Tool | Function |
|---|---|
| No Tool Selected (Shortcuts) | Quick links: My Lists / My VMDs / My Plugins, "Prepare Glitch Harvester (Start+Savestate)". |
| VMD Pool | List loaded VMDs, with summary (size, real domain), "Show VMD Contents", "Load VMD from File", "Save VMD to File", "Unload", "Rename", "Send to My VMDs". |
| VMD Generator | Domain, "VMD Name:", "Set pointer every" N addresses (PointerSpacer), "bytes of padding", word size / endian, and a "Remove/Add addresses" text of ranges (`start-end`, hex; `-` prefix removes). Then "Generate VMD". |
| Simple VMD Generator | Domain plus a "Range" or "Start Address" + "Range expression". |
| Active Table Generator (`VmdActForm`) | Collects periodic memory dumps ("Auto-add every N sec", "Add state"), computes bytes that change (activity threshold, "Exclude ever-changing %", cap size), then "Generate VMD from ACT". Save/Load/Add/Subtract ACT. |
| Limiter Profiler | Domain + limiter list, then a VMD of the matching addresses (`LongArrayFilterDomain`), optionally after loading the GH state. |
| List Generator | Type hex values, then "Generate New List" / "Save List to File". |
| Code Cave Settings | Domains to search for code caves (`UISPEC.SELECTEDDOMAINS_FORCAVESEARCH`). |
| Extra Tools and Plugins | Buttons that plugins add. |

## 4. NetCore and the Vanguard interface

### 4.1 Transport

- **Roles**: the RTC UI process is the **TCP server** (`UIConnector`, `NetworkSide.SERVER`, `Loopback = true`). The emulator is the **TCP client** (`VanguardConnector`, `NetworkSide.CLIENT`). Defaults are hard-coded in `NC/NetCoreSpec.cs`: `IP = "127.0.0.1"`, `Port = 42069`, `ClientReconnectDelay = 1500` ms, `messageReadTimerDelay = 5` ms, `DefaultBoopMonitoringCounter = 20` (1500 under a debugger). The server binds `IPAddress.Loopback` when IP is 127.0.0.1.
- **TCP framing** (`NC/TCPLink.cs`): `[int32 little-endian length][Ceras-serialized NetCoreAdvancedMessage]`. Ceras config: `PersistTypeCache = true`, so the type cache is per connection and stateful. Message = `{ string Type; Guid? requestGuid; object objectValue }`. **The payload is arbitrary .NET object graphs** (StashKey, BlastLayer, PartialSpec, `object[]` argument tuples), which is not portable. `ReadTimeout = 10000` ms. One reader/writer thread per link polls every 5 ms: it reads if `DataAvailable`, then drains the outgoing `PeerMessageQueue` (LinkedList; `priority` messages go to the front).
- **UDP** (`NC/UDPLink.cs`) carries `NetCoreSimpleMessage` (payload-less), sent as the ASCII `Type` string. Ports: server listens on 42070 (Port+1 on loopback) and sends to 42069. Client listens on 42069 and sends to 42070. It is used for `{BOOP}` keepalives while a synced call is blocking TCP, and for any non-synced, payload-less `Route` (e.g. `UI|Basic_KillswitchPulse`).
- **Handshake**: after connecting, the client sends `{HI}`. The server replies `{HI}`, and both mark `CONNECTED` (`ServerConnected` / `ClientConnected` events).
- **Keepalive**: every 500 ms each side sends `{BOOP}` (TCP with priority, or UDP if waiting on a synced return) and decrements its counter. Receiving `{BOOP}` resets it to 20. At 0 (about 10 s silent) the link is dropped as `CONNECTIONLOST`.
- **Reconnect**: `TCPLinkWatch` checks every `ClientReconnectDelay` (1.5 s). If the link is `DISCONNECTED`/`CONNECTIONLOST` it stops, sleeps 800 ms, and restarts (the server re-listens, the client re-dials with a 200 ms connect timeout). Graceful stop sends `{BYE}`.
- Internal message types use braces: `{HI}`, `{BYE}`, `{BOOP}`, `{RETURNVALUE}`, and `{EVENT_*}` (status events re-queued into the hub so handlers run on the sync thread).
- **Sync vs async**: a message is *synced* if `requestGuid != null`. `SendSyncedMessage` enqueues it and blocks in `ReturnWatch.GetValue`, polling a `ConcurrentDictionary<Guid,object>` every 5 ms (with `Application.DoEvents()` every 5th poll) **with no timeout**. It only ends when the link dies (`watch.Kill()` returns null) or a stack-depth guard trips. The receiver's `MessageHub` runs handlers on the UI/sync thread via a 5 ms timer (`SynchronizingObject = syncObject`). If the request had a GUID, it always sends back `{RETURNVALUE}` with the same GUID (payload = `e.setReturnValue(obj)`, or null).
- **Local routing** (`NC/LocalRouter.cs`): each process registers named endpoints: `"UI"`, `"VANGUARD"`, `"CORRUPTCORE"`, `"DEFAULT"`. `Route(endpoint, type, obj, synced)` calls the endpoint directly if it is local. Otherwise it goes to `DEFAULT` with the type rewritten to `"<ENDPOINT>|<Type>"` and is sent over the network. The receiving connector splits on `|` and dispatches locally. UI process: `UI` = `UIConnector`, and `VANGUARD`/`DEFAULT` = NetCore, so `CORRUPTCORE` messages also go over the wire. Emulator process: `VANGUARD` = `VanguardConnector` (which forwards un-prefixed messages to the emulator's own handler), `CORRUPTCORE` = `CorruptCoreConnector`, `DEFAULT` = NetCore.
- **Threading helpers** (`NC/SyncObjectSingleton.cs`): `FormExecute(action)` invokes on the main (WinForms) thread. `EmuThreadExecute(action, fallbackToMain)` runs on the emulator thread via the emulator-supplied `EmuInvokeDelegate`. melonDS pauses its emu thread (`EmuRunning = 2`, waits for `EmuStatus == 2`), runs the action, then restores it.

### 4.2 Command catalogue (`NC/NetcoreCommands.cs`)

Wire names are `"<Class>_<Name>"`, e.g. `Remote_LoadState`, with an optional `"ENDPOINT|"` prefix. Direction: **UI→CC** = UI to CorruptCore in the emulator process, **UI→EMU** = UI (or emu-side CC, locally) to the emulator's Vanguard handler, **EMU→UI** = emulator side to the UI. "sync" = the caller waits for a return.

**Spec sync**
| Command | Dir | Payload / return |
|---|---|---|
| `Remote.PushVanguardSpec` | EMU→UI (+local CC) | full VanguardSpec `PartialSpec`, sync. Sent on client connect. |
| `Remote.AllSpecSent` | EMU→UI then UI→EMU | Handshake complete markers. |
| `Remote.PushVanguardSpecUpdate` | EMU→UI, EMU→CC | delta `PartialSpec`. |
| `Remote.PushUISpec` / `PushUISpecUpdate` | UI→CC | full / delta UISpec. |
| `Remote.PushCorruptCoreSpec` / `PushCorruptCoreSpecUpdate` | UI→CC (full, sync); deltas both ways | CorruptCoreSpec. The emu side keeps its own `MEMORYINTERFACES`. |
| `Remote.PushPluginSpec` / `PushPluginSpecUpdate` | UI→CC; deltas both ways | PluginSpec. |
| `Remote.PushRTCSpec` / `PushRTCSpecUpdate` | UI→CC | Replace / update CorruptCoreSpec (legacy). |
| `Remote.PushVMDProtos` | UI→CC | `VmdPrototype[]` (on reconnect). |
| `Remote.EventRestrictFeatures` | →CC | CC replies to the UI with `DisableSavestateSupport`, `DisableRealtimeSupport`, `DisableKillSwitchSupport`, `DisableGameProtectionSupport` per VSPEC flags. |

**Memory / domains**
| Command | Dir | Payload / return |
|---|---|---|
| `Remote.DomainGetDomains` | UI→CC→EMU | Emulator calls `RefreshDomains()`: updates `VSPEC.MEMORYDOMAINS_INTERFACES`, then sends `EventDomainsUpdated`. |
| `Remote.EventDomainsUpdated` | EMU→CC→UI | `bool domainsChanged`. CC rebuilds `MemoryInterfaces`, and the UI refreshes the domain list and auto-selects all non-blacklisted domains. |
| `Remote.DomainRefreshDomains` | UI→EMU, sync | Sent by the Memory Domains "Refresh" button (`MemoryDomainsForm.cs`). The emulator implementation handles it (melonDS only handles `DomainGetDomains`). |
| `Remote.DomainPeekByte` | UI→CC, sync | `[string domain, long addr]` → `byte`. |
| `Remote.DomainPokeByte` | UI→CC | `[domain, addr, byte]`. |
| `Remote.DomainPeekBytes` | UI→CC, sync | `[domain, long start, long endExclusive, bool raw]` → `byte[]`. |
| `Remote.DomainPokeBytes` | UI→CC | `[domain, addr, byte[], bool raw]`. |
| `Remote.DomainVMDAdd` / `DomainVMDRemove` | both | `VmdPrototype` / VMD name. Remove also clears step units. |
| `Remote.DomainActiveTableMakeDump` | UI→CC | `[domain, key]`. Writes a full dump to `WORKING/MEMORYDUMPS/<key>.dmp` (Active Table tool). |
| `Remote.LongArrayFilterDomain` | UI→CC, sync | `[domain, limiterHash, StashKey?]` → `long[]` of matching addresses (VMD limiter profiler). |
| `Remote.GenerateVMDText` | →UI | `[domain, text]`. Creates a VMD from address-range text (sent by the HexEditor plugin). |

**Savestates / game / emulation control**
| Command | Dir | Payload / return |
|---|---|---|
| `Basic.SaveSavestate` | CC→EMU, sync | `string key` → full path of the written `.State` (in `WORKING/SESSION`). |
| `Basic.LoadSavestate` | CC→EMU, sync | `[string path, StashKeySavestateLocation]` → bool. The emulator applies `SYNCSETTINGS` and calls `StepActions.ClearStepBlastUnits()` + `RtcClock.ResetCount()`. |
| `Remote.SaveState` / `SaveStateless` | UI→CC, sync | `StashKey?` → `StashKey` with state path. |
| `Remote.LoadState` | UI→CC, sync | `[StashKey, bool reloadRom, bool applyBlastLayer]` → bool. |
| `Remote.BackupKeyRequest` → `Remote.BackupKeyStash` | UI→CC, then CC→UI async | Game-protection backup `StashKey`. |
| `Remote.LoadROM` | CC→EMU, sync | `string path`. Must block until the load is done. |
| `Remote.CloseGame` | →EMU | Stop the game. |
| `Remote.KeySetSystemCore` | CC→EMU, sync | `[systemName, systemCore]`. Switch core (multi-system emulators). |
| `Remote.KeySetSyncSettings` | CC→EMU, sync | `string syncSettings` (emulator-defined, JSON in melonDS). |
| `Remote.ResumeEmulation` | CC→EMU | Unpause after main-thread load/generation (`LOADSTATE_USES_CALLBACKS`). |
| `Remote.PreCorruptAction` / `PostCorruptAction` | UI→EMU | Hooks around every GH operation (melonDS re-inits its renderer on Post). |
| `Remote.IsNormalAdvance` | →EMU, sync | → bool. Meant to skip game-protection backups during abnormal advance (usage is commented out in `CorruptCoreConnector`; melonDS returns true). |
| `Emulator.ResetGame`, `Emulator.GetScreenshot`, `Emulator.GetRealtimeAPI` | →EMU | Defined for implementations; not used by RTCV core. `Emulator.InFocus` is used as a VanguardSpec key: UI hotkeys fire when `UISpec[RTCInFocus] || VanguardSpec[Emulator_InFocus]` (`UI/Input/Input.cs`). |
| `Remote.EventEmuStarted`, `EventLoadGameDoneNewGame`, `EventLoadgameDoneSameGame`, `EventEmuMainFormClose`, `EventCloseEmulator`, `EventShutdown` | lifecycle | Emulator close/quit, CC shutdown. |
| `Remote.RenderStart` / `RenderStop` / `RenderDisplay` | UI↔EMU | AV rendering of corrupted output (`Render.RENDERTYPE { NONE, WAV, AVI, MPEG, LAST }`), if `SUPPORTS_RENDERING`. |

**Blast / corruption**
| Command | Dir | Payload / return |
|---|---|---|
| `Basic.ManualBlast` | UI→CC | Generate with current settings and apply now (`GenerateAndBlast`). |
| `Basic.GenerateBlastLayer` | UI→CC, sync | `[StashKey, bool loadBeforeCorrupt, bool apply, bool backup]` → `BlastLayer`. |
| `Basic.ApplyBlastLayer` | UI→CC | `[BlastLayer, bool storeUncorruptBackup, bool merge?]`. Applied on the emu thread with `followMaximums=true`. |
| `Basic.BlastGeneratorBlast` | UI→CC, sync | `[StashKey, List<BlastGeneratorProto>, loadBefore, applyAfter, resumeAfter]` → protos with generated `bl`. |
| `Remote.GetBlastGeneratorLayer` | CC→UI, sync | → `BlastLayer` (Blast Generator engine). |
| `Remote.RerollBlastLayer` | UI→CC, sync | `BlastLayer` → rerolled `BlastLayer`. |
| `Remote.ClearStepBlastUnits` | UI→CC | Clear StepActions. |
| `Remote.RemoveExcessInfiniteStepUnits` | UI→CC | Enforce the max-infinite cap. |
| `Remote.SetApplyUncorruptBL` / `SetApplyCorruptBL` / `ClearBlastlayerCache` | UI→CC | BlastLayer toggle support. |
| `Remote.BlastToolsGetAppliedBackupLayer` | UI→CC, sync | `[BlastLayer, StashKey]` → backup layer after a one-frame apply (Blast Editor "bake"). |
| `Remote.KeyGetRawBlastLayer` | UI→CC, sync | → `StashKey` with the raw layer (active units + ROM diff). |
| `Remote.BLGetDiffBlastLayer` | UI→CC, sync | `filename` → layer from diffing a file against memory (`CC/BlastDiff.cs`). |
| `Remote.UpdatedSelectedPluginEngine` | UI→CC | `ICorruptionEngine`. |
| `Basic.ErrorDisableAutoCorrupt` | CC→UI | Generation failed; untick auto-corrupt. |
| `Basic.ApplyCachedBlastLayer`, `Basic.Blast`, `Basic.StashKey` | (defined; no handler found) | |

**UI events / misc**
| Command | Dir | Purpose |
|---|---|---|
| `Basic.KillswitchPulse` | EMU→UI (UDP) | Heartbeat for the auto kill switch. |
| `Basic.ResetGameProtectionIfRunning` | EMU→UI | Game changed. |
| `Basic.RTCInFocus` | UISpec key | Focus state for hotkeys. |
| `Remote.TriggerHotkey` | EMU→UI | Emulator-captured hotkey forwarded to the RTC. `Remote.Hotkey*` constants (ManualBlast, AutoCorruptToggle, ErrorDelay±, Intensity±, GHLoadCorrupt, GHCorrupt, GHReroll, GHLoad, GHSave, GHStashToStockpile, BlastRawStash, SendRawStash, BlastLayerToggle, BlastLayerReBlast, GameProtectionBack, GameProtectionNow, Be* blast editor ops) name the actions. |
| `Remote.BlastEditor*`, `Remote.SanitizeTool*` | →UI | Drive the Blast Editor / Sanitize tool remotely (plugins, hotkeys). |
| `Remote.OpenHexEditor`, `Emulator.OpenHexEditorAddress` | UI→CC→EMU or HEXEDITOR plugin | Open a hex editor (at `[domain, address]`). |
| `Remote.LoadPlugins` | UI→CC | Load plugins emu-side. |
| `Remote.EditController` | (defined; no handler found in this repo) | |
| `GETSPECDUMPS` | →CC, sync | Debug text dump of all specs. |

### 4.3 Connection sequence

1. The UI starts, creates `UISpec`, registers `CorruptCoreSpec` (defaults + `Params` flag files), and starts the TCP server.
2. The emulator starts `VanguardConnector` (TCP client), builds `VanguardSpec` from its defaults, calls `RtcCore.StartEmuSide()` (starts the kill-switch pulse timer), and connects.
3. On `ClientConnected` the emulator sends `PushVanguardSpec` (sync) and then `AllSpecSent`.
4. The UI stores VanguardSpec and replies with `PushUISpec`, `PushCorruptCoreSpec`, `PushPluginSpec` (all sync), then `AllSpecSent`.
5. First connect only: the UI loads plugins on both sides (`LoadPlugins`), configures itself from VSPEC flags, and loads lists from `<emuDir>/LISTS` and `RTC/LISTS`. On reconnect it also pushes VMD prototypes and refuses a different emulator (`VSPEC.NAME` must match).
6. On game load the emulator: `LOAD_GAME_START` clears step units, resets `RtcClock` and sets `OPENROMFILENAME`. `LOAD_GAME_DONE` sets `SYSTEM`, `SYSTEMPREFIX`, `SYSTEMCORE`, `GAMENAME`, `CORE_DISKBASED`, then `RefreshDomains()` (spec update + `EventDomainsUpdated`), plus `ResetGameProtectionIfRunning` if the game changed. `GAME_CLOSED` clears `OPENROMFILENAME` and the domains.

### 4.4 Responsibility split

| Concern | Where it runs |
|---|---|
| Memory domain implementation (peek/poke) | Emulator (`IMemoryDomain` classes; melonDS maps `MainRAM`, `VRAM`, `CartROM`, `SharedWRAM`, `ARM7WRAM` onto `NDS::ARM9Read8/Write8` and raw arrays). |
| Blast layer **generation** (engines, radius, limiter checks) | Emulator process (CorruptCore via `Basic.GenerateBlastLayer` / `ManualBlast` / auto-corrupt), because it peeks memory constantly. Exception: the Blast Generator engine's layer comes from the UI; the BG protos themselves are generated emu-side (`BlastGeneratorBlast`). |
| **StepActions** (freeze, pipe, delayed, looping units) | Emulator process, called synchronously from the frame hook on the emulation thread. |
| Auto-corrupt timer (ErrorDelay counter) | Emulator process (`RtcClock.StepCorrupt`, per frame). |
| Frame hook | Emulator calls `RtcClock.StepCorrupt(true, true)` once per frame. melonDS does this at the top of `NDS::RunFrame()` (`VanguardClientUnmanaged::CORE_STEP`), before the frame is emulated. There is only one hook point (no separate pre/post hook); the `StepStart`/`StepPreCorrupt`/`StepPostCorrupt`/`StepEnd` events fire inside it for plugins. |
| Savestate files | Written/read by the emulator at paths it chooses (`RTC/WORKING/SESSION/<game>.<key>.timejump.State`). The path string is returned to the RTC. RTC copies/moves them into `SKS`/`SSK`/`TEMP` when saving stockpiles. **Assumes a shared filesystem.** |
| ROM loading, core switch, sync settings, pause/resume | Emulator (`LoadROM`, `KeySetSystemCore`, `KeySetSyncSettings`, `ResumeEmulation`). |
| Glitch Harvester state (stash history, stockpile, savestate boxes, current key) | UI process. |
| Settings master copy (CorruptCoreSpec, UISpec) | UI process, mirrored to the emulator. |
| Lists (limiter/value) | Loaded on the UI side from files, shipped to the emulator as objects inside CorruptCoreSpec. |
| Hotkeys | UI (and emulator forwarding via `TriggerHotkey`). Emulator-side GUI: only the emulator's own window, plus the optional integrated hex editor. RTCV has no Vanguard settings window of its own. |

## 5. Implications for rtcv-ish

**Minimal emulator-side API** (what a Vanguard implementation actually provides, reduced to primitives):
- `get_info()` returns name, system, core, game name, ROM path, capability flags (savestates, realtime, multithread, disk-based), blacklisted domains, and sync settings (the VanguardSpec subset that matters).
- `list_domains()` returns `[{name, size, word_size, big_endian}]`. The emulator also pushes a "domains changed" event on game load and close.
- `peek(domain, addr, len)` / `poke(domain, addr, bytes)`, and **batched** versions: `peek_many([(domain, addr, len)])`, `poke_many([(domain, addr, bytes)])`, `dump(domain)`. RTCV pokes byte by byte in-process. Over IP, per-byte RPC is far too slow, so batching (or bulk dump + local cache, as the `IRPCMemoryDomain` DumpMemory/UpdateMemory pattern does) is mandatory.
- `save_state()` returns an opaque state blob or ID, and `load_state(blob)`. **Transfer bytes, not paths.** RTCV's path exchange assumes a shared disk.
- `load_rom(path_or_blob)`, `close_game()`, `pause()`, `resume()`, `step_frame(n)`, `reset()`, and optionally `screenshot()`. Loads must be acknowledged when *complete* (RTCV relies on synced LoadROM).
- **Frame event**: a per-frame callback at a fixed point (RTCV: frame start, before emulation) with a frame counter. Everything time-based (lifetime, ExecuteFrame, ErrorDelay, loops, pipes, freezes) is counted in these ticks.
- **Per-frame active-unit execution must run in or next to the emulator.** A network round-trip per frame is not viable at 60 fps with many units. Two options:
  1. Port `StepActions` + `BlastUnit.Execute` into the emulator adapter (C/C++), receiving the rasterized unit list from the Go core (`apply_layer(units)`, `clear_units()`, `list_active_units()`).
  2. A minimal "freeze/pipe list" primitive: `set_freezes([(domain, addr, bytes)])`, `set_pipes([(src_dom, src_addr, dst_dom, dst_addr, len, tilt)])`, and `schedule(unit, at_frame, lifetime, loop)`.
  
  Option 1 keeps full semantics (STORE ONCE/CONTINUOUS, IMMEDIATE/PREEXECUTE, limiter EXECUTE/PREEXECUTE checks, tilt, loop/LoopTiming, MaxInfiniteBlastUnits).
- Generation needs memory reads (Nightmare tilt, Distortion, Vector/Cluster limiter checks). Options: have the Go core generate using batched peeks or a cached domain dump (after pausing), or implement generation in the adapter. Generation should happen while emulation is paused at a frame boundary (RTCV runs it on the emu thread or with the emu thread paused).
- Heartbeat/keepalive and a watchdog (RTCV: `{BOOP}` every 500 ms, a 20-miss limit, and a 250 ms kill-switch pulse with 25 misses, followed by an emulator restart).
- Optional: hotkey forwarding, rendering (AV capture), config-file handoff for stockpiles (`CONFIG_PATHS`), and multi-core switching (`KeySetSystemCore`).

**Windows/.NET-specific pieces that must be redesigned**
- Ceras binary serialization of arbitrary .NET object graphs (`object[]` tuples, `PartialSpec`, `BlastLayer`, `StashKey`) with a per-connection type cache. Replace with a language-neutral schema (JSON or protobuf/msgpack) and explicit request/response types.
- The endpoint-prefix routing (`"CORRUPTCORE|Remote_LoadState"`) and the "synced message blocks the UI thread with `Application.DoEvents()` and no timeout" pattern. Replace with request IDs, timeouts, cancellation, and async events.
- Hard-coded `127.0.0.1:42069` (TCP) plus UDP on 42069/42070, with the RTC as server and the emulator as client. For IP-based control across machines, make addresses configurable, add authentication, and probably invert roles: Go core as the server, emulators connect or are dialed.
- The shared-filesystem assumptions: savestate paths (`RTC/WORKING/SESSION/...timejump.State`), ROM paths in StashKeys, `RESTARTDETACHEDRTC.bat` for the kill switch, `Params` flag files, `EmuDir`/`RtcDir` path juggling, and `AssemblyResolve` DLL loading.
- WinForms threading (`SyncObjectSingleton.FormExecute`, `ISynchronizeInvoke` timers), `MessageBox` calls from inside core logic (CorruptCore shows dialogs on errors), `OpenFileDialog` in core code (`BlastTools`, `CustomEngine`), and `System.Windows.Forms.Timer` for the kill switch.
- C++/CLI mixed-mode glue in emulators (melonDS `VanguardClient.cpp` uses `gcnew`, `msclr`). This needs a plain C/C++ RPC client library.
- The MEF plugin host and plugins that execute on both sides. Plugins returning `System.Windows.Forms.Form` controls (`ICorruptionEngine.Control`).
- BinaryFormatter `.vmd` files (insecure and .NET-only). The `.sks`/`.ssk` zip + JSON formats are portable, but they embed .NET enum names, `BigInteger` values, and `$type`-free JSON produced by Newtonsoft. A compatibility importer is possible if desired.
- The thread-local `System.Random` seeded from `DateTime.Now.Ticks`. rtcv-ish could add explicit seeds for reproducibility (RTCV has none except in the Blast Generator).
- Spec replication (four mutable dictionaries mirrored with delta pushes and an echo-suppression flag). In rtcv-ish, the Go core should be the single source of truth for settings, with emulators receiving only what they need (capabilities in, units/commands out).
