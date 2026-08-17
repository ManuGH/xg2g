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
    private var currentTelemetryProvider: (() -> [String: Any])?
    private var screenshotProvider: (@Sendable () -> Data?)?

    public var port: UInt16 = 8099

    public init() {}

    public func setTelemetryProvider(_ provider: @escaping () -> [String: Any]) {
        lock.lock()
        defer { lock.unlock() }
        self.currentTelemetryProvider = provider
    }

    public func setScreenshotProvider(_ provider: @escaping @Sendable () -> Data?) {
        lock.lock()
        defer { lock.unlock() }
        self.screenshotProvider = provider
    }

    public func log(_ message: String) {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let entry = "[\(timestamp)] \(message)"
        lock.lock()
        logRingBuffer.append(entry)
        if logRingBuffer.count > maxLogEntries {
            logRingBuffer.removeFirst(logRingBuffer.count - maxLogEntries)
        }
        lock.unlock()
    }

    public func start() {
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

    private func generateHTTPResponse(for request: String) -> Data {
        let firstLine = request.components(separatedBy: "\r\n").first ?? ""
        let parts = firstLine.components(separatedBy: " ")
        let path = parts.count > 1 ? parts[1] : "/"

        if path.hasPrefix("/screenshot") || path.hasPrefix("/frame") {
            var imageData: Data? = nil
            if Thread.isMainThread {
                imageData = self.screenshotProvider?()
            } else {
                DispatchQueue.main.sync {
                    imageData = self.screenshotProvider?()
                }
            }

            if let jpeg = imageData {
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
            let logsText = logRingBuffer.joined(separator: "\n")
            lock.unlock()

            let payload = logsText.data(using: .utf8) ?? Data()
            let headers = "HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: \(payload.count)\r\nAccess-Control-Allow-Origin: *\r\nConnection: close\r\n\r\n"
            var response = headers.data(using: .utf8)!
            response.append(payload)
            return response
        }

        // Default: /telemetry JSON
        lock.lock()
        let telemetryDict = currentTelemetryProvider?() ?? [:]
        let recentLogs = Array(logRingBuffer.suffix(30))
        lock.unlock()

        var combined: [String: Any] = telemetryDict
        combined["recent_logs"] = recentLogs
        combined["server_time"] = ISO8601DateFormatter().string(from: Date())

        let jsonData = (try? JSONSerialization.data(withJSONObject: combined, options: [.prettyPrinted])) ?? Data()
        let headers = "HTTP/1.1 200 OK\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: \(jsonData.count)\r\nAccess-Control-Allow-Origin: *\r\nConnection: close\r\n\r\n"
        var response = headers.data(using: .utf8)!
        response.append(jsonData)
        return response
    }
}
