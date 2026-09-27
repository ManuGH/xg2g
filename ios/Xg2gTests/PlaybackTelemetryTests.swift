// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// A window's verdict is taken from counters that only ever grow, never from rates.
/// The rates are recomputed only while frames flow, so a frozen picture keeps
/// reporting the last healthy one; these tests hold the verdict to the totals.
@Suite
struct PlaybackTelemetryWindowTests {

    /// A stream in good health: bytes in, frames decoded, fields shown, no faults.
    private func healthy(_ step: Int) -> TelemetryValues {
        var v = TelemetryValues()
        v.bytesReceivedTotal = 1_000_000 * step
        v.sampleBuffersDecodedCount = 375 * step
        v.presentedFieldsTotal = 750 * step
        v.fieldsSubmittedPerSec = 50
        v.decodedFramesPerSec = 25
        v.thermalState = "Nominal"
        return v
    }

    @Test func healthyWindowIsAHeartbeat() {
        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: healthy(2), windowSeconds: 15)
        #expect(window.reasons.isEmpty)
        #expect(!window.isDegraded)
        #expect(window.metrics["presentedFieldsDelta"] == 750)
        #expect(window.metrics["windowSeconds"] == 15)
    }

    /// The case that went unseen: fields stop reaching the display while the last
    /// published rate still reads 50 a second.
    @Test func frozenPictureIsCaughtDespiteAStaleRate() {
        var current = healthy(2)
        current.presentedFieldsTotal = healthy(1).presentedFieldsTotal
        current.fieldsSubmittedPerSec = 50

        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15)
        #expect(window.reasons == ["presentation_stalled"])
    }

    @Test func decoderThatStopsIsNamed() {
        var current = healthy(2)
        current.sampleBuffersDecodedCount = healthy(1).sampleBuffersDecodedCount
        current.presentedFieldsTotal = healthy(1).presentedFieldsTotal

        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15)
        #expect(window.reasons == ["video_decode_stalled"])
    }

    @Test func streamThatStopsArrivingIsNoInput() {
        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: healthy(1), windowSeconds: 15)
        #expect(window.reasons == ["no_input"])
    }

    /// The first window of a session starts from zero and has nothing to compare
    /// against, so it must never be read as a stall.
    @Test func firstWindowIsNeverAStall() {
        let window = PlaybackTelemetryWindow.evaluate(previous: nil, current: TelemetryValues(), windowSeconds: 15)
        #expect(window.reasons.isEmpty)
    }

    /// The sound breaking up while the picture plays on, as in the session where the
    /// underruns climbed to 1002 and the app still reported no stall.
    @Test func audioStarvationIsNamedAboveTheThreshold() {
        var current = healthy(2)
        current.audioUnderruns = PlaybackTelemetryWindow.audioUnderrunThreshold
        #expect(PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15).reasons
            == ["audio_underruns"])

        current.audioUnderruns = PlaybackTelemetryWindow.audioUnderrunThreshold - 1
        #expect(PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15).reasons
            .isEmpty)
    }

    @Test func networkStallsDecodeErrorsAndWarningsAreEachNamed() {
        var current = healthy(2)
        current.networkStalls = 1
        current.decodeErrors = 3
        current.pipelineWarnings = ["picture stopped advancing"]

        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15)
        #expect(window.reasons == ["network_stalls", "decode_errors", "pipeline_warning"])
        #expect(window.detail == "picture stopped advancing")
    }

    /// A counter that went down was reset under the window; what it holds now is
    /// the growth, not a negative number.
    @Test func resetCounterCountsFromZero() {
        var current = healthy(2)
        current.audioUnderruns = 12
        var previous = healthy(1)
        previous.audioUnderruns = 400

        let window = PlaybackTelemetryWindow.evaluate(previous: previous, current: current, windowSeconds: 15)
        #expect(window.metrics["audioUnderrunsDelta"] == 12)
    }

    /// One NaN would cost the whole window at the server, which refuses it.
    @Test func nonFiniteFiguresAreLeftOut() {
        var current = healthy(2)
        current.audioLeadMs = .nan
        current.tsBitrateKbps = .infinity

        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15)
        #expect(window.metrics["audioLeadMs"] == nil)
        #expect(window.metrics["tsBitrateKbps"] == nil)
        #expect(window.metrics.values.allSatisfy { $0.isFinite })
    }

    /// Keys become log field names on the server, which accepts lowerCamelCase only.
    @Test func metricKeysAreLowerCamelCase() {
        var current = healthy(2)
        current.ttfpVisibleMs = 900
        let window = PlaybackTelemetryWindow.evaluate(previous: healthy(1), current: current, windowSeconds: 15)
        let pattern = /^[a-z][A-Za-z0-9]{0,47}$/
        for key in window.metrics.keys {
            #expect(key.wholeMatch(of: pattern) != nil, "\(key)")
        }
        #expect(window.metrics.count <= 48)
    }
}

