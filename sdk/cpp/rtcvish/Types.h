// Plain C++ types shared by the SDK's public interfaces. They mirror the
// messages in api/emulator/v1/emulator.proto without exposing nanopb.
#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace rtcvish {

enum class ErrorCode {
    Unknown = 0,
    InvalidArgument = 1,
    NotFound = 2,
    OutOfRange = 3,
    NoRom = 4,
    Unsupported = 5,
    Failed = 6,
    Busy = 7,
};

struct Error {
    ErrorCode code = ErrorCode::Unknown;
    std::string message;

    // Sets the error and returns false, so that `return err.set(...)` works
    // in functions that report failure with a bool.
    bool set(ErrorCode c, std::string msg) {
        code = c;
        message = std::move(msg);
        return false;
    }
};

enum class State {
    NoRom = 0,
    Running = 1,
    Paused = 2,
};

struct Status {
    State state = State::NoRom;
    uint64_t frame = 0;
    std::string romPath;
    std::string gameTitle;
    std::string gameCode;
    std::string console;
};

struct Capabilities {
    bool savestates = false;
    bool screenshot = false;
    bool input = false;
    bool loadRom = false;
    bool reset = false;
    // 0 lets the server fill in its own limit.
    uint32_t maxPayload = 0;
};

struct Info {
    std::string emulator;
    std::string version;
    std::string system;
    Capabilities capabilities;
};

struct Domain {
    std::string name;
    uint64_t size = 0;
    uint32_t wordSize = 1;
    bool bigEndian = false;
    bool writable = true;
    bool hidden = false;
};

struct Image {
    uint32_t width = 0;
    uint32_t height = 0;
    std::vector<uint8_t> rgba;
};

struct Input {
    uint32_t buttons = 0;
    bool touch = false;
    uint32_t touchX = 0;
    uint32_t touchY = 0;
    // True removes the override.
    bool clear = false;
};

// A memory write executed by the Scheduler at frame boundaries. See
// design/emulator-api.md, "Unit scheduler".
struct Unit {
    uint64_t id = 0;
    std::string domain;
    uint64_t address = 0;
    uint32_t size = 0;

    bool isStore = false;
    std::vector<uint8_t> value; // !isStore
    std::string storeDomain;    // isStore
    uint64_t storeAddress = 0;  // isStore
    bool continuous = false;    // isStore

    int64_t tilt = 0;
    uint32_t delay = 0;
    uint32_t lifetime = 0;
    bool loop = false;
    uint32_t loopDelay = 0;
};

const Domain* findDomain(const std::vector<Domain>& domains, const std::string& name);

// True when [address, address+size) lies inside the domain.
bool inRange(const Domain& domain, uint64_t address, uint64_t size);

} // namespace rtcvish
