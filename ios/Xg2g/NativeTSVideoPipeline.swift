// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import UIKit

/// Coordinates the end-to-end native DVB TS $\rightarrow$ VideoToolbox $\rightarrow$ Metal Deinterlace pipeline.
public final class NativeTSVideoPipeline: NSObject, ObservableObject, @unchecked Sendable,
    TSPacketParserDelegate,
    PESPacketAssemblerDelegate,
    H264AccessUnitAssemblerDelegate,
    HardwareVideoDecoderDelegate,
    URLSessionDataDelegate {

    public let telemetry = StreamTelemetry()

    private let tsParser = TSPacketParser()
    private let pesAssembler = PESPacketAssembler()
    private let accessUnitAssembler = H264AccessUnitAssembler()
    private let decoder = HardwareVideoDecoder()

    public weak var renderView: MetalVideoView?

    private var urlSession: URLSession?
    private var streamTask: URLSessionDataTask?
    private var decodedFrameCounter: Int = 0
    private var lastDecodedRateCheck: Date = Date()
    private var bytesReceived: Int = 0
    private var lastBitrateCheck: Date = Date()

    public var useNativeVTDeinterlace: Bool {
        get { decoder.useNativeVTDeinterlace }
        set {
            decoder.useNativeVTDeinterlace = newValue
            telemetry.activeDecoderMode = newValue ? "VideoToolbox Native (Path B)" : "Metal Shader (Path A)"
        }
    }

    public override init() {
        super.init()
        tsParser.delegate = self
        pesAssembler.delegate = self
        accessUnitAssembler.delegate = self
        decoder.delegate = self
    }

    public func startStreaming(url: URL) {
        stopStreaming()
        telemetry.reset()

        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        config.timeoutIntervalForRequest = 10
        let session = URLSession(configuration: config, delegate: self, delegateQueue: OperationQueue())
        self.urlSession = session

        var request = URLRequest(url: url)
        request.setValue("xg2g-ios-native-poc/1.0", forHTTPHeaderField: "User-Agent")

        let task = session.dataTask(with: request)
        self.streamTask = task
        task.resume()

        startSystemMonitoring()
    }

    public func stopStreaming() {
        streamTask?.cancel()
        streamTask = nil
        urlSession?.invalidateAndCancel()
        urlSession = nil

        tsParser.reset()
        pesAssembler.reset()
        accessUnitAssembler.reset()
        decoder.reset()
    }

    // MARK: - URLSessionDataDelegate (Streaming Ingest)

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        bytesReceived += data.count
        let now = Date()
        if now.timeIntervalSince(lastBitrateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastBitrateCheck)
            let kbps = (Double(bytesReceived * 8) / 1000.0) / elapsed
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.tsBitrateKbps = kbps
            }
            bytesReceived = 0
            lastBitrateCheck = now
        }

        tsParser.feed(data: data)
    }

    // MARK: - TSPacketParserDelegate

    public func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.videoPID = pid
        }
    }

    public func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        pesAssembler.feed(payload: data, unitStart: unitStart)
    }

    public func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.continuityErrors += 1
        }
    }

    // MARK: - PESPacketAssemblerDelegate

    public func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoUnit unit: PESVideoAccessUnit) {
        accessUnitAssembler.process(unit: unit)
    }

    public func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.pesErrors += 1
        }
    }

    // MARK: - H264AccessUnitAssemblerDelegate

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        decoder.configure(with: formatDescription)
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.telemetry.videoWidth = info.width
            self.telemetry.videoHeight = info.height
            self.telemetry.isInterlaced = info.isInterlaced
            self.telemetry.fieldOrder = info.isInterlaced ? (info.isTopFieldFirst ? "TFF" : "BFF") : "Progressive"
            self.telemetry.vtSessionActive = true
        }
    }

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isIDR: Bool, isTopFieldFirst: Bool) {
        decoder.decode(sampleBuffer: sampleBuffer, isTopFieldFirst: isTopFieldFirst)
    }

    // MARK: - HardwareVideoDecoderDelegate

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        decodedFrameCounter += 1
        let now = Date()
        if now.timeIntervalSince(lastDecodedRateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastDecodedRateCheck)
            let rate = Double(decodedFrameCounter) / elapsed
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.decodedFramesPerSec = rate
            }
            decodedFrameCounter = 0
            lastDecodedRateCheck = now
        }

        renderView?.enqueueFrame(frame)
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.hwDecodeActive = isHWActive
        }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.decodeErrors += 1
        }
    }

    // MARK: - System Telemetry Monitoring

    private func startSystemMonitoring() {
        Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            guard let self = self else { return }
            let state = ProcessInfo.processInfo.thermalState
            let thermalString: String
            switch state {
            case .nominal: thermalString = "Nominal 🟢"
            case .fair: thermalString = "Fair 🟡"
            case .serious: thermalString = "Serious 🟠"
            case .critical: thermalString = "Critical 🔴"
            @unknown default: thermalString = "Unknown"
            }

            var info = mach_task_basic_info()
            var count = mach_msg_type_number_t(MemoryLayout<mach_task_basic_info>.size) / 4
            let kerr: kern_return_t = withUnsafeMutablePointer(to: &info) {
                $0.withMemoryRebound(to: integer_t.self, capacity: 1) {
                    task_info(mach_task_self_, task_flavor_t(MACH_TASK_BASIC_INFO), $0, &count)
                }
            }
            let memMB = (kerr == KERN_SUCCESS) ? Double(info.resident_size) / (1024.0 * 1024.0) : 0.0

            DispatchQueue.main.async {
                self.telemetry.thermalState = thermalString
                self.telemetry.memoryUsageMB = memMB
            }
        }
    }
}
