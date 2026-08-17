// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "telemetry")

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
    private var systemMonitoringTimer: Timer?
    private var lastDecodedPTS: CMTime = .invalid

    // TTFP Stage Timestamps
    private var requestStartTime: CFTimeInterval = 0
    private var firstDataTime: CFTimeInterval = 0
    private var psiParsedTime: CFTimeInterval = 0
    private var paramsReadyTime: CFTimeInterval = 0
    private var firstIdrTime: CFTimeInterval = 0
    private var firstDecodedTime: CFTimeInterval = 0
    private var firstPictureDeliveredTime: CFTimeInterval = 0

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

        TelemetryServer.shared.start()
        TelemetryServer.shared.setTelemetryProvider { [weak self] in
            return self?.telemetry.toDictionary() ?? [:]
        }
    }

    deinit {
        stopStreaming()
    }

    public func startStreaming(url: URL) {
        stopStreaming()
        telemetry.reset()
        lastDecodedPTS = .invalid

        requestStartTime = CACurrentMediaTime()
        firstDataTime = 0
        psiParsedTime = 0
        paramsReadyTime = 0
        firstIdrTime = 0
        firstDecodedTime = 0
        firstPictureDeliveredTime = 0

        let targetURL = normalizeStreamURL(url)

        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.renderView?.resetForChannelZap()
            self.renderView?.onFirstFrameRendered = { [weak self] in
                self?.handleFirstFrameRendered()
            }
            self.renderView?.onFirstFrameActuallyPresentedOnScreen = { [weak self] screenTime in
                self?.handleFirstFrameActuallyPresented(screenTimestamp: screenTime)
            }
        }

        let config = URLSessionConfiguration.default
        config.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        config.timeoutIntervalForRequest = 10
        let session = URLSession(configuration: config, delegate: self, delegateQueue: OperationQueue())
        self.urlSession = session

        var request = URLRequest(url: targetURL)
        request.setValue("xg2g-ios-native-poc/1.0", forHTTPHeaderField: "User-Agent")

        let task = session.dataTask(with: request)
        self.streamTask = task
        task.resume()

        startSystemMonitoring()
    }

    private func normalizeStreamURL(_ url: URL) -> URL {
        var urlString = url.absoluteString
        if urlString.contains(":8001/1:0:") && !urlString.hasSuffix(":") {
            urlString += ":"
            if let normalized = URL(string: urlString) {
                return normalized
            }
        }
        return url
    }

    public func stopStreaming() {
        streamTask?.cancel()
        streamTask = nil
        urlSession?.invalidateAndCancel()
        urlSession = nil

        stopSystemMonitoring()

        tsParser.reset()
        pesAssembler.reset()
        accessUnitAssembler.reset()
        decoder.reset()
    }

    // MARK: - URLSessionDataDelegate (Streaming Ingest)

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive response: URLResponse, completionHandler: @escaping (URLSession.ResponseDisposition) -> Void) {
        if let httpResponse = response as? HTTPURLResponse {
            let serverName = httpResponse.value(forHTTPHeaderField: "Server") ?? "Enigma2 Streamserver"
            let contentType = httpResponse.mimeType ?? "video/mp2t"
            let httpLog = "[1080i50-HTTP] Connected: Status \(httpResponse.statusCode) | Type: \(contentType) | Server: \(serverName)"
            print(httpLog)
            logger.notice("\(httpLog, privacy: .public)")
            TelemetryServer.shared.log(httpLog)

            if httpResponse.statusCode < 200 || httpResponse.statusCode >= 300 {
                DispatchQueue.main.async { [weak self] in
                    self?.telemetry.ttfpRating = "❌ HTTP \(httpResponse.statusCode) (\(HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)))"
                }
                completionHandler(.cancel)
                return
            }
        }
        completionHandler(.allow)
    }

    public func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        if let error = error as NSError?, error.code != NSURLErrorCancelled {
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpRating = "❌ Connection Error: \(error.localizedDescription)"
            }
        }
    }

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        if firstDataTime == 0 && requestStartTime > 0 {
            firstDataTime = CACurrentMediaTime()
            let netMs = (firstDataTime - requestStartTime) * 1000.0
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpNetworkMs = netMs
            }
        }

        bytesReceived += data.count
        let now = Date()
        if now.timeIntervalSince(lastBitrateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastBitrateCheck)
            let kbps = (Double(bytesReceived * 8) / 1000.0) / elapsed
            DispatchQueue.main.async { [weak self] in
                guard let self = self else { return }
                self.telemetry.tsBitrateKbps = kbps
                let qualityLog = "[1080i50-QUALITY] Bitrate: \(String(format: "%.1f", kbps)) kbps | VideoPID: \(self.telemetry.videoPID) | ContinuityErr: \(self.telemetry.continuityErrors) | PESErr: \(self.telemetry.pesErrors) | DecErrors: \(self.telemetry.decodeErrors)"
                print(qualityLog)
                logger.notice("\(qualityLog, privacy: .public)")
                TelemetryServer.shared.log(qualityLog)
            }
            bytesReceived = 0
            lastBitrateCheck = now
        }

        tsParser.feed(data: data)
    }

    // MARK: - TSPacketParserDelegate

    public func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        if psiParsedTime == 0 {
            psiParsedTime = CACurrentMediaTime()
            let base = firstDataTime > 0 ? firstDataTime : requestStartTime
            let psiMs = (psiParsedTime - base) * 1000.0
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpPsiMs = psiMs
            }
        }

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

    public func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        accessUnitAssembler.feed(payload: payload)
    }

    public func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.pesErrors += 1
        }
    }

    // MARK: - H264AccessUnitAssemblerDelegate

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        if paramsReadyTime == 0 {
            paramsReadyTime = CACurrentMediaTime()
            let base = psiParsedTime > 0 ? psiParsedTime : (firstDataTime > 0 ? firstDataTime : requestStartTime)
            let paramMs = (paramsReadyTime - base) * 1000.0
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpParamSetsMs = paramMs
            }
        }

        decoder.configure(with: formatDescription)
        let logMsg = "[1080i50-CODEC] Format: \(info.width)x\(info.height) | Interlaced: \(info.isInterlaced) | TFF: \(info.isTopFieldFirst)"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.telemetry.videoWidth = info.width
            self.telemetry.videoHeight = info.height
            self.telemetry.isInterlaced = info.isInterlaced
            self.telemetry.fieldOrder = info.isInterlaced ? (info.isTopFieldFirst ? "TFF" : "BFF") : "Progressive"
            self.telemetry.vtSessionActive = true

            if info.isInterlaced {
                self.telemetry.isDirect1080iVerified = true
                self.telemetry.validationWarning = nil
            } else {
                self.telemetry.isDirect1080iVerified = false
                self.telemetry.validationWarning = "⚠️ WARNUNG: Stream ist PROGRESSIV (\(info.width)x\(info.height)p) – Server-Transcode aktiv!"
            }
        }
    }

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isIDR: Bool, isTopFieldFirst: Bool) {
        if firstIdrTime == 0 {
            firstIdrTime = CACurrentMediaTime()
            let base = paramsReadyTime > 0 ? paramsReadyTime : (psiParsedTime > 0 ? psiParsedTime : requestStartTime)
            let idrMs = (firstIdrTime - base) * 1000.0
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpIdrMs = idrMs
            }
        }

        decoder.decode(sampleBuffer: sampleBuffer, isTopFieldFirst: isTopFieldFirst)
    }

    // MARK: - HardwareVideoDecoderDelegate

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        if firstDecodedTime == 0 {
            firstDecodedTime = CACurrentMediaTime()
            let base = firstIdrTime > 0 ? firstIdrTime : (paramsReadyTime > 0 ? paramsReadyTime : requestStartTime)
            let decMs = (firstDecodedTime - base) * 1000.0
            DispatchQueue.main.async { [weak self] in
                self?.telemetry.ttfpDecodeMs = decMs
            }
        }

        decodedFrameCounter += 1
        let now = Date()
        if now.timeIntervalSince(lastDecodedRateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastDecodedRateCheck)
            let rate = Double(decodedFrameCounter) / elapsed
            DispatchQueue.main.async { [weak self] in
                guard let self = self else { return }
                self.telemetry.decodedFramesPerSec = rate
                let decLog = "[1080i50-DECODER] HW: \(self.telemetry.hwDecodeActive) | Decoded FPS: \(String(format: "%.1f", rate)) | PTS Delta: \(String(format: "%.1f", self.telemetry.ptsProgressionMs))ms"
                print(decLog)
                logger.notice("\(decLog, privacy: .public)")
                TelemetryServer.shared.log(decLog)
            }
            decodedFrameCounter = 0
            lastDecodedRateCheck = now
        }

        if lastDecodedPTS.isValid && frame.pts.isValid {
            let delta = CMTimeSubtract(frame.pts, lastDecodedPTS)
            if delta.isValid && delta.seconds > 0.0 && delta.seconds < 1.0 {
                let deltaMs = delta.seconds * 1000.0
                DispatchQueue.main.async { [weak self] in
                    self?.telemetry.ptsProgressionMs = deltaMs
                }
            }
        }
        if frame.pts.isValid {
            lastDecodedPTS = frame.pts
        }

        renderView?.enqueueFrame(frame)
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.hwDecodeActive = isHWActive
        }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool) {
        DispatchQueue.main.async { [weak self] in
            self?.telemetry.vtDeinterlaceAccepted = isAccepted
        }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.telemetry.decodeErrors += 1
            if self.firstPictureDeliveredTime > 0 && CACurrentMediaTime() - self.firstPictureDeliveredTime <= 2.0 {
                self.telemetry.earlyStabilityIssues += 1
            }
        }
    }

    // MARK: - TTFP Completion Handler

    private func handleFirstFrameRendered() {
        guard firstPictureDeliveredTime == 0, requestStartTime > 0 else { return }
        firstPictureDeliveredTime = CACurrentMediaTime()
        let totalMs = (firstPictureDeliveredTime - requestStartTime) * 1000.0
        let renderBase = firstDecodedTime > 0 ? firstDecodedTime : (firstIdrTime > 0 ? firstIdrTime : requestStartTime)
        let renderMs = (firstPictureDeliveredTime - renderBase) * 1000.0

        let rating: String
        if totalMs <= 800.0 {
            rating = "🎯 Zielwert (≤ 800 ms)"
        } else if totalMs <= 1200.0 {
            rating = "🟢 Gut (≤ 1.2 s)"
        } else if totalMs <= 1500.0 {
            rating = "🟡 Noch gut (≤ 1.5 s)"
        } else if totalMs <= 2000.0 {
            rating = "🟠 Optimierungsbedarf (> 1.5 s)"
        } else {
            rating = "🔴 Inakzeptabel (> 2.0 s)"
        }

        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.telemetry.ttfpTotalMs = totalMs
            self.telemetry.ttfpRenderMs = renderMs
            self.telemetry.ttfpRating = rating
            self.telemetry.isFirstPicturePresented = true
            let ttfpLog = "[1080i50-TTFP] Total: \(String(format: "%.1f", totalMs))ms | Net: \(String(format: "%.1f", self.telemetry.ttfpNetworkMs))ms | PSI: \(String(format: "%.1f", self.telemetry.ttfpPsiMs))ms | Params: \(String(format: "%.1f", self.telemetry.ttfpParamSetsMs))ms | FirstAU: \(String(format: "%.1f", self.telemetry.ttfpIdrMs))ms | Dec: \(String(format: "%.1f", self.telemetry.ttfpDecodeMs))ms | Render: \(String(format: "%.1f", renderMs))ms"
            print(ttfpLog)
            logger.notice("\(ttfpLog, privacy: .public)")
            TelemetryServer.shared.log(ttfpLog)
        }
    }

    private func handleFirstFrameActuallyPresented(screenTimestamp: Double) {
        guard requestStartTime > 0 else { return }
        let gpuDoneMs = (screenTimestamp - requestStartTime) * 1000.0
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.telemetry.ttfpGpuCompletedMs = gpuDoneMs
        }
    }

    // MARK: - System Telemetry Monitoring

    private func startSystemMonitoring() {
        stopSystemMonitoring()
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.updateSystemMetrics()
            self.systemMonitoringTimer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
                self?.updateSystemMetrics()
            }
        }
    }

    private func updateSystemMetrics() {
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

        telemetry.thermalState = thermalString
        telemetry.memoryUsageMB = memMB
        let sysLog = "[1080i50-SYSTEM] Thermal: \(thermalString) | RAM: \(String(format: "%.1f", memMB)) MB"
        print(sysLog)
        logger.notice("\(sysLog, privacy: .public)")
        TelemetryServer.shared.log(sysLog)
    }

    private func stopSystemMonitoring() {
        DispatchQueue.main.async { [weak self] in
            self?.systemMonitoringTimer?.invalidate()
            self?.systemMonitoringTimer = nil
        }
    }
}
