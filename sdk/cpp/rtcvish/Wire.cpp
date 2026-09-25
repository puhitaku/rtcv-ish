#include "Wire.h"

#include <pb_decode.h>
#include <pb_encode.h>

#include <cstdlib>
#include <cstring>

namespace rtcvish {
namespace wire {

OwnedMessage::OwnedMessage() {
    std::memset(&msg_, 0, sizeof(msg_));
}

OwnedMessage::~OwnedMessage() {
    pb_release(rtcvish_emulator_v1_Message_fields, &msg_);
}

Request& OwnedMessage::request() {
    msg_.which_body = rtcvish_emulator_v1_Message_request_tag;
    return msg_.body.request;
}

Response& OwnedMessage::response(uint32_t id) {
    msg_.which_body = rtcvish_emulator_v1_Message_response_tag;
    msg_.body.response.id = id;
    return msg_.body.response;
}

Event& OwnedMessage::event() {
    msg_.which_body = rtcvish_emulator_v1_Message_event_tag;
    return msg_.body.event;
}

uint32_t readLength(const uint8_t* h) {
    return uint32_t(h[0]) | uint32_t(h[1]) << 8 | uint32_t(h[2]) << 16 | uint32_t(h[3]) << 24;
}

std::shared_ptr<OwnedMessage> decode(const uint8_t* data, size_t size) {
    auto msg = std::make_shared<OwnedMessage>();
    pb_istream_t stream = pb_istream_from_buffer(data, size);
    if (!pb_decode(&stream, rtcvish_emulator_v1_Message_fields, &msg->get())) {
        // pb_decode releases partially decoded fields on failure.
        std::memset(&msg->get(), 0, sizeof(Message));
        return nullptr;
    }
    return msg;
}

bool encodeFrame(const Message& msg, std::vector<uint8_t>& out) {
    size_t size = 0;
    if (!pb_get_encoded_size(&size, rtcvish_emulator_v1_Message_fields, &msg) ||
        size > kMaxMessageSize) {
        return false;
    }
    out.resize(kHeaderSize + size);
    out[0] = uint8_t(size);
    out[1] = uint8_t(size >> 8);
    out[2] = uint8_t(size >> 16);
    out[3] = uint8_t(size >> 24);
    pb_ostream_t stream = pb_ostream_from_buffer(out.data() + kHeaderSize, size);
    return pb_encode(&stream, rtcvish_emulator_v1_Message_fields, &msg);
}

char* copyString(const std::string& s) {
    char* p = static_cast<char*>(malloc(s.size() + 1));
    std::memcpy(p, s.c_str(), s.size() + 1);
    return p;
}

std::string toString(const char* s) {
    return s ? std::string(s) : std::string();
}

pb_bytes_array_t* allocBytes(size_t size) {
    auto* p = static_cast<pb_bytes_array_t*>(malloc(PB_BYTES_ARRAY_T_ALLOCSIZE(size)));
    p->size = pb_size_t(size);
    return p;
}

pb_bytes_array_t* copyBytes(const uint8_t* data, size_t size) {
    pb_bytes_array_t* p = allocBytes(size);
    if (size > 0) {
        std::memcpy(p->bytes, data, size);
    }
    return p;
}

void setError(Response& resp, const Error& err) {
    setError(resp, err.code, err.message);
}

void setError(Response& resp, ErrorCode code, const std::string& message) {
    // The body may already hold heap fields of another member of the oneof.
    Message tmp;
    std::memset(&tmp, 0, sizeof(tmp));
    tmp.which_body = rtcvish_emulator_v1_Message_response_tag;
    tmp.body.response = resp;
    pb_release(rtcvish_emulator_v1_Message_fields, &tmp);

    uint32_t id = resp.id;
    std::memset(&resp, 0, sizeof(resp));
    resp.id = id;
    resp.which_body = rtcvish_emulator_v1_Response_error_tag;
    resp.body.error.code = rtcvish_emulator_v1_Error_Code(code);
    resp.body.error.message = copyString(message);
}

void toPb(const Status& in, rtcvish_emulator_v1_Status& out) {
    out.state = rtcvish_emulator_v1_Status_State(in.state);
    out.frame = in.frame;
    out.rom_path = copyString(in.romPath);
    out.game_title = copyString(in.gameTitle);
    out.game_code = copyString(in.gameCode);
    out.console = copyString(in.console);
}

void toPb(const Domain& in, rtcvish_emulator_v1_Domain& out) {
    out.name = copyString(in.name);
    out.size = in.size;
    out.word_size = in.wordSize;
    out.big_endian = in.bigEndian;
    out.writable = in.writable;
    out.hidden = in.hidden;
}

void toPb(const Capabilities& in, rtcvish_emulator_v1_Capabilities& out) {
    out.savestates = in.savestates;
    out.screenshot = in.screenshot;
    out.input = in.input;
    out.load_rom = in.loadRom;
    out.reset = in.reset;
    out.max_payload = in.maxPayload;
    out.scanline_units = in.scanlineUnits;
    out.hard_units = in.hardUnits;
}

void toPb(const Unit& in, rtcvish_emulator_v1_Unit& out, bool includeValue) {
    out.id = in.id;
    out.domain = copyString(in.domain);
    out.address = in.address;
    out.size = in.size;
    if (in.isStore) {
        out.which_source = rtcvish_emulator_v1_Unit_store_tag;
        out.source.store.domain = copyString(in.storeDomain);
        out.source.store.address = in.storeAddress;
        out.source.store.continuous = in.continuous;
    } else {
        out.which_source = rtcvish_emulator_v1_Unit_value_tag;
        out.source.value =
            includeValue ? copyBytes(in.value.data(), in.value.size()) : allocBytes(0);
    }
    out.tilt = in.tilt;
    out.delay = in.delay;
    out.lifetime = in.lifetime;
    out.loop = in.loop;
    out.loop_delay = in.loopDelay;
    out.mode = rtcvish_emulator_v1_Mode(in.mode);
}

Unit fromPb(const rtcvish_emulator_v1_Unit& in) {
    Unit u;
    u.id = in.id;
    u.domain = toString(in.domain);
    u.address = in.address;
    u.size = in.size;
    if (in.which_source == rtcvish_emulator_v1_Unit_store_tag) {
        u.isStore = true;
        u.storeDomain = toString(in.source.store.domain);
        u.storeAddress = in.source.store.address;
        u.continuous = in.source.store.continuous;
    } else if (in.which_source == rtcvish_emulator_v1_Unit_value_tag && in.source.value) {
        u.value.assign(in.source.value->bytes, in.source.value->bytes + in.source.value->size);
    }
    u.tilt = in.tilt;
    u.delay = in.delay;
    u.lifetime = in.lifetime;
    u.loop = in.loop;
    u.loopDelay = in.loop_delay;
    u.mode = UnitMode(in.mode);
    return u;
}

} // namespace wire
} // namespace rtcvish
