// TCP server for the rtcv-ish emulator API.
//
// The server owns a socket thread (accept, read, decode, answer the requests
// that need no emulator state) and a writer thread (send responses and
// events). Requests that need emulator state become jobs, which the emulator
// runs on its own thread by calling pollJobs() at a frame boundary.
//
// Emulator integration, all on the emulation thread:
//
//   loop:
//       server.pollJobs();                // every iteration, running or paused
//       if (frame will be emulated) {
//           server.runFrame();            // unit scheduler, before emulating
//           emulateFrame();               // server.runScanline() per scanline
//           server.frameCompleted(++frameCounter);
//       } else {
//           server.waitForJobs(timeout);  // instead of sleeping while paused
//       }
//
// Call statusChanged() after resets or ROM loads triggered from the emulator's
// own UI (pollJobs() detects other state changes by itself), and clear the
// scheduler when the game changes.
#pragma once

#include "Backend.h"
#include "Scheduler.h"
#include "Types.h"

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace rtcvish {

namespace wire {
class OwnedMessage;
}

class Server {
public:
    static constexpr uint32_t kProtocolVersion = 1;

    explicit Server(Backend& backend);
    ~Server();
    Server(const Server&) = delete;
    Server& operator=(const Server&) = delete;

    // Split "HOST:PORT" ("[::1]:PORT" for IPv6).
    static bool parseAddress(const std::string& address, std::string& host, uint16_t& port);

    // Bind and start the threads. Calls Backend::hello() on this thread.
    bool start(const std::string& host, uint16_t port, std::string& err);

    // Stop the threads and close all sockets. Pending jobs are dropped.
    void stop();

    // --- Emulation thread ---

    // Run all queued jobs, then refresh the cached status (emitting a
    // StatusEvent if it changed).
    void pollJobs();

    // Block until a job is queued or the timeout expires. True if jobs are
    // pending.
    bool waitForJobs(std::chrono::milliseconds timeout);

    // Execute the unit scheduler for the frame about to be emulated.
    void runFrame();

    // Rewrite SCANLINE and HARD units; call at every scanline of the frame
    // (only needed while scheduler().scanlineActive()).
    void runScanline();

    // Report an emulated frame; `frame` is the backend's frame counter after
    // the frame. Sends FrameEvents and completes Step requests.
    void frameCompleted(uint64_t frame);

    // Refresh the cached status and always emit a StatusEvent.
    void statusChanged();

    Scheduler& scheduler() { return scheduler_; }

private:
    struct Connection;
    using ConnPtr = std::shared_ptr<Connection>;
    using MsgPtr = std::shared_ptr<wire::OwnedMessage>;

    struct PendingStep {
        ConnPtr conn;
        uint32_t id = 0;
        uint32_t remaining = 0;
    };

    void socketLoop();
    void writerLoop();
    void closeConnection(const ConnPtr& conn);
    bool handleFrame(const ConnPtr& conn, const uint8_t* data, size_t size);
    void handleRequest(const ConnPtr& conn, const MsgPtr& msg);
    void runJob(const ConnPtr& conn, const MsgPtr& msg);

    void send(const ConnPtr& conn, const wire::OwnedMessage& msg,
              std::function<void()> after = nullptr);
    void sendEvent(const wire::OwnedMessage& msg);
    void sendError(const ConnPtr& conn, uint32_t id, ErrorCode code, const std::string& message);
    void enqueueJob(std::function<void()> job);
    ConnPtr currentConnection();

    void refreshStatus(bool force);
    void abortStep(const std::string& reason);
    bool requireRom(const ConnPtr& conn, uint32_t id);

    Backend& backend_;
    Scheduler scheduler_;
    Info info_;

    std::atomic<bool> stopping_{false};
    std::thread socketThread_;
    std::thread writerThread_;
    intptr_t listenFd_ = -1;

    std::mutex connMu_;
    ConnPtr conn_;

    struct OutItem {
        ConnPtr conn;
        std::vector<uint8_t> data;
        // Runs on the writer thread once `data` was sent (or dropped).
        std::function<void()> after;
    };

    std::mutex outMu_;
    std::condition_variable outCv_;
    std::deque<OutItem> outQueue_;

    std::mutex jobsMu_;
    std::condition_variable jobsCv_;
    std::deque<std::function<void()>> jobs_;

    std::mutex statusMu_;
    Status status_;
    bool statusValid_ = false;

    // Emulation thread only.
    PendingStep step_;
};

} // namespace rtcvish
