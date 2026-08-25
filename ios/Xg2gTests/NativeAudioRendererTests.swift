// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct NativeAudioRendererTests {

    @Test func audioRendererInitializesWithSynchronizerTimebase() throws {
        let renderer = NativeTSAudioRenderer()
        #expect(renderer.status != .failed)
    }

    @Test func audioRendererEnqueuesAC3SampleBuffersWithoutFailing() throws {
        let renderer = NativeTSAudioRenderer()
        renderer.setAudible(true)

        let frameParser = AC3FrameParser()
        let sbAssembler = AudioSampleBufferAssembler()
        let sink = MockAudioSinkCollector(renderer: renderer)

        frameParser.delegate = sbAssembler
        sbAssembler.delegate = sink

        // Build 2 valid AC-3 frames
        let frame1 = makeAC3TestFrame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2)
        let frame2 = makeAC3TestFrame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2)

        let initialPTS = CMTime(value: 90000, timescale: 90000)
        frameParser.feed(data: frame1, pts: initialPTS, pts90k: 90000)
        frameParser.feed(data: frame2)

        #expect(sink.emittedCount == 2)
        #expect(renderer.status != .failed)
    }

    @Test func audioRendererFlushesCleanlyOnDiscontinuity() throws {
        let renderer = NativeTSAudioRenderer()
        renderer.setAudible(true)

        let frameParser = AC3FrameParser()
        let sbAssembler = AudioSampleBufferAssembler()
        let sink = MockAudioSinkCollector(renderer: renderer)

        frameParser.delegate = sbAssembler
        sbAssembler.delegate = sink

        let frame = makeAC3TestFrame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2)
        frameParser.feed(data: frame, pts: CMTime(value: 90000, timescale: 90000), pts90k: 90000)

        // Perform flush
        renderer.flush()

        #expect(renderer.status != .failed)
    }

    @Test func pipelineHandlesAudioInterruptionBeganAndEnded() async throws {
        let pipeline = NativeTSVideoPipeline()
        let dummyURL = URL(string: "http://127.0.0.1:8001/1:0:19:11:6:85:C00000:0:0:0:")!
        pipeline.startStreaming(url: dummyURL)

        // Simulate interruption began (e.g. phone call started)
        NotificationCenter.default.post(
            name: AVAudioSession.interruptionNotification,
            object: AVAudioSession.sharedInstance(),
            userInfo: [AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.began.rawValue]
        )

        try await Task.sleep(nanoseconds: 50_000_000)

        // Simulate interruption ended with shouldResume
        NotificationCenter.default.post(
            name: AVAudioSession.interruptionNotification,
            object: AVAudioSession.sharedInstance(),
            userInfo: [
                AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.ended.rawValue,
                AVAudioSessionInterruptionOptionKey: AVAudioSession.InterruptionOptions.shouldResume.rawValue
            ]
        )

        try await Task.sleep(nanoseconds: 50_000_000)

        #expect(pipeline.audioRenderer.status != .failed)
        pipeline.stopStreaming()
    }

    @Test func pipelineRecoversFromAudioRendererFatalError() async throws {
        let pipeline = NativeTSVideoPipeline()
        let dummyURL = URL(string: "http://127.0.0.1:8001/1:0:19:11:6:85:C00000:0:0:0:")!
        pipeline.startStreaming(url: dummyURL)

        let initialSync = pipeline.audioRenderer.synchronizer

        // Trigger an audio renderer failure
        let simulatedError = NSError(domain: "AVFoundationErrorDomain", code: -11800, userInfo: [NSLocalizedDescriptionKey: "Simulated audio failure"])
        let realRenderer = pipeline.audioRenderer as! NativeTSAudioRenderer
        pipeline.audioRendererDidEncounterError(realRenderer, error: simulatedError)

        try await Task.sleep(nanoseconds: 50_000_000)

        // A new synchronizer was constructed upon reset
        #expect(pipeline.audioRenderer.synchronizer !== initialSync)
        #expect(pipeline.audioRenderer.status != .failed)
        pipeline.stopStreaming()
    }

    @Test func pipelineHandlesAutomaticRendererFlushNotification() async throws {
        let pipeline = NativeTSVideoPipeline()
        let dummyURL = URL(string: "http://127.0.0.1:8001/1:0:19:11:6:85:C00000:0:0:0:")!
        pipeline.startStreaming(url: dummyURL)

        NotificationCenter.default.post(
            name: .AVSampleBufferAudioRendererWasFlushedAutomatically,
            object: (pipeline.audioRenderer as! NativeTSAudioRenderer).audioRenderer,
            userInfo: [AVSampleBufferAudioRendererFlushTimeKey: CMTime(value: 1000, timescale: 1000)]
        )

        try await Task.sleep(nanoseconds: 50_000_000)

        #expect(pipeline.audioRenderer.status != .failed)
        pipeline.stopStreaming()
    }

    @Test func testSkySportF1RealStreamAudioPlayback() async throws {
        guard FileManager.default.fileExists(atPath: "/tmp/sky_f1_10s.ts") else { return }
        let tsData = try Data(contentsOf: URL(fileURLWithPath: "/tmp/sky_f1_10s.ts"))
        let pipeline = NativeTSVideoPipeline()
        pipeline.audioRenderer.setAudible(true)
        
        let chunkSize = 32768
        var offset = 0
        while offset < tsData.count {
            let end = min(offset + chunkSize, tsData.count)
            pipeline.feedData(tsData.subdata(in: offset..<end))
            offset = end
            try await Task.sleep(nanoseconds: 10_000_000) // 10ms
        }

        try await Task.sleep(nanoseconds: 500_000_000) // wait 500ms for queue drain

        print("[TestSkyF1] Ingest completed.")
        print("[TestSkyF1] AudioRenderer status: \(pipeline.audioRenderer.status)")
        let tb = pipeline.audioRenderer.synchronizer.timebase
        print("[TestSkyF1] Synchronizer rate: \(CMTimebaseGetRate(tb)), time: \(CMTimebaseGetTime(tb).seconds)s")
    }
}

private final class MockAudioSinkCollector: AudioSampleBufferAssemblerDelegate, @unchecked Sendable {
    let renderer: NativeTSAudioRenderer
    var emittedCount = 0

    init(renderer: NativeTSAudioRenderer) {
        self.renderer = renderer
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int) {}

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime) {
        emittedCount += 1
        renderer.enqueue(sampleBuffer: sampleBuffer)
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String) {}
}

private func makeAC3TestFrame(fscod: Int, frmsizecod: Int, bsid: Int, acmod: Int) -> Data {
    let sizeTable = [
        [
            128, 128, 160, 160, 192, 192, 224, 224, 256, 256,
            320, 320, 384, 384, 448, 448, 512, 512, 640, 640,
            768, 768, 896, 896, 1024, 1024, 1280, 1280, 1536, 1536,
            1792, 1792, 2048, 2048, 2304, 2304, 2560, 2560
        ]
    ]

    let frameSize = sizeTable[0][frmsizecod]
    var frame = Data(repeating: 0x00, count: frameSize)
    frame[0] = 0x0B
    frame[1] = 0x77
    frame[2] = 0x12
    frame[3] = 0x34
    frame[4] = UInt8(((fscod & 0x03) << 6) | (frmsizecod & 0x3F))
    frame[5] = UInt8(((bsid & 0x1F) << 3) | 0x00)
    frame[6] = UInt8((acmod & 0x07) << 5)
    return frame
}
