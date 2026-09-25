// Wire format helpers: framing, nanopb encode/decode and conversions
// between nanopb structs and the SDK types. Internal to the SDK; emulators
// only need Server.h, Backend.h and Scheduler.h.
//
// Every pointer field inside a nanopb struct handled here is allocated with
// malloc, so pb_release() (called by OwnedMessage) frees it.
#pragma once

#include "Types.h"
#include "emulator.pb.h"

#include <cstddef>
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace rtcvish {
namespace wire {

using Message = rtcvish_emulator_v1_Message;
using Request = rtcvish_emulator_v1_Request;
using Response = rtcvish_emulator_v1_Response;
using Event = rtcvish_emulator_v1_Event;

constexpr size_t kHeaderSize = 4;
constexpr uint32_t kMaxMessageSize = 64u << 20;
// Default Capabilities.max_payload. A payload this large does not fit a
// frame together with its envelope; such responses fail with FAILED.
constexpr uint32_t kMaxPayload = kMaxMessageSize;

// A zero-initialized message whose heap fields are released on destruction.
class OwnedMessage {
public:
    OwnedMessage();
    ~OwnedMessage();
    OwnedMessage(const OwnedMessage&) = delete;
    OwnedMessage& operator=(const OwnedMessage&) = delete;

    Message& get() { return msg_; }
    const Message& get() const { return msg_; }

    Request& request();
    Response& response(uint32_t id);
    Event& event();

private:
    Message msg_;
};

uint32_t readLength(const uint8_t* header);

// Decode one message body (without the length prefix). Returns nullptr when
// the bytes are not a valid Message.
std::shared_ptr<OwnedMessage> decode(const uint8_t* data, size_t size);

// Encode a message with its length prefix. False if encoding fails or the
// result exceeds kMaxMessageSize.
bool encodeFrame(const Message& msg, std::vector<uint8_t>& out);

char* copyString(const std::string& s);
std::string toString(const char* s);
pb_bytes_array_t* allocBytes(size_t size);
pb_bytes_array_t* copyBytes(const uint8_t* data, size_t size);

// Allocate a zeroed array of `count` elements for a repeated field.
template <typename T> T* allocArray(size_t count) {
    return static_cast<T*>(calloc(count == 0 ? 1 : count, sizeof(T)));
}

void setError(Response& resp, const Error& err);
void setError(Response& resp, ErrorCode code, const std::string& message);

void toPb(const Status& in, rtcvish_emulator_v1_Status& out);
void toPb(const Domain& in, rtcvish_emulator_v1_Domain& out);
void toPb(const Capabilities& in, rtcvish_emulator_v1_Capabilities& out);
// With includeValue false, a value unit is encoded with an empty value.
void toPb(const Unit& in, rtcvish_emulator_v1_Unit& out, bool includeValue = true);
Unit fromPb(const rtcvish_emulator_v1_Unit& in);

} // namespace wire
} // namespace rtcvish
