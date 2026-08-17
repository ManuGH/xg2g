// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Network

/// Lightweight embedded HTTP telemetry server running directly inside the iOS app.
/// Allows the development environment to query live telemetry and logs via HTTP at any time.
public final class TelemetryServer: @unchecked Sendable {
    public static let shared = TelemetryServer()

    private var listener: NWListener?
    private let queue = DispatchQueue(label: "io.github.manugh.xg2g.telemetryserver", qos: .utility)
    private var logRingBuffer: [String] = []
    private let lock = NSLock()
    private let maxLogEntries = 500
    private var currentTelemetryProvider: (@Sendable () -> [String: Any])?
    private var screenshotProvider: (@MainActor @Sendable () -> Data?)?

    /// Reused: constructing a formatter per log line is expensive, and `log` is
    /// called from the render path. `ISO8601DateFormatter` is documented as safe
    /// for concurrent formatting, matching `HTTPAPIClient`'s shared formatters.
    nonisolated(unsafe) private static let timestampFormatter = ISO8601DateFormatter()

    /// Upper bound on how long a `/screenshot` request may wait for the main
    /// thread. Without it a stalled main thread would pin a server connection
    /// indefinitely.
    private static let screenshotTimeout: TimeInterval = 2.0

    public var port: UInt16 = 8099

    public init() {}

    public func setTelemetryProvider(_ provider: @escaping @Sendable () -> [String: Any]) {
        lock.lock()
        defer { lock.unlock() }
        self.currentTelemetryProvider = provider
    }

    public func setScreenshotProvider(_ provider: @escaping @MainActor @Sendable () -> Data?) {
        lock.lock()
        defer { lock.unlock() }
        self.screenshotProvider = provider
    }

    public func log(_ message: String) {
        let timestamp = Self.timestampFormatter.string(from: Date())
        let entry = "[\(timestamp)] \(message)"
        lock.lock()
        logRingBuffer.append(entry)
        if logRingBuffer.count > maxLogEntries {
            logRingBuffer.removeFirst(logRingBuffer.count - maxLogEntries)
        }
        lock.unlock()
    }

    /// Starts the diagnostic listener. Debug builds only.
    ///
    /// The endpoints are unauthenticated and bind every interface, and
    /// `/screenshot` returns a live capture of whatever is on screen. That is
    /// acceptable on a development device and would be a privacy hole in a
    /// shipped build, so release builds never open the socket. `log` stays live
    /// either way — it only feeds an in-process ring buffer.
    public func start() {
#if !DEBUG
        return
#else
        guard listener == nil else { return }

        do {
            let parameters = NWParameters.tcp
            parameters.allowLocalEndpointReuse = true
            let listener = try NWListener(using: parameters, on: NWEndpoint.Port(rawValue: port)!)
            
            listener.newConnectionHandler = { [weak self] connection in
                self?.handleConnection(connection)
            }

            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    print("[TelemetryServer] 🚀 Live Telemetry Server listening on port \(self.port)")
                case .failed(let error):
                    print("[TelemetryServer] ❌ Listener failed: \(error)")
                default:
                    break
                }
            }

            listener.start(queue: queue)
            self.listener = listener
        } catch {
            print("[TelemetryServer] ❌ Failed to create NWListener: \(error)")
        }
#endif
    }

    public func stop() {
        listener?.cancel()
        listener = nil
    }

    private func handleConnection(_ connection: NWConnection) {
        connection.start(queue: queue)
        receiveRequest(connection: connection)
    }

    private func receiveRequest(connection: NWConnection) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 4096) { [weak self] content, _, isComplete, error in
            guard let self = self else { return }
            if let data = content, let requestString = String(data: data, encoding: .utf8) {
                let responseData = self.generateHTTPResponse(for: requestString)
                connection.send(content: responseData, completion: .contentProcessed({ _ in
                    connection.cancel()
                }))
            } else {
                connection.cancel()
            }
        }
    }

    /// Grabs a frame from the render view on the main thread, giving up after
    /// `screenshotTimeout` rather than blocking the server queue forever.
    private func captureScreenshot() -> Data? {
        lock.lock()
        let provider = screenshotProvider
        lock.unlock()

        guard let provider else { return nil }

        if Thread.isMainThread {
            return MainActor.assumeIsolated { provider() }
        }

        let box = ScreenshotBox()
        let semaphore = DispatchSemaphore(value: 0)
        DispatchQueue.main.async {
            box.store(MainActor.assumeIsolated { provider() })
            semaphore.signal()
        }

        guard semaphore.wait(timeout: .now() + Self.screenshotTimeout) == .success else {
            return nil
        }
        return box.take()
    }

    private func generateHTTPResponse(for request: String) -> Data {
        let firstLine = request.components(separatedBy: "\r\n").first ?? ""
        let parts = firstLine.components(separatedBy: " ")
        let path = parts.count > 1 ? parts[1] : "/"

        if path.hasPrefix("/screenshot") || path.hasPrefix("/frame") {
            if let jpeg = captureScreenshot() {
                let headers = "HTTP/1.1 200 OK\r\nContent-Type: image/jpeg\r\nContent-Length: \(jpeg.count)\r\nAccess-Control-Allow-Origin: *\r\nConnection: close\r\n\r\n"
                var response = headers.data(using: .utf8)!
                response.append(jpeg)
                return response
            } else {
                let notFound = "No frame or screenshot available".data(using: .utf8)!
                let headers = "HTTP/1.1 404 Not Found\r\nContent-Type: text/plain\r\nContent-Length: \(notFound.count)\r\nConnection: close\r\n\r\n"
                var response = headers.data(using: .utf8)!
                response.append(notFound)
                return response
            }
        }

        if path.hasPrefix("/logs") {
            lock.lock()
            let entries = logRingBuffer
            lock.unlock()
            let logsText = entries.joined(separator: "\n")

            let payload = logsText.data(using: .utf8) ?? Data()
            let headers = "HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: \(payload.count)\r\nAccess-Control-Allow-Origin: *\r\nConnection: close\r\n\r\n"
            var response = headers.data(using: .utf8)!
            response.append(payload)
            return response
        }

        // Default: /telemetry JSON
        //
        // The provider is fetched under the lock but invoked outside it: `lock` is
        // an NSLock and therefore not reentrant, so any provider that logged would
        // otherwise deadlock the whole app against itself.
        lock.lock()
        let provider = currentTelemetryProvider
        let recentLogs = Array(logRingBuffer.suffix(30))
        lock.unlock()

        var combined: [String: Any] = provider?() ?? [:]
        combined["recent_logs"] = recentLogs
        combined["server_time"] = Self.timestampFormatter.string(from: Date())

        let jsonData = (try? JSONSerialization.data(withJSONObject: combined, options: [.prettyPrinted])) ?? Data()
        let headers = "HTTP/1.1 200 OK\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: \(jsonData.count)\r\nAccess-Control-Allow-Origin: *\r\nConnection: close\r\n\r\n"
        var response = headers.data(using: .utf8)!
        response.append(jsonData)
        return response
    }
}

/// Carries a screenshot across the main-thread hop in `captureScreenshot`.
///
/// A locked box rather than a captured `var`: if the wait times out, the main
/// thread may still write its result afterwards, and that write must not race
/// the server queue's read.
private final class ScreenshotBox: @unchecked Sendable {
    private var data: Data?
    private let lock = NSLock()

    func store(_ value: Data?) {
        lock.lock()
        defer { lock.unlock() }
        data = value
    }

    func take() -> Data? {
        lock.lock()
        defer { lock.unlock() }
        return data
    }
}
