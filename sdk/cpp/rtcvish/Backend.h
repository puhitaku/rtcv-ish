// The interface an emulator implements to be driven by rtcvish::Server.
//
// Every method is called on the thread that drains Server::pollJobs(),
// normally the emulation thread at a frame boundary, so implementations may
// touch emulator state freely. The server validates requests before calling
// in: domain names and ranges passed to read/write exist and are in bounds,
// and methods that need a loaded game are only called when status() does
// not report State::NoRom.
#pragma once

#include "Types.h"

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace rtcvish {

class Backend {
public:
    virtual ~Backend() = default;

    // Static emulator description. Called once from Server::start().
    virtual Info hello() = 0;

    // Current state. `frame` is the emulator's monotonic frame counter: it
    // counts frames emulated since the last loadRom/reset (from any source)
    // and is not rewound by loadState.
    virtual Status status() = 0;

    // Memory domains of the running game; empty when no game is loaded.
    virtual std::vector<Domain> domains() = 0;

    // Copy `size` bytes at `address` of `domain` into `out`.
    virtual bool read(const std::string& domain, uint64_t address, uint32_t size, uint8_t* out,
                      Error& err) = 0;

    // Write `size` bytes to `address` of `domain`, keeping caches (JIT,
    // renderer) coherent. Only called for writable domains.
    virtual bool write(const std::string& domain, uint64_t address, const uint8_t* data,
                       uint32_t size, Error& err) = 0;

    // Serialize the console into `out`.
    virtual bool saveState(std::vector<uint8_t>& out, Error& err) = 0;

    // Restore the console from a blob produced by saveState. Reject bad blobs
    // with ErrorCode::InvalidArgument.
    virtual bool loadState(const uint8_t* data, size_t size, Error& err) = 0;

    // Load the ROM at `path` (on the emulator host) and start running. Return
    // only once the game runs. Missing files: ErrorCode::NotFound; anything
    // else: ErrorCode::Failed. Must not block on user interaction. Resets the
    // frame counter.
    virtual bool loadRom(const std::string& path, Error& err) = 0;

    // Stop emulation and unload the game.
    virtual bool closeRom(Error& err) = 0;

    // Hard reset the console, keeping the running/paused state. Resets the
    // frame counter.
    virtual bool reset(Error& err) = 0;

    // Pause / resume emulation. Both are idempotent. resume() also discards
    // frames requested with runFrames().
    virtual bool pause(Error& err) = 0;
    virtual bool resume(Error& err) = 0;

    // While paused, emulate `frames` more frames and stay paused. Every
    // emulated frame must be reported with Server::frameCompleted().
    virtual bool runFrames(uint32_t frames, Error& err) = 0;

    // Discard frames requested with runFrames() that have not run yet.
    virtual void cancelFrames() = 0;

    // Set or clear (input.clear) the input override, OR-ed with user input.
    virtual bool setInput(const Input& input, Error& err) = 0;

    // RGBA8 images of every screen.
    virtual bool screenshot(std::vector<Image>& screens, Error& err) = 0;

    // Exit the emulator process. Called after the QuitResponse was sent.
    virtual void quit() = 0;

    // HARD units: `ranges` replaces the set of bytes whose guest writes (CPU,
    // DMA, ...) must be dropped, so that they keep their current contents.
    // Ranges may overlap. Called whenever the set changes, with an empty
    // list when no HARD unit executes, and before the emulator runs again.
    // write() itself must not be intercepted: the scheduler uses it to
    // establish the frozen values. Backends without Capabilities::hardUnits
    // never receive ranges.
    virtual void setFrozen(const std::vector<FrozenRange>& ranges) { (void)ranges; }
};

} // namespace rtcvish
