#include "Server.h"

#include "Wire.h"

#include <cstring>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <cerrno>
#include <csignal>
#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>
#endif

namespace rtcvish {

namespace {

#ifdef _WIN32
using socket_t = SOCKET;
const socket_t kInvalidSocket = INVALID_SOCKET;
const int kShutdownBoth = SD_BOTH;
void closeSocket(socket_t s) {
    closesocket(s);
}
bool interrupted() {
    return false;
}
#else
using socket_t = int;
const socket_t kInvalidSocket = -1;
const int kShutdownBoth = SHUT_RDWR;
void closeSocket(socket_t s) {
    close(s);
}
bool interrupted() {
    return errno == EINTR;
}
#endif

#ifdef MSG_NOSIGNAL
const int kSendFlags = MSG_NOSIGNAL;
#else
const int kSendFlags = 0;
#endif

constexpr size_t kRecvChunk = 256 * 1024;
constexpr size_t kSendChunk = 1024 * 1024;
constexpr long kSelectTimeoutUs = 200 * 1000;

bool sendAll(socket_t fd, const uint8_t* data, size_t size) {
    while (size > 0) {
        size_t n = size < kSendChunk ? size : kSendChunk;
        auto r = ::send(fd, reinterpret_cast<const char*>(data), int(n), kSendFlags);
        if (r < 0 && interrupted()) {
            continue;
        }
        if (r <= 0) {
            return false;
        }
        data += r;
        size -= size_t(r);
    }
    return true;
}

void setSendTimeout(socket_t fd, int ms) {
#ifdef _WIN32
    DWORD tv = DWORD(ms);
#else
    timeval tv{ms / 1000, (ms % 1000) * 1000};
#endif
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, reinterpret_cast<const char*>(&tv), sizeof(tv));
}

} // namespace

struct Server::Connection {
    explicit Connection(socket_t s) : fd(s) {}
    ~Connection() { closeSocket(fd); }

    void shutdownNow() {
        if (!closed.exchange(true)) {
            ::shutdown(fd, kShutdownBoth);
        }
    }

    socket_t fd;
    std::atomic<bool> closed{false};
    std::atomic<bool> helloDone{false};
    std::atomic<uint32_t> frameInterval{0};
};

Server::Server(Backend& backend) : backend_(backend) {}

Server::~Server() {
    stop();
}

bool Server::parseAddress(const std::string& address, std::string& host, uint16_t& port) {
    size_t colon = address.rfind(':');
    if (colon == std::string::npos || colon + 1 >= address.size()) {
        return false;
    }
    std::string h = address.substr(0, colon);
    if (h.size() >= 2 && h.front() == '[' && h.back() == ']') {
        h = h.substr(1, h.size() - 2);
    }
    unsigned long p = 0;
    for (char c : address.substr(colon + 1)) {
        if (c < '0' || c > '9') {
            return false;
        }
        p = p * 10 + unsigned(c - '0');
        if (p > 65535) {
            return false;
        }
    }
    host = h.empty() ? "127.0.0.1" : h;
    port = uint16_t(p);
    return true;
}

bool Server::start(const std::string& host, uint16_t port, std::string& err) {
    if (socketThread_.joinable()) {
        err = "server already started";
        return false;
    }

#ifdef _WIN32
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        err = "WSAStartup failed";
        return false;
    }
#else
    signal(SIGPIPE, SIG_IGN);
#endif

    addrinfo hints;
    std::memset(&hints, 0, sizeof(hints));
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_flags = AI_PASSIVE | AI_NUMERICSERV;
    addrinfo* res = nullptr;
    std::string portStr = std::to_string(port);
    if (getaddrinfo(host.c_str(), portStr.c_str(), &hints, &res) != 0 || !res) {
        err = "cannot resolve " + host;
        return false;
    }

    socket_t fd = kInvalidSocket;
    for (addrinfo* ai = res; ai; ai = ai->ai_next) {
        fd = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
        if (fd == kInvalidSocket) {
            continue;
        }
        int one = 1;
#ifdef _WIN32
        setsockopt(fd, SOL_SOCKET, SO_EXCLUSIVEADDRUSE, reinterpret_cast<const char*>(&one),
                   sizeof(one));
#else
        setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
#endif
        if (bind(fd, ai->ai_addr, int(ai->ai_addrlen)) == 0 && listen(fd, 4) == 0) {
            break;
        }
        closeSocket(fd);
        fd = kInvalidSocket;
    }
    freeaddrinfo(res);
    if (fd == kInvalidSocket) {
        err = "cannot listen on " + host + ":" + portStr;
        return false;
    }

    info_ = backend_.hello();
    if (info_.capabilities.maxPayload == 0 || info_.capabilities.maxPayload > wire::kMaxPayload) {
        info_.capabilities.maxPayload = wire::kMaxPayload;
    }

    listenFd_ = intptr_t(fd);
    stopping_ = false;
    socketThread_ = std::thread(&Server::socketLoop, this);
    writerThread_ = std::thread(&Server::writerLoop, this);
    return true;
}

