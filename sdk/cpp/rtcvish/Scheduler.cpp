#include "Scheduler.h"

#include <unordered_set>

namespace rtcvish {

const Domain* findDomain(const std::vector<Domain>& domains, const std::string& name) {
    for (const auto& d : domains) {
        if (d.name == name) {
            return &d;
        }
    }
    return nullptr;
}

bool inRange(const Domain& domain, uint64_t address, uint64_t size) {
    return address <= domain.size && size <= domain.size - address;
}

namespace {

bool validate(const Unit& u, const std::vector<Domain>& domains, Error& err) {
    std::string prefix = "unit " + std::to_string(u.id) + ": ";
    const Domain* d = findDomain(domains, u.domain);
    if (!d) {
        return err.set(ErrorCode::NotFound, prefix + "unknown domain " + u.domain);
    }
    if (!d->writable) {
        return err.set(ErrorCode::InvalidArgument, prefix + "domain " + u.domain + " is read-only");
    }
    if (u.size == 0) {
        return err.set(ErrorCode::InvalidArgument, prefix + "size is 0");
    }
    if (!inRange(*d, u.address, u.size)) {
        return err.set(ErrorCode::OutOfRange, prefix + "range outside " + u.domain);
    }
    if (u.isStore) {
        const Domain* s = findDomain(domains, u.storeDomain);
        if (!s) {
            return err.set(ErrorCode::NotFound, prefix + "unknown store domain " + u.storeDomain);
        }
        if (!inRange(*s, u.storeAddress, u.size)) {
            return err.set(ErrorCode::OutOfRange, prefix + "store range outside " + u.storeDomain);
        }
        if (u.tilt != 0 && u.size != 1 && u.size != 2 && u.size != 4 && u.size != 8) {
            return err.set(ErrorCode::InvalidArgument, prefix + "tilt needs size 1, 2, 4 or 8");
        }
    } else if (u.value.size() != u.size) {
        return err.set(ErrorCode::InvalidArgument, prefix + "value length does not match size");
    }
    return true;
}

void applyTilt(std::vector<uint8_t>& buf, int64_t tilt, bool bigEndian) {
    size_t n = buf.size();
    uint64_t v = 0;
    for (size_t i = 0; i < n; i++) {
        size_t shift = 8 * (bigEndian ? n - 1 - i : i);
        v |= uint64_t(buf[i]) << shift;
    }
    v += uint64_t(tilt);
    for (size_t i = 0; i < n; i++) {
        size_t shift = 8 * (bigEndian ? n - 1 - i : i);
        buf[i] = uint8_t(v >> shift);
    }
}

} // namespace

bool Scheduler::apply(const std::vector<Unit>& units, const std::vector<Domain>& domains,
                      Error& err) {
    std::unordered_set<uint64_t> ids;
    for (const auto& e : entries_) {
        ids.insert(e.unit.id);
    }
    for (const auto& u : units) {
        if (!ids.insert(u.id).second) {
            return err.set(ErrorCode::InvalidArgument,
                           "unit id " + std::to_string(u.id) + " is already in use");
        }
        if (!validate(u, domains, err)) {
            return false;
        }
    }
    for (const auto& u : units) {
        Entry e;
        e.unit = u;
        e.wait = u.delay;
        e.bigEndian = findDomain(domains, u.domain)->bigEndian;
        entries_.push_back(std::move(e));
    }
    return true;
}

void Scheduler::remove(const std::vector<uint64_t>& ids) {
    std::unordered_set<uint64_t> set(ids.begin(), ids.end());
    entries_.remove_if([&](const Entry& e) { return set.count(e.unit.id) != 0; });
}

void Scheduler::clear() {
    entries_.clear();
}

std::vector<Unit> Scheduler::list() const {
    std::vector<Unit> out;
    out.reserve(entries_.size());
    for (const auto& e : entries_) {
        out.push_back(e.unit);
    }
    return out;
}

bool Scheduler::sample(Backend& backend, Entry& e) {
    const Unit& u = e.unit;
    e.sample.resize(u.size);
    Error err;
    if (!backend.read(u.storeDomain, u.storeAddress, u.size, e.sample.data(), err)) {
        e.sample.clear();
        return false;
    }
    if (u.tilt != 0) {
        applyTilt(e.sample, u.tilt, e.bigEndian);
    }
    return true;
}

void Scheduler::runFrame(Backend& backend) {
    for (auto& e : entries_) {
        if (!e.executing) {
            if (e.wait > 0) {
                e.wait--;
                continue;
            }
            e.executing = true;
            e.executed = 0;
            e.sample.clear();
        }
        const Unit& u = e.unit;
        Error err;
        if (!u.isStore) {
            backend.write(u.domain, u.address, u.value.data(), u.size, err);
        } else {
            // Sampled right before writing, so that writes of earlier units
            // in this frame are visible. Once-units keep their first sample.
            if (u.continuous || e.sample.empty()) {
                sample(backend, e);
            }
            if (e.sample.size() == u.size) {
                backend.write(u.domain, u.address, e.sample.data(), u.size, err);
            }
        }
        e.executed++;
    }

    for (auto it = entries_.begin(); it != entries_.end();) {
        const Unit& u = it->unit;
        if (!it->executing || u.lifetime == 0 || it->executed < u.lifetime) {
            ++it;
            continue;
        }
        if (!u.loop) {
            it = entries_.erase(it);
            continue;
        }
        it->executing = false;
        it->wait = u.loopDelay != 0 ? u.loopDelay : u.delay;
        it->sample.clear();
        ++it;
    }
}

} // namespace rtcvish