/// Collects what the reporter sends.
private actor RecordingSink: PlaybackTelemetrySink {
    private(set) var events: [Xg2gContract.PlaybackTelemetryEvent] = []

    func send(_ batch: Xg2gContract.PlaybackTelemetryBatch) async throws {
        events.append(contentsOf: batch.events)
    }

    func waitForEvents(_ count: Int) async -> [Xg2gContract.PlaybackTelemetryEvent] {
        for _ in 0..<200 where events.count < count {
            try? await Task.sleep(for: .milliseconds(10))
        }
        return events
    }
}

@MainActor
@Suite
struct PlaybackTelemetryReporterTests {

    private func makeReporter(_ sink: RecordingSink) -> PlaybackTelemetryReporter {
        PlaybackTelemetryReporter(
            sinkProvider: { sink },
            client: Xg2gContract.PlaybackTelemetryClient(platform: .ios, appVersion: "test"),
            sampleInterval: .seconds(3600)
        )
    }

    @Test func watchReportsTheStartAndFinishTheEnd() async {
        let sink = RecordingSink()
        let reporter = makeReporter(sink)
        let session = NativeTSVideoPipeline()

        reporter.watch(session, zapID: "ios-t-1", serviceRef: "1:0:1:test", startMetrics: ["transportMs": 470])
        reporter.finish(session, reason: "stop.stats")

        let events = await sink.waitForEvents(2)
        #expect(events.map(\.kind) == [.sessionStart, .sessionEnd])
        #expect(events.allSatisfy { $0.zapId == "ios-t-1" && $0.serviceRef == "1:0:1:test" })
        #expect(events.first?.metrics?["transportMs"] == 470)
        #expect(events.last?.detail == "stop.stats")
    }

    /// Winding a stream down stops every counter. That is the end of the stream,
    /// and reporting it as a stall would put a freeze on every channel change.
    @Test func theEndIsNeverReportedAsAStall() async {
        let sink = RecordingSink()
        let reporter = makeReporter(sink)
        let session = NativeTSVideoPipeline()

        reporter.watch(session, zapID: "ios-t-2", serviceRef: "1:0:1:test")
        reporter.finish(session, reason: "retire.stats")

        let events = await sink.waitForEvents(2)
        #expect(events.last?.kind == .sessionEnd)
        #expect(events.last?.reasons == nil)
    }

    /// A session that closed without being finished is ended by the next sample,
    /// and after that the reporter reports nothing more about it.
    @Test func aClosedSessionIsEndedOnceAndThenLeftAlone() async {
        let sink = RecordingSink()
        let reporter = makeReporter(sink)
        let session = NativeTSVideoPipeline()

        reporter.watch(session, zapID: "ios-t-3", serviceRef: "1:0:1:test")
        #expect(!session.isStreaming)
        reporter.sample()
        reporter.sample()
        reporter.finish(session, reason: "stop.stats")

        let events = await sink.waitForEvents(2)
        try? await Task.sleep(for: .milliseconds(50))
        let settled = await sink.events
        #expect(settled.map(\.kind) == [.sessionStart, .sessionEnd])
        #expect(settled.last?.detail == "stream closed")
        _ = events
    }

    /// Only the session on screen is reported. A retiring one that was already
    /// replaced is not ended a second time.
    @Test func replacingASessionEndsItOnce() async {
        let sink = RecordingSink()
        let reporter = makeReporter(sink)
        let first = NativeTSVideoPipeline()
        let second = NativeTSVideoPipeline()

        reporter.watch(first, zapID: "ios-t-4", serviceRef: "1:0:1:a")
        reporter.watch(second, zapID: "ios-t-5", serviceRef: "1:0:1:b")
        reporter.finish(first, reason: "retire.stats")

        _ = await sink.waitForEvents(3)
        try? await Task.sleep(for: .milliseconds(50))
        let events = await sink.events
        #expect(events.map(\.kind) == [.sessionStart, .sessionEnd, .sessionStart])
        #expect(events[1].zapId == "ios-t-4")
        #expect(events[1].detail == "replaced")
        #expect(events[2].zapId == "ios-t-5")
    }
}