void Server::stop() {
    if (!socketThread_.joinable() && !writerThread_.joinable()) {
        return;
    }
    stopping_ = true;
    jobsCv_.notify_all();
    if (ConnPtr c = currentConnection()) {
        setSendTimeout(c->fd, 2000);
    }
    {
        std::lock_guard<std::mutex> lk(outMu_);
        outCv_.notify_all();
    }
    // The writer drains queued messages first so that e.g. a QuitResponse
    // reaches the client.
    if (writerThread_.joinable()) {
        writerThread_.join();
    }
    if (socketThread_.joinable()) {
        socketThread_.join();
    }
    closeSocket(socket_t(listenFd_));
    listenFd_ = -1;
    {
        std::lock_guard<std::mutex> lk(jobsMu_);
        jobs_.clear();
    }
    step_ = PendingStep();
#ifdef _WIN32
    WSACleanup();
#endif
}

Server::ConnPtr Server::currentConnection() {
    std::lock_guard<std::mutex> lk(connMu_);
    return conn_;
}

void Server::closeConnection(const ConnPtr& conn) {
    conn->shutdownNow();
    std::lock_guard<std::mutex> lk(connMu_);
    if (conn_ == conn) {
        conn_.reset();
    }
}

void Server::socketLoop() {
    socket_t lfd = socket_t(listenFd_);
    ConnPtr conn;
    std::vector<uint8_t> buf;
    std::vector<uint8_t> chunk(kRecvChunk);

    while (!stopping_) {
        if (conn && conn->closed) {
            closeConnection(conn);
            conn.reset();
        }

        fd_set rd;
        FD_ZERO(&rd);
        FD_SET(lfd, &rd);
        socket_t maxfd = lfd;
        if (conn) {
            FD_SET(conn->fd, &rd);
            if (conn->fd > maxfd) {
                maxfd = conn->fd;
            }
        }
        timeval tv{0, kSelectTimeoutUs};
        int n = select(int(maxfd + 1), &rd, nullptr, nullptr, &tv);
        if (n <= 0) {
            continue;
        }

        if (FD_ISSET(lfd, &rd)) {
            socket_t c = accept(lfd, nullptr, nullptr);
            if (c != kInvalidSocket) {
                if (conn) {
                    // One client at a time.
                    closeSocket(c);
                } else {
                    int one = 1;
                    setsockopt(c, IPPROTO_TCP, TCP_NODELAY, reinterpret_cast<const char*>(&one),
                               sizeof(one));
#ifdef SO_NOSIGPIPE
                    setsockopt(c, SOL_SOCKET, SO_NOSIGPIPE, &one, sizeof(one));
#endif
                    conn = std::make_shared<Connection>(c);
                    buf.clear();
                    std::lock_guard<std::mutex> lk(connMu_);
                    conn_ = conn;
                }
            }
        }

        if (!conn || !FD_ISSET(conn->fd, &rd)) {
            continue;
        }
        auto r = recv(conn->fd, reinterpret_cast<char*>(chunk.data()), int(chunk.size()), 0);
        if (r < 0 && interrupted()) {
            continue;
        }
        if (r <= 0) {
            closeConnection(conn);
            conn.reset();
            continue;
        }
        buf.insert(buf.end(), chunk.begin(), chunk.begin() + r);

        size_t off = 0;
        bool ok = true;
        while (buf.size() - off >= wire::kHeaderSize) {
            uint32_t len = wire::readLength(buf.data() + off);
            if (len > wire::kMaxMessageSize) {
                ok = false;
                break;
            }
            if (buf.size() - off - wire::kHeaderSize < len) {
                buf.reserve(off + wire::kHeaderSize + len);
                break;
            }
            if (!handleFrame(conn, buf.data() + off + wire::kHeaderSize, len)) {
                ok = false;
                break;
            }
            off += wire::kHeaderSize + len;
        }
        if (!ok) {
            closeConnection(conn);
            conn.reset();
            continue;
        }
        buf.erase(buf.begin(), buf.begin() + off);
    }

    if (conn) {
        closeConnection(conn);
    }
}

