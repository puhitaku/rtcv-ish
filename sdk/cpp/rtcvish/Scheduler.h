// Per-frame unit scheduler (design/emulator-api.md, "Unit scheduler").
// Not thread-safe: use it from the emulation thread only.
#pragma once

#include "Backend.h"
#include "Types.h"

#include <cstdint>
#include <list>
#include <vector>

namespace rtcvish {

class Scheduler {
public:
    // Validate and queue units, all or nothing. `domains` is the current
    // domain list of the backend.
    bool apply(const std::vector<Unit>& units, const std::vector<Domain>& domains, Error& err);

    // Remove units by id. Unknown ids are ignored.
    void remove(const std::vector<uint64_t>& ids);

    void clear();

    // Units that are queued or executing, in apply order.
    std::vector<Unit> list() const;

    size_t size() const { return entries_.size(); }

    // Execute one frame worth of units in apply order. Call right before
    // the emulator emulates a frame. A unit applied with delay=N is skipped
    // for N calls and writes on call N+1. Store units sample their source
    // right before writing (continuous) or on their first write (once).
    void runFrame(Backend& backend);

private:
    struct Entry {
        Unit unit;
        bool bigEndian = false;
        bool executing = false;
        uint32_t wait = 0;
        uint32_t executed = 0;
        std::vector<uint8_t> sample;
    };

    bool sample(Backend& backend, Entry& e);

    std::list<Entry> entries_;
};

} // namespace rtcvish
