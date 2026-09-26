// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Where playback telemetry goes. A seam so the reporter can be tested without a
/// server.
protocol PlaybackTelemetrySink: Sendable {
    func send(_ batch: Xg2gContract.PlaybackTelemetryBatch) async throws
}

/// Sends playback telemetry to the backend.
///
/// The server delivers the stream and can see only that it delivered it. Whether the
/// picture moved and the sound kept playing is known here alone, and until this
/// existed it stayed here: a frozen picture left no trace on the server, and the
/// Xcode console that did see it was gone with the next launch.
struct PlaybackTelemetryClient: PlaybackTelemetrySink {
    private let api: any APIClient
    private let clientID: String

    init(api: any APIClient, clientID: String) {
        self.api = api
        self.clientID = clientID
    }

    // Relative to the API base, which already carries /api/v3/.
    private static let path = "telemetry/playback"

    func send(_ batch: Xg2gContract.PlaybackTelemetryBatch) async throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        let body = try encoder.encode(batch)
        let _: EmptyResponse = try await api.send(APIRequest(
            method: .post,
            path: Self.path,
            body: body,
            contentType: "application/json",
            headers: ["X-Xg2g-Client-Id": clientID]
        ))
    }
}

/// What one sampling window says about the stream on screen.
///
/// Pure: two snapshots of the same session in, one verdict out. Every judgement is
/// made on cumulative counters, never on a rate. The rates are recomputed only while
/// the thing they measure is happening, so a stream that stops keeps reporting its
/// last healthy rate - a frozen picture reads 50 fields a second - while a total that
/// stops growing cannot hide it.
struct PlaybackTelemetryWindow: Equatable {
    /// Underruns in one window that count as the sound breaking up rather than a
    /// single hiccup. The audio renderer counts every time its cushion falls under
    /// 50 ms, and a clean window has none.
    static let audioUnderrunThreshold = 10

    let reasons: [String]
    let metrics: [String: Double]
    let detail: String?

    var isDegraded: Bool { !reasons.isEmpty }

    static func evaluate(previous: TelemetryValues?, current: TelemetryValues, windowSeconds: Double) -> PlaybackTelemetryWindow {
        let prev = previous ?? TelemetryValues()

        let input = delta(current.bytesReceivedTotal, prev.bytesReceivedTotal)
        let decoded = delta(current.sampleBuffersDecodedCount, prev.sampleBuffersDecodedCount)
        let presented = delta(current.presentedFieldsTotal, prev.presentedFieldsTotal)
        let underruns = delta(current.audioUnderruns, prev.audioUnderruns)
        let stalls = delta(current.networkStalls, prev.networkStalls)
        let decodeErrors = delta(current.decodeErrors, prev.decodeErrors)
        let continuity = delta(current.continuityErrors, prev.continuityErrors)
        let dropped = delta(current.droppedFrames, prev.droppedFrames)
        let newWarnings = current.pipelineWarnings.count > prev.pipelineWarnings.count
            ? Array(current.pipelineWarnings.suffix(current.pipelineWarnings.count - prev.pipelineWarnings.count))
            : []

        // Only with a previous sample is "nothing moved" a finding. The first
        // window of a session starts from zero and says nothing about stalling.
        var reasons: [String] = []
        if previous != nil {
            if input == 0 {
                reasons.append("no_input")
            } else if decoded == 0 {
                reasons.append("video_decode_stalled")
            } else if presented == 0 {
                reasons.append("presentation_stalled")
            }
        }
        if underruns >= audioUnderrunThreshold { reasons.append("audio_underruns") }
        if stalls > 0 { reasons.append("network_stalls") }
        if decodeErrors > 0 { reasons.append("decode_errors") }
        if !newWarnings.isEmpty { reasons.append("pipeline_warning") }

        var metrics: [String: Double] = [
            "windowSeconds": windowSeconds,
            "bytesReceivedDelta": Double(input),
            "decodedFramesDelta": Double(decoded),
            "presentedFieldsDelta": Double(presented),
            "audioUnderrunsDelta": Double(underruns),
            "networkStallsDelta": Double(stalls),
            "decodeErrorsDelta": Double(decodeErrors),
            "continuityErrorsDelta": Double(continuity),
            "droppedFramesDelta": Double(dropped),
            "audioUnderruns": Double(current.audioUnderruns),
            "audioLeadMs": current.audioLeadMs,
            "audioMinLeadMs": current.audioMinLeadMs,
            "networkStalls": Double(current.networkStalls),
            "longestNetworkStallMs": current.longestNetworkStallMs,
            "decodeErrors": Double(current.decodeErrors),
            "decoderRecoveries": Double(current.decoderRecoveries),
            "continuityErrors": Double(current.continuityErrors),
            "pesErrors": Double(current.pesErrors),
            "scrambledPackets": Double(current.scrambledPackets),
            "droppedFrames": Double(current.droppedFrames),
            "lateFrames": Double(current.lateFrames),
            "ptsDiscontinuities": Double(current.ptsDiscontinuities),
            "tsBitrateKbps": current.tsBitrateKbps,
            "decodedFramesPerSec": current.decodedFramesPerSec,
            "fieldsSubmittedPerSec": current.fieldsSubmittedPerSec,
            "ingestBacklogBytes": Double(current.ingestBacklogBytes),
            "memoryUsageMb": current.memoryUsageMB,
            "processCpuPercent": current.processCpuUsagePercent,
            "thermalLevel": thermalLevel(current.thermalState),
        ]
        if current.ttfpVisibleMs > 0 { metrics["ttfpVisibleMs"] = current.ttfpVisibleMs }
        if current.ttfpTotalMs > 0 { metrics["ttfpTotalMs"] = current.ttfpTotalMs }

        // Only finite figures cross the wire; the server refuses anything else,
        // and one NaN would cost the whole window.
        metrics = metrics.filter { $0.value.isFinite }

        let detail = newWarnings.last.map { String($0.prefix(512)) }
        return PlaybackTelemetryWindow(reasons: reasons, metrics: metrics, detail: detail)
    }

