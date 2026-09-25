// Per-frame unit scheduler (design/emulator-api.md, "Unit scheduler").
// Not thread-safe: use it from the emulation thread only.
#pragma once

#include "Backend.h"
#include "Types.h"

#include <cstdint>
#include <functional>
#include <list>
#include <vector>

namespace rtcvish {

class Scheduler {
public:
    using FrozenListener = std::function<void(const std::vector<FrozenRange>&)>;

    // Modes the emulator implements. A unit asking for a missing mode runs
    // with the next weaker one (HARD -> SCANLINE -> FRAME). Default: FRAME
    // only.
    void setModes(bool scanline, bool hard);

    // Called with the ranges of the executing HARD units whenever they
    // change (see Backend::setFrozen).
    void setFrozenListener(FrozenListener listener) { listener_ = std::move(listener); }

    // Validate and queue units, all or nothing. `domains` is the current
    // domain list of the backend.
    bool apply(const std::vector<Unit>& units, const std::vector<Domain>& domains, Error& err);

    // Remove units by id. Unknown ids are ignored.
    void remove(const std::vector<uint64_t>& ids);

    void clear();

    // Call f(const Unit&) for every queued or executing unit, in apply
    // order, without copying.
    template <typename F> void forEach(F&& f) const {
        for (const auto& e : entries_) {
            f(e.unit);
        }
    }

    size_t size() const { return entries_.size(); }

    // Execute one frame worth of units in apply order. Call right before
    // the emulator emulates a frame. A unit applied with delay=N is skipped
    // for N calls and writes on call N+1. Store units sample their source
    // right before writing (continuous) or on their first write (once).
    void runFrame(Backend& backend);

    // Rewrite the SCANLINE and HARD units executing in the current frame.
    // Call at every scanline between runFrame() and endFrame(); returns
    // immediately when there are none.
    void runScanline(Backend& backend) {
        if (scanlineUnits_ != 0) {
            rewrite(backend);
        }
    }

    // True when runScanline() has something to do in the current frame.
    bool scanlineActive() const { return scanlineUnits_ != 0; }

    // Retire units whose lifetime ended in the frame just emulated. Call
    // after every frame that followed runFrame().
    void endFrame();

    // Write the executing SCANLINE and HARD units again, e.g. after a
    // savestate replaced memory.
    void rewrite(Backend& backend);

    // Replace the bytes of a pending API write that HARD units freeze with
    // their frozen values, so that the write cannot change them either.
    void maskWrite(const std::string& domain, uint64_t address, uint8_t* data, size_t size) const;

    bool hasFrozen() const { return !frozen_.empty(); }

private:
    struct Entry {
        Unit unit;
        UnitMode mode = UnitMode::Frame; // effective mode
        bool bigEndian = false;
        bool executing = false;
        uint32_t wait = 0;
        uint32_t executed = 0;
        std::vector<uint8_t> sample;
    };

    bool sample(Backend& backend, Entry& e);
    void write(Backend& backend, Entry& e);
    // Recount scanline units and notify the listener of frozen changes.
    void refresh();

    std::list<Entry> entries_;
    bool scanlineSupported_ = false;
    bool hardSupported_ = false;
    size_t scanlineUnits_ = 0;
    std::vector<FrozenRange> frozen_;
    FrozenListener listener_;
};

} // namespace rtcvish