void Server::writerLoop() {
    for (;;) {
        OutItem item;
        {
            std::unique_lock<std::mutex> lk(outMu_);
            outCv_.wait(lk, [&] { return stopping_ || !outQueue_.empty(); });
            if (outQueue_.empty()) {
                return;
            }
            item = std::move(outQueue_.front());
            outQueue_.pop_front();
        }
        if (!item.conn->closed && !sendAll(item.conn->fd, item.data.data(), item.data.size())) {
            item.conn->shutdownNow();
        }
        if (item.after) {
            item.after();
        }
    }
}

void Server::send(const ConnPtr& conn, const wire::OwnedMessage& msg, std::function<void()> after) {
    if (!conn || conn->closed) {
        if (after) {
            after();
        }
        return;
    }
    OutItem item;
    item.conn = conn;
    item.after = std::move(after);
    if (!wire::encodeFrame(msg.get(), item.data)) {
        const wire::Message& m = msg.get();
        if (m.which_body != rtcvish_emulator_v1_Message_response_tag) {
            return;
        }
        wire::OwnedMessage err;
        wire::setError(err.response(m.body.response.id), ErrorCode::Failed, "response too large");
        if (!wire::encodeFrame(err.get(), item.data)) {
            return;
        }
    }
    std::lock_guard<std::mutex> lk(outMu_);
    outQueue_.push_back(std::move(item));
    outCv_.notify_one();
}

void Server::sendEvent(const wire::OwnedMessage& msg) {
    ConnPtr c = currentConnection();
    if (c && c->helloDone) {
        send(c, msg);
    }
}

void Server::sendError(const ConnPtr& conn, uint32_t id, ErrorCode code,
                       const std::string& message) {
    wire::OwnedMessage out;
    wire::setError(out.response(id), code, message);
    send(conn, out);
}

void Server::enqueueJob(std::function<void()> job) {
    std::lock_guard<std::mutex> lk(jobsMu_);
    jobs_.push_back(std::move(job));
    jobsCv_.notify_all();
}

bool Server::handleFrame(const ConnPtr& conn, const uint8_t* data, size_t size) {
    MsgPtr msg = wire::decode(data, size);
    if (!msg || msg->get().which_body != rtcvish_emulator_v1_Message_request_tag) {
        return false;
    }
    handleRequest(conn, msg);
    return true;
}

void Server::handleRequest(const ConnPtr& conn, const MsgPtr& msg) {
    const wire::Request& req = msg->get().body.request;
    uint32_t id = req.id;

    if (req.which_body == rtcvish_emulator_v1_Request_hello_tag) {
        if (req.body.hello.protocol_version != kProtocolVersion) {
            sendError(conn, id, ErrorCode::Unsupported,
                      "unsupported protocol version " +
                          std::to_string(req.body.hello.protocol_version));
            return;
        }
        wire::OwnedMessage out;
        wire::Response& resp = out.response(id);
        resp.which_body = rtcvish_emulator_v1_Response_hello_tag;
        auto& h = resp.body.hello;
        h.protocol_version = kProtocolVersion;
        h.emulator = wire::copyString(info_.emulator);
        h.version = wire::copyString(info_.version);
        h.system = wire::copyString(info_.system);
        h.has_capabilities = true;
        wire::toPb(info_.capabilities, h.capabilities);
        conn->helloDone = true;
        send(conn, out);
        return;
    }

    if (!conn->helloDone) {
        sendError(conn, id, ErrorCode::InvalidArgument, "the first request must be Hello");
        return;
    }

    wire::OwnedMessage out;
    wire::Response& resp = out.response(id);
    switch (req.which_body) {
    case rtcvish_emulator_v1_Request_ping_tag:
        resp.which_body = rtcvish_emulator_v1_Response_ping_tag;
        send(conn, out);
        return;
    case rtcvish_emulator_v1_Request_subscribe_tag:
        conn->frameInterval = req.body.subscribe.frame_interval;
        resp.which_body = rtcvish_emulator_v1_Response_subscribe_tag;
        send(conn, out);
        return;
    case rtcvish_emulator_v1_Request_get_status_tag: {
        resp.which_body = rtcvish_emulator_v1_Response_get_status_tag;
        resp.body.get_status.has_status = true;
        std::lock_guard<std::mutex> lk(statusMu_);
        wire::toPb(status_, resp.body.get_status.status);
        break;
    }
    case rtcvish_emulator_v1_Request_quit_tag:
        resp.which_body = rtcvish_emulator_v1_Response_quit_tag;
        send(conn, out, [this] { enqueueJob([this] { backend_.quit(); }); });
        return;
    case 0:
        wire::setError(resp, ErrorCode::InvalidArgument, "request has no body");
        break;
    default:
        enqueueJob([this, conn, msg] { runJob(conn, msg); });
        return;
    }
    send(conn, out);
}