    /// Growth of a cumulative counter. A counter that went down was reset under
    /// us, and everything it now holds happened since.
    private static func delta(_ current: Int, _ previous: Int) -> Int {
        current >= previous ? current - previous : current
    }

    private static func thermalLevel(_ state: String) -> Double {
        switch state.lowercased() {
        case "nominal": return 0
        case "fair": return 1
        case "serious": return 2
        case "critical": return 3
        default: return -1
        }
    }
}

/// Reports the health of the stream on screen to the backend.
///
/// One session is watched at a time: the one the viewer is looking at. Its start and
/// end are reported, and in between it is sampled every `sampleInterval`. A window
/// with a fault is sent as `degraded` with the reasons named, a clean one as a
/// `heartbeat`, so the server's log holds the healthy stretch around a fault as well
/// as the fault itself.
///
/// Best-effort by design. Uploads are fire-and-forget, a failed one is dropped, and
/// nothing here can hold up or alter playback.
@MainActor
final class PlaybackTelemetryReporter {
    static let defaultSampleInterval: Duration = .seconds(15)

    private struct Watched {
        weak var session: NativeTSVideoPipeline?
        let zapID: String
        let serviceRef: String
        var previous: TelemetryValues
        var previousAt: Date
    }

    private let sinkProvider: @MainActor () -> (any PlaybackTelemetrySink)?
    private let client: Xg2gContract.PlaybackTelemetryClient
    private let sampleInterval: Duration
    private let now: () -> Date
    private var watched: Watched?
    private var sampler: Task<Void, Never>?

    /// The upload in flight, which the next one waits for. One channel's end and the
    /// next one's start are emitted back to back, and read in the wrong order they
    /// would describe two channels on screen at once.
    private var lastUpload: Task<Void, Never>?

    init(
        sinkProvider: @escaping @MainActor () -> (any PlaybackTelemetrySink)?,
        client: Xg2gContract.PlaybackTelemetryClient = PlaybackTelemetryReporter.currentClient(),
        sampleInterval: Duration = PlaybackTelemetryReporter.defaultSampleInterval,
        now: @escaping () -> Date = Date.init
    ) {
        self.sinkProvider = sinkProvider
        self.client = client
        self.sampleInterval = sampleInterval
        self.now = now
    }

