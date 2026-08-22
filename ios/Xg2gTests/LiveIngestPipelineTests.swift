// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// The native pipeline against the real server, on real broadcast.
///
/// Every other pipeline test in this target feeds synthetic or captured bytes,
/// which cannot answer the question that matters after a server-side change to
/// the live ingest route: does the stream this server now produces actually
/// decode here. A capture proves the parser; only the wire proves the path.
///
/// Gated on reachability like `BackendContractTests`, and for the same reason:
/// an ordinary `xcodebuild test` on a machine with no staging server must skip
/// cleanly rather than fail, while a deliberate run against staging must not be
/// allowed to pass by skipping.
enum LiveIngest {
    /// Staging's v3 live ingest route. Overridable so this is not pinned to one host.
    static let baseURL: String = {
        let raw = ProcessInfo.processInfo.environment["XG2G_LIVE_BASE_URL"] ?? ""
        let trimmed = raw.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, !trimmed.contains("$(") else {
            return "http://10.10.55.14:8089/api/v3/stream/live"
        }
        return trimmed
    }()

    /// ORF1 HD: encrypted, so it also exercises the descrambled path end to end.
    static let serviceRef = ProcessInfo.processInfo.environment["XG2G_LIVE_SREF"]
        .flatMap { $0.isEmpty ? nil : $0 } ?? "1:0:19:132F:3EF:1:C00000:0:0:0:"

    static var streamURL: URL? { URL(string: "\(baseURL)/\(serviceRef)") }

    /// How long to watch. Long enough to cross several GOPs and let the audio
    /// cushion settle, short enough to stay a test.
    static let observationSeconds: UInt64 = 20

    /// Asks only whether the route answers 200, so an absent staging server
    /// skips instead of failing.
    ///
    /// The completion-handler form cannot be used: this route never ends, so
    /// the handler would not fire until the timeout and every run would skip -
    /// silently, because xcodebuild reports a fully skipped suite as success.
    /// The response has to be read from the delegate callback and the task
    /// cancelled there.
    static func isReachable() -> Bool {
        guard let url = streamURL else { return false }
        let probe = HeaderProbe()
        var request = URLRequest(url: url)
        request.timeoutInterval = 4
        let session = URLSession(configuration: .ephemeral, delegate: probe, delegateQueue: nil)
        defer { session.invalidateAndCancel() }
        let task = session.dataTask(with: request)
        task.resume()
        return probe.awaitStatus(timeout: 6) == 200
    }

    /// Captures the response status and cancels, so nothing is downloaded.
    private final class HeaderProbe: NSObject, URLSessionDataDelegate, @unchecked Sendable {
        private let semaphore = DispatchSemaphore(value: 0)
        private var status: Int = 0

        func urlSession(_ session: URLSession, dataTask: URLSessionDataTask,
                        didReceive response: URLResponse,
                        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void) {
            status = (response as? HTTPURLResponse)?.statusCode ?? 0
            completionHandler(.cancel)
            semaphore.signal()
        }

        func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
            semaphore.signal()
        }

        func awaitStatus(timeout: TimeInterval) -> Int {
            _ = semaphore.wait(timeout: .now() + timeout)
            return status
        }
    }
}

@Suite(.enabled(if: LiveIngest.isReachable()))
struct LiveIngestPipelineTests {

    /// Decoding is the only honest proof of picture: a frame count that rises
    /// means VideoToolbox accepted real elementary stream, which cannot happen
    /// on scrambled payload or on a stream whose parameter sets never arrived.
    @Test func nativePipelineDecodesLiveBroadcastFromV3Route() async throws {
        let url = try #require(LiveIngest.streamURL)

        let pipeline = NativeTSVideoPipeline()
        pipeline.startStreaming(url: url)
        defer { pipeline.stopStreaming() }

        try await Task.sleep(for: .seconds(LiveIngest.observationSeconds))

        let t = pipeline.telemetry.snapshot()

        // Printed unconditionally: when this fails the numbers are the finding.
        print("""
        === Sterling live telemetry (\(LiveIngest.serviceRef)) ===
        url                     \(url.absoluteString)
        ttfpTotalMs             \(t.ttfpTotalMs)
        ttfpNetworkMs           \(t.ttfpNetworkMs)
        ttfpPsiMs               \(t.ttfpPsiMs)
        ttfpParamSetsMs         \(t.ttfpParamSetsMs)
        ttfpIdrMs               \(t.ttfpIdrMs)
        ttfpDecodeMs            \(t.ttfpDecodeMs)
        ttfpMotionMs            \(t.ttfpMotionMs)
        tsBitrateKbps           \(t.tsBitrateKbps)
        buffersEmitted/decoded  \(t.sampleBuffersEmittedCount)/\(t.sampleBuffersDecodedCount)
        continuityErrors        \(t.continuityErrors)
        pesErrors               \(t.pesErrors)
        scrambledPackets        \(t.scrambledPackets)
        ptsDiscontinuities      \(t.ptsDiscontinuities)
        audioChannels/rate      \(t.audioChannels)/\(t.audioSampleRate)
        audioLeadMs             \(t.audioLeadMs)
        audioMinLeadMs          \(t.audioMinLeadMs)
        audioUnderruns          \(t.audioUnderruns)
        earlyStabilityIssues    \(t.earlyStabilityIssues)
        """)

        #expect(t.sampleBuffersDecodedCount > 0,
                "VideoToolbox decoded no frame in \(LiveIngest.observationSeconds)s; the route did not deliver usable elementary stream")

        // A descrambled service must show no scrambled packet at all. Any is a
        // receiver-side descrambling failure, not a client one.
        #expect(t.scrambledPackets == 0,
                "received \(t.scrambledPackets) scrambled packets; the receiver is not descrambling this service")

        // Sustained decoding, not just a first frame: at 25fps twenty seconds
        // is ~500 frames, so this is a floor well under any healthy run.
        #expect(t.sampleBuffersDecodedCount > 100,
                "only \(t.sampleBuffersDecodedCount) frames decoded in \(LiveIngest.observationSeconds)s; decoding is not sustained")
    }
}