bool Server::requireRom(const ConnPtr& conn, uint32_t id) {
    if (backend_.status().state != State::NoRom) {
        return true;
    }
    sendError(conn, id, ErrorCode::NoRom, "no ROM loaded");
    return false;
}

void Server::runJob(const ConnPtr& conn, const MsgPtr& msg) {
    const wire::Request& req = msg->get().body.request;
    const uint32_t id = req.id;
    wire::OwnedMessage out;
    wire::Response& resp = out.response(id);
    Error err;

    switch (req.which_body) {
    case rtcvish_emulator_v1_Request_list_domains_tag: {
        std::vector<Domain> ds = backend_.domains();
        resp.which_body = rtcvish_emulator_v1_Response_list_domains_tag;
        auto& r = resp.body.list_domains;
        r.domains = wire::allocArray<rtcvish_emulator_v1_Domain>(ds.size());
        r.domains_count = pb_size_t(ds.size());
        for (size_t i = 0; i < ds.size(); i++) {
            wire::toPb(ds[i], r.domains[i]);
        }
        break;
    }

    case rtcvish_emulator_v1_Request_read_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        const auto& rq = req.body.read;
        std::vector<Domain> ds = backend_.domains();
        uint64_t total = 0;
        bool ok = true;
        for (pb_size_t i = 0; i < rq.ranges_count && ok; i++) {
            const auto& rg = rq.ranges[i];
            std::string name = wire::toString(rg.domain);
            const Domain* d = findDomain(ds, name);
            if (!d) {
                ok = err.set(ErrorCode::NotFound, "unknown domain " + name);
            } else if (!inRange(*d, rg.address, rg.size)) {
                ok = err.set(ErrorCode::OutOfRange, "range outside " + name);
            }
            total += rg.size;
        }
        if (ok && total > info_.capabilities.maxPayload) {
            ok = err.set(ErrorCode::OutOfRange, "read exceeds max_payload");
        }
        if (!ok) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_read_tag;
        auto& r = resp.body.read;
        r.data = wire::allocArray<pb_bytes_array_t*>(rq.ranges_count);
        r.data_count = rq.ranges_count;
        for (pb_size_t i = 0; i < rq.ranges_count; i++) {
            const auto& rg = rq.ranges[i];
            r.data[i] = wire::allocBytes(rg.size);
            if (!backend_.read(wire::toString(rg.domain), rg.address, rg.size, r.data[i]->bytes,
                               err)) {
                wire::setError(resp, err);
                break;
            }
        }
        break;
    }

    case rtcvish_emulator_v1_Request_write_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        const auto& rq = req.body.write;
        std::vector<Domain> ds = backend_.domains();
        bool ok = true;
        for (pb_size_t i = 0; i < rq.chunks_count && ok; i++) {
            const auto& ch = rq.chunks[i];
            std::string name = wire::toString(ch.domain);
            uint32_t size = ch.data ? ch.data->size : 0;
            const Domain* d = findDomain(ds, name);
            if (!d) {
                ok = err.set(ErrorCode::NotFound, "unknown domain " + name);
            } else if (!d->writable) {
                ok = err.set(ErrorCode::InvalidArgument, "domain " + name + " is read-only");
            } else if (!inRange(*d, ch.address, size)) {
                ok = err.set(ErrorCode::OutOfRange, "range outside " + name);
            }
        }
        for (pb_size_t i = 0; i < rq.chunks_count && ok; i++) {
            const auto& ch = rq.chunks[i];
            if (ch.data && ch.data->size > 0) {
                ok = backend_.write(wire::toString(ch.domain), ch.address, ch.data->bytes,
                                    ch.data->size, err);
            }
        }
        if (!ok) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_write_tag;
        break;
    }

    case rtcvish_emulator_v1_Request_save_state_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        std::vector<uint8_t> data;
        if (!backend_.saveState(data, err)) {
            wire::setError(resp, err);
        } else if (data.size() > info_.capabilities.maxPayload) {
            wire::setError(resp, ErrorCode::Failed, "savestate exceeds max_payload");
        } else {
            resp.which_body = rtcvish_emulator_v1_Response_save_state_tag;
            resp.body.save_state.data = wire::copyBytes(data.data(), data.size());
        }
        break;
    }

    case rtcvish_emulator_v1_Request_load_state_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        const pb_bytes_array_t* data = req.body.load_state.data;
        if (!data || data->size == 0) {
            wire::setError(resp, ErrorCode::InvalidArgument, "empty savestate");
        } else if (!backend_.loadState(data->bytes, data->size, err)) {
            wire::setError(resp, err);
        } else {
            resp.which_body = rtcvish_emulator_v1_Response_load_state_tag;
        }
        break;
    }

    case rtcvish_emulator_v1_Request_load_rom_tag: {
        abortStep("interrupted by LoadRom");
        scheduler_.clear();
        if (!backend_.loadRom(wire::toString(req.body.load_rom.path), err)) {
            wire::setError(resp, err);
            break;
        }
        refreshStatus(true);
        resp.which_body = rtcvish_emulator_v1_Response_load_rom_tag;
        resp.body.load_rom.has_status = true;
        wire::toPb(backend_.status(), resp.body.load_rom.status);
        break;
    }

    case rtcvish_emulator_v1_Request_close_rom_tag:
        abortStep("interrupted by CloseRom");
        scheduler_.clear();
        if (!backend_.closeRom(err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_close_rom_tag;
        break;

    case rtcvish_emulator_v1_Request_reset_tag:
        if (!requireRom(conn, id)) {
            return;
        }
        abortStep("interrupted by Reset");
        scheduler_.clear();
        if (!backend_.reset(err)) {
            wire::setError(resp, err);
            break;
        }
        refreshStatus(true);
        resp.which_body = rtcvish_emulator_v1_Response_reset_tag;
        break;

    case rtcvish_emulator_v1_Request_pause_tag:
        if (!requireRom(conn, id)) {
            return;
        }
        if (!backend_.pause(err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_pause_tag;
        break;

    case rtcvish_emulator_v1_Request_resume_tag:
        if (!requireRom(conn, id)) {
            return;
        }
        abortStep("interrupted by Resume");
        if (!backend_.resume(err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_resume_tag;
        break;

    case rtcvish_emulator_v1_Request_step_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        uint32_t frames = req.body.step.frames;
        if (frames == 0) {
            wire::setError(resp, ErrorCode::InvalidArgument, "frames must be > 0");
            break;
        }
        if (step_.remaining > 0) {
            wire::setError(resp, ErrorCode::Busy, "another Step is in progress");
            break;
        }
        if (!backend_.pause(err) || !backend_.runFrames(frames, err)) {
            wire::setError(resp, err);
            break;
        }
        step_.conn = conn;
        step_.id = id;
        step_.remaining = frames;
        // Answered from frameCompleted().
        return;
    }

    case rtcvish_emulator_v1_Request_apply_units_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        const auto& rq = req.body.apply_units;
        std::vector<Unit> units;
        units.reserve(rq.units_count);
        for (pb_size_t i = 0; i < rq.units_count; i++) {
            units.push_back(wire::fromPb(rq.units[i]));
        }
        if (!scheduler_.apply(units, backend_.domains(), err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_apply_units_tag;
        break;
    }

    case rtcvish_emulator_v1_Request_remove_units_tag: {
        const auto& rq = req.body.remove_units;
        scheduler_.remove(std::vector<uint64_t>(rq.ids, rq.ids + rq.ids_count));
        resp.which_body = rtcvish_emulator_v1_Response_remove_units_tag;
        break;
    }

    case rtcvish_emulator_v1_Request_clear_units_tag:
        scheduler_.clear();
        resp.which_body = rtcvish_emulator_v1_Response_clear_units_tag;
        break;

    case rtcvish_emulator_v1_Request_list_units_tag: {
        std::vector<Unit> units = scheduler_.list();
        resp.which_body = rtcvish_emulator_v1_Response_list_units_tag;
        auto& r = resp.body.list_units;
        r.units = wire::allocArray<rtcvish_emulator_v1_Unit>(units.size());
        r.units_count = pb_size_t(units.size());
        for (size_t i = 0; i < units.size(); i++) {
            wire::toPb(units[i], r.units[i]);
        }
        break;
    }

    case rtcvish_emulator_v1_Request_set_input_tag: {
        const auto& rq = req.body.set_input;
        Input in;
        in.buttons = rq.buttons;
        in.touch = rq.touch;
        in.touchX = rq.touch_x;
        in.touchY = rq.touch_y;
        in.clear = rq.clear;
        if (!backend_.setInput(in, err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_set_input_tag;
        break;
    }

    case rtcvish_emulator_v1_Request_screenshot_tag: {
        if (!requireRom(conn, id)) {
            return;
        }
        std::vector<Image> screens;
        if (!backend_.screenshot(screens, err)) {
            wire::setError(resp, err);
            break;
        }
        resp.which_body = rtcvish_emulator_v1_Response_screenshot_tag;
        auto& r = resp.body.screenshot;
        r.screens = wire::allocArray<rtcvish_emulator_v1_Image>(screens.size());
        r.screens_count = pb_size_t(screens.size());
        for (size_t i = 0; i < screens.size(); i++) {
            r.screens[i].width = screens[i].width;
            r.screens[i].height = screens[i].height;
            r.screens[i].rgba = wire::copyBytes(screens[i].rgba.data(), screens[i].rgba.size());
        }
        break;
    }

    default:
        wire::setError(resp, ErrorCode::InvalidArgument, "unknown request");
        break;
    }

    send(conn, out);
}

void Server::pollJobs() {
    std::deque<std::function<void()>> jobs;
    {
        std::lock_guard<std::mutex> lk(jobsMu_);
        jobs.swap(jobs_);
    }
    for (auto& job : jobs) {
        job();
    }
    refreshStatus(false);
}

bool Server::waitForJobs(std::chrono::milliseconds timeout) {
    std::unique_lock<std::mutex> lk(jobsMu_);
    jobsCv_.wait_for(lk, timeout, [&] { return !jobs_.empty() || stopping_; });
    return !jobs_.empty();
}

void Server::runFrame() {
    scheduler_.runFrame(backend_);
}

void Server::frameCompleted(uint64_t frame) {
    {
        std::lock_guard<std::mutex> lk(statusMu_);
        status_.frame = frame;
    }

    ConnPtr c = currentConnection();
    if (c && c->helloDone) {
        uint32_t interval = c->frameInterval;
        if (interval != 0 && frame % interval == 0) {
            wire::OwnedMessage out;
            wire::Event& ev = out.event();
            ev.which_body = rtcvish_emulator_v1_Event_frame_tag;
            ev.body.frame.frame = frame;
            send(c, out);
        }
    }

    if (step_.remaining > 0 && --step_.remaining == 0) {
        wire::OwnedMessage out;
        wire::Response& resp = out.response(step_.id);
        resp.which_body = rtcvish_emulator_v1_Response_step_tag;
        resp.body.step.frame = frame;
        ConnPtr conn = std::move(step_.conn);
        step_ = PendingStep();
        send(conn, out);
    }
}

void Server::statusChanged() {
    refreshStatus(true);
}

void Server::refreshStatus(bool force) {
    Status s = backend_.status();
    bool changed;
    {
        std::lock_guard<std::mutex> lk(statusMu_);
        changed = !statusValid_ || s.state != status_.state || s.romPath != status_.romPath ||
                  s.gameTitle != status_.gameTitle || s.gameCode != status_.gameCode ||
                  s.console != status_.console;
        status_ = s;
        statusValid_ = true;
    }
    if (s.state == State::NoRom) {
        abortStep("no ROM loaded");
    }
    if (!changed && !force) {
        return;
    }
    wire::OwnedMessage out;
    wire::Event& ev = out.event();
    ev.which_body = rtcvish_emulator_v1_Event_status_tag;
    ev.body.status.has_status = true;
    wire::toPb(s, ev.body.status.status);
    sendEvent(out);
}

void Server::abortStep(const std::string& reason) {
    if (step_.remaining == 0) {
        return;
    }
    backend_.cancelFrames();
    ConnPtr conn = std::move(step_.conn);
    uint32_t id = step_.id;
    step_ = PendingStep();
    sendError(conn, id, ErrorCode::Failed, "Step " + reason);
}

} // namespace rtcvish