    /// Starts watching the session now on screen and reports its start.
    ///
    /// A session still watched is ended first: the surface has moved on, and what it
    /// did up to this moment is its whole record.
    func watch(_ session: NativeTSVideoPipeline, zapID: String, serviceRef: String, startMetrics: [String: Double] = [:]) {
        if let current = watched?.session, current !== session {
            finish(current, reason: "replaced")
        }

        let snapshot = session.telemetry.snapshot()
        watched = Watched(session: session, zapID: zapID, serviceRef: serviceRef, previous: snapshot, previousAt: now())
        emit(event(
            kind: .sessionStart,
            zapID: zapID,
            serviceRef: serviceRef,
            metrics: startMetrics.filter { $0.value.isFinite }
        ))

        sampler?.cancel()
        let interval = sampleInterval
        sampler = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard !Task.isCancelled, let self else { return }
                self.sample()
            }
        }
    }

    /// Reports the end of a session with what it did in its last window. A session
    /// that is not the watched one has already been reported and is ignored.
    func finish(_ session: NativeTSVideoPipeline, reason: String) {
        guard let current = watched, current.session === session else { return }
        stopWatching()
        report(current, final: session.telemetry.snapshot(), kind: .sessionEnd, detail: reason)
    }

    /// Takes one sample of the watched session. Called by the sampler; internal so a
    /// test can drive it without waiting.
    func sample() {
        guard var current = watched else { return }
        guard let session = current.session, session.isStreaming else {
            // Closed without being finished, or already gone. Its counters stopped
            // because the stream did, which is an ending, not a freeze.
            stopWatching()
            report(current, final: current.session?.telemetry.snapshot() ?? current.previous,
                   kind: .sessionEnd, detail: "stream closed")
            return
        }

        let snapshot = session.telemetry.snapshot()
        let takenAt = now()
        let window = PlaybackTelemetryWindow.evaluate(
            previous: current.previous,
            current: snapshot,
            windowSeconds: takenAt.timeIntervalSince(current.previousAt)
        )
        current.previous = snapshot
        current.previousAt = takenAt
        watched = current

        emit(event(
            kind: window.isDegraded ? .degraded : .heartbeat,
            zapID: current.zapID,
            serviceRef: current.serviceRef,
            metrics: window.metrics,
            reasons: window.reasons,
            detail: window.detail,
            at: takenAt
        ))
    }

    /// Stops sampling without reporting anything, for a player going away with no
    /// session to account for.
    func stopWatching() {
        sampler?.cancel()
        sampler = nil
        watched = nil
    }

    private func report(_ current: Watched, final snapshot: TelemetryValues, kind: Xg2gContract.PlaybackTelemetryEventKind, detail: String) {
        let takenAt = now()
        let window = PlaybackTelemetryWindow.evaluate(
            previous: current.previous,
            current: snapshot,
            windowSeconds: takenAt.timeIntervalSince(current.previousAt)
        )
        // The closing window is not judged for stalling: a stream that is being
        // taken down stops moving because it was told to.
        let reasons = window.reasons.filter { !Self.stallReasons.contains($0) }
        emit(event(
            kind: kind,
            zapID: current.zapID,
            serviceRef: current.serviceRef,
            metrics: window.metrics,
            reasons: reasons,
            detail: detail,
            at: takenAt
        ))
    }

    private static let stallReasons: Set<String> = ["no_input", "video_decode_stalled", "presentation_stalled"]

    private func event(
        kind: Xg2gContract.PlaybackTelemetryEventKind,
        zapID: String,
        serviceRef: String,
        metrics: [String: Double],
        reasons: [String] = [],
        detail: String? = nil,
        at: Date? = nil
    ) -> Xg2gContract.PlaybackTelemetryEvent {
        Xg2gContract.PlaybackTelemetryEvent(
            kind: kind,
            occurredAt: at ?? now(),
            detail: detail,
            metrics: metrics.isEmpty ? nil : metrics,
            reasons: reasons.isEmpty ? nil : Array(reasons.prefix(8)),
            serviceRef: String(serviceRef.prefix(128)),
            zapId: String(zapID.prefix(64))
        )
    }

    private func emit(_ event: Xg2gContract.PlaybackTelemetryEvent) {
        guard let sink = sinkProvider() else { return }
        let batch = Xg2gContract.PlaybackTelemetryBatch(client: client, events: [event])
        let previous = lastUpload
        lastUpload = Task.detached(priority: .utility) {
            await previous?.value
            try? await sink.send(batch)
        }
    }

    /// The running build, named by version and hardware model. Never the device's
    /// user-assigned name.
    nonisolated static func currentClient() -> Xg2gContract.PlaybackTelemetryClient {
        let info = Bundle.main.infoDictionary ?? [:]
        #if os(tvOS)
        let platform = Xg2gContract.PlaybackTelemetryClientPlatform.tvos
        #else
        let platform = Xg2gContract.PlaybackTelemetryClientPlatform.ios
        #endif
        return Xg2gContract.PlaybackTelemetryClient(
            platform: platform,
            appVersion: (info["CFBundleShortVersionString"] as? String).map { String($0.prefix(64)) },
            build: (info["CFBundleVersion"] as? String).map { String($0.prefix(64)) },
            device: hardwareModel().map { String($0.prefix(64)) }
        )
    }

    private nonisolated static func hardwareModel() -> String? {
        var system = utsname()
        uname(&system)
        let model = withUnsafeBytes(of: &system.machine) { raw -> String in
            let bytes = raw.prefix { $0 != 0 }
            return String(decoding: bytes, as: UTF8.self)
        }
        return model.isEmpty ? nil : model
    }
}
