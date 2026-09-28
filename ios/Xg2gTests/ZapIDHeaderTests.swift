// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// The transport request is inspected before any receiver response is needed.
@MainActor
@Suite(.serialized)
struct ZapIDHeaderTests {
    private let streamURL = URL(string: "http://127.0.0.1:1/live")!

    private func prestartedPipeline() -> NativeTSVideoPipeline {
        let pipeline = NativeTSVideoPipeline()
        pipeline.startStreaming(url: streamURL)
        pipeline.stopStreaming()
        return pipeline
    }

    private func eventually(_ condition: @MainActor () -> Bool) async -> Bool {
        for _ in 0..<200 {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(5))
        }
        return condition()
    }

    @Test("An explicit zap ID becomes the live request header")
    func explicitZapID() throws {
        let pipeline = NativeTSVideoPipeline()
        pipeline.startStreaming(url: streamURL, zapID: "ios-test-override-42")
        defer { pipeline.stopStreaming() }

        let request = try #require(pipeline.currentStreamRequestForTesting)
        #expect(request.value(forHTTPHeaderField: "X-Xg2g-Zap-Id") == "ios-test-override-42")
        #expect(pipeline.currentZapId == 1)
    }

    @Test("Callers without a zap ID retain the pipeline-local header")
    func defaultZapID() throws {
        let pipeline = NativeTSVideoPipeline()
        pipeline.startStreaming(url: streamURL)
        defer { pipeline.stopStreaming() }

        let request = try #require(pipeline.currentStreamRequestForTesting)
        #expect(request.value(forHTTPHeaderField: "X-Xg2g-Zap-Id")
                == NativeTSVideoPipeline.zapIdentifier(pipeline.currentZapId))
    }

    @Test("An unprepared coordinator start forwards its ID across a fresh stream")
    func directCoordinatorZapID() async throws {
        let pipeline = prestartedPipeline()
        let coordinator = ZapCoordinator(streamURL: { _ in nil }, makeSession: { pipeline })

        await coordinator.play(unprepared: streamURL)
        let request = pipeline.currentStreamRequestForTesting
        await coordinator.stop()

        #expect(pipeline.currentZapId == 2)
        #expect(try #require(request).value(forHTTPHeaderField: "X-Xg2g-Zap-Id")
                == NativeTSVideoPipeline.zapIdentifier(1))
    }

    @Test("Prepare and live stream use the same coordinator zap ID")
    func preparedCoordinatorZapID() async throws {
        ZapIDReadyURLProtocol.reset()
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ZapIDReadyURLProtocol.self]
        let api = HTTPAPIClient(
            address: try ServerAddressParser.parseTrusted("http://example.test:8089/"),
            session: URLSession(configuration: config)
        )
        let preparation = ZapPreparationClient(api: api, clientID: "zap-id-test")
        let pipeline = prestartedPipeline()
        let coordinator = ZapCoordinator(
            preparations: preparation,
            streamURL: { _ in self.streamURL },
            makeSession: { pipeline }
        )

        let zapTask = Task { await coordinator.zap(to: "1:0:19:132F:3EF:1:C00000:0:0:0:") }
        let opened = await eventually { pipeline.currentZapId == 2 && pipeline.currentStreamRequestForTesting != nil }
        let prepareRequest = ZapIDReadyURLProtocol.lastRequest
        let streamRequest = pipeline.currentStreamRequestForTesting
        await coordinator.stop()
        zapTask.cancel()
        await zapTask.value

        #expect(opened)
        let prepareID = try #require(prepareRequest?.value(forHTTPHeaderField: "X-Xg2g-Zap-Id"))
        let streamID = try #require(streamRequest?.value(forHTTPHeaderField: "X-Xg2g-Zap-Id"))
        #expect(streamID == prepareID)
        #expect(streamID == NativeTSVideoPipeline.zapIdentifier(1))
    }
}

/// A private preparation transport; the live request is read from its URLSessionTask.
private final class ZapIDReadyURLProtocol: URLProtocol {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var storedRequest: URLRequest?

    static var lastRequest: URLRequest? {
        lock.lock(); defer { lock.unlock() }
        return storedRequest
    }

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        storedRequest = nil
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.lock()
        Self.storedRequest = request
        Self.lock.unlock()

        let response = HTTPURLResponse(
            url: request.url!, statusCode: 202, httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"]
        )!
        let body = Data(#"{"preparationId":"prep-zap-id","state":"ready","generation":1}"#.utf8)
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
