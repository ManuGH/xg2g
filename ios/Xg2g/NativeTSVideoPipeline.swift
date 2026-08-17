// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import OSLog
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "telemetry")

/// Coordinates the end-to-end native DVB TS $\rightarrow$ VideoToolbox $\rightarrow$ Metal Deinterlace pipeline
/// and synchronized Audio Engine (AC-3/E-AC-3/AAC $\rightarrow$ AVSampleBufferAudioRenderer).
public final class NativeTSVideoPipeline: NSObject, ObservableObject, @unchecked Sendable,
    TSPacketParserDelegate,
    PESPacketAssemblerDelegate,
    H264AccessUnitAssemblerDelegate,
    HardwareVideoDecoderDelegate,
    AudioPESAssemblerDelegate,
    AudioSampleBufferAssemblerDelegate,
    URLSessionDataDelegate {

    public let telemetry = StreamTelemetry()

    private let tsParser = TSPacketParser()
    private let pesAssembler = PESPacketAssembler()
    private let accessUnitAssembler = H264AccessUnitAssembler()
    private let decoder = HardwareVideoDecoder()

    // Audio Engine Subsystems
    private let audioPesAssembler = AudioPESAssembler()
    private let ac3FrameParser = AC3FrameParser()
    private let audioSampleBufferAssembler = AudioSampleBufferAssembler()
    public let audioRenderer = NativeTSAudioRenderer()

    public private(set) var selectedAudioPID: UInt16?
    public private(set) var availableAudioTracks: [AudioTrackInfo] = []
    private var isAudioClockStarted = false
    private var audioBuffersPreRolledCount = 0

    public weak var renderView: MetalVideoView?

    private var urlSession: URLSession?
    private var streamTask: URLSessionDataTask?
    private var decodedFrameCounter: Int = 0
    private var lastDecodedRateCheck: Date = Date()
    private var bytesReceived: Int = 0
    private var lastBitrateCheck: Date = Date()
    private var systemMonitoringTimer: Timer?

    /// Owns the parse chain: TS → PES → access units → VideoToolbox.
    ///
    /// Serial, and deliberately not the URLSession delegate queue. Parsing and
    /// decode submission used to run inline on that queue, which accepts no new
    /// data while a delegate callback is executing — so every slow stretch
    /// stalled the socket and the backlog then arrived as a burst. Picture
    /// delivery measured 0–49/s against a 25/s source because of it.
    private let ingestQueue = DispatchQueue(label: "io.github.manugh.xg2g.ingest", qos: .userInitiated)
    private let ingestStateLock = NSLock()
    /// Bumped on stop so feeds queued for a previous stream bail out instead of
    /// corrupting the assembler state of the next one.
    private var ingestGeneration: Int = 0
    private var pendingIngestBytes: Int = 0

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
            let mode = newValue ? "VideoToolbox Native (Path B)" : "Metal Shader (Path A)"
            telemetry.mutate { $0.activeDecoderMode = mode }
        }
    }

    public override init() {
        super.init()
        tsParser.delegate = self
        pesAssembler.delegate = self
        accessUnitAssembler.delegate = self
        decoder.delegate = self
        audioPesAssembler.delegate = self
        ac3FrameParser.delegate = audioSampleBufferAssembler
        audioSampleBufferAssembler.delegate = self

        TelemetryServer.shared.start()
        TelemetryServer.shared.setTelemetryProvider { [weak self] in
            return self?.telemetry.toDictionary() ?? [:]
        }
        // The server owns the main-thread hop and its timeout; this closure only
        // has to promise it runs there.
        TelemetryServer.shared.setScreenshotProvider { [weak self] in
            self?.renderView?.captureCurrentFrameJPEG()
        }
    }

    deinit {
        stopStreaming()
        TelemetryServer.shared.setTelemetryProvider { [:] }
        TelemetryServer.shared.setScreenshotProvider { nil }
    }

    public func startStreaming(url: URL) {
        stopStreaming()
        telemetry.reset()

        requestStartTime = CACurrentMediaTime()
        firstDataTime = 0
        psiParsedTime = 0
        paramsReadyTime = 0
        firstIdrTime = 0
        firstDecodedTime = 0
        firstPictureDeliveredTime = 0

        audioRenderer.activateAudioSession()

        let targetURL = normalizeStreamURL(url)

        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.renderView?.synchronizer = self.audioRenderer.synchronizer
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
        let opQueue = OperationQueue()
        opQueue.maxConcurrentOperationCount = 1
        opQueue.qualityOfService = .userInteractive
        let session = URLSession(configuration: config, delegate: self, delegateQueue: opQueue)
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

        audioRenderer.reset()
        isAudioClockStarted = false
        audioBuffersPreRolledCount = 0
        selectedAudioPID = nil
        availableAudioTracks.removeAll()

        // Retire any feeds still queued for this stream first, so the barrier
        // below returns promptly instead of waiting out the whole backlog.
        ingestStateLock.lock()
        ingestGeneration += 1
        pendingIngestBytes = 0
        ingestStateLock.unlock()

        // The parse chain is owned by `ingestQueue`; resetting it from here while
        // a feed is in flight would corrupt the assembler state mid-packet.
        ingestQueue.sync {
            tsParser.reset()
            pesAssembler.reset()
            accessUnitAssembler.reset()
            decoder.reset()
            audioPesAssembler.reset()
            ac3FrameParser.reset()
            audioSampleBufferAssembler.reset()
        }
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
                let rating = "❌ HTTP \(httpResponse.statusCode) (\(HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode)))"
                telemetry.mutate { $0.ttfpRating = rating }
                completionHandler(.cancel)
                return
            }
        }
        completionHandler(.allow)
    }

    public func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        if let error = error as NSError?, error.code != NSURLErrorCancelled {
            let rating = "❌ Connection Error: \(error.localizedDescription)"
            telemetry.mutate { $0.ttfpRating = rating }
        }
    }

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        if firstDataTime == 0 && requestStartTime > 0 {
            firstDataTime = CACurrentMediaTime()
            let netMs = (firstDataTime - requestStartTime) * 1000.0
            telemetry.mutate { $0.ttfpNetworkMs = netMs }
        }

        bytesReceived += data.count

        ingestStateLock.lock()
        let generation = ingestGeneration
        pendingIngestBytes += data.count
        let backlog = pendingIngestBytes
        ingestStateLock.unlock()

        let now = Date()
        if now.timeIntervalSince(lastBitrateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastBitrateCheck)
            let kbps = (Double(bytesReceived * 8) / 1000.0) / elapsed
            telemetry.mutate {
                $0.tsBitrateKbps = kbps
                $0.ingestBacklogBytes = backlog
            }

            let snapshot = telemetry.snapshot()
            let qualityLog = "[1080i50-QUALITY] Bitrate: \(String(format: "%.1f", kbps)) kbps | VideoPID: \(snapshot.videoPID) | ContinuityErr: \(snapshot.continuityErrors) | PESErr: \(snapshot.pesErrors) | DecErrors: \(snapshot.decodeErrors) | Backlog: \(backlog / 1024) KiB"
            print(qualityLog)
            logger.notice("\(qualityLog, privacy: .public)")
            TelemetryServer.shared.log(qualityLog)

            bytesReceived = 0
            lastBitrateCheck = now
        }

        // Hand off and return, so the socket keeps draining while this chunk is
        // parsed and its access units are submitted to the decoder.
        ingestQueue.async { [weak self] in
            guard let self = self else { return }

            self.ingestStateLock.lock()
            self.pendingIngestBytes -= data.count
            let isStale = generation != self.ingestGeneration
            self.ingestStateLock.unlock()

            guard !isStale else { return }
            self.tsParser.feed(data: data)
        }
    }

    // MARK: - TSPacketParserDelegate

    public func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        if psiParsedTime == 0 {
            psiParsedTime = CACurrentMediaTime()
            let base = firstDataTime > 0 ? firstDataTime : requestStartTime
            let psiMs = (psiParsedTime - base) * 1000.0
            telemetry.mutate { $0.ttfpPsiMs = psiMs }
        }

        telemetry.mutate { $0.videoPID = pid }
    }

    public func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        self.availableAudioTracks = tracks
        let trackListLog = tracks.map { "PID \($0.pid) [\($0.codec), \($0.language ?? "und")]" }.joined(separator: ", ")
        let logMsg = "[1080i50-PMT] 🎵 Discovered \(tracks.count) audio tracks: \(trackListLog)"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)

        // Policy: Prefer German audio if available, else AC-3, else first track
        if selectedAudioPID == nil || !tracks.contains(where: { $0.pid == selectedAudioPID }) {
            let preferred: AudioTrackInfo?
            if let deu = tracks.first(where: { $0.language == "deu" }) {
                preferred = deu
            } else if let ac3 = tracks.first(where: { $0.codec == .ac3 || $0.codec == .eac3 }) {
                preferred = ac3
            } else {
                preferred = tracks.first
            }

            if let track = preferred {
                self.selectedAudioPID = track.pid
                let selLog = "[1080i50-AUDIO] 🎯 Selected audio track: PID \(track.pid) (\(track.codec), lang: \(track.language ?? "und"))"
                print(selLog)
                logger.notice("\(selLog, privacy: .public)")
                TelemetryServer.shared.log(selLog)

                telemetry.mutate {
                    $0.audioPID = track.pid
                    $0.audioCodec = track.codec.description
                    $0.audioLanguage = track.language ?? "und"
                }
            }
        }
    }

    public func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        if let vPid = tsParser.videoPID, pid == vPid {
            pesAssembler.feed(payload: data, unitStart: unitStart)
        } else if let aPid = selectedAudioPID, pid == aPid {
            audioPesAssembler.feed(payload: data, pid: pid, unitStart: unitStart)
        }
    }

    public func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        telemetry.mutate { $0.continuityErrors += 1 }
    }

    // MARK: - AudioPESAssemblerDelegate

    public func audioPESAssembler(_ assembler: AudioPESAssembler, didEmitAudioPES payload: AudioPESData) {
        ac3FrameParser.feed(data: payload.payload, pts: payload.pts, pts90k: payload.pts90k)
    }

    public func audioPESAssembler(_ assembler: AudioPESAssembler, didEncounterPESError reason: String, onPID pid: UInt16) {
        telemetry.mutate { $0.pesErrors += 1 }
    }

    // MARK: - AudioSampleBufferAssemblerDelegate

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, info: AC3FrameInfo) {
        let logMsg = "[1080i50-AUDIO] Format: \(info.isEnhanced ? "E-AC-3" : "AC-3") | \(info.sampleRate) Hz | \(info.channelCount) ch (\(info.isLFEOn ? ".1 LFE" : "no LFE")) | \(info.bitrateKbps) kbps"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)

        telemetry.mutate {
            $0.audioSampleRate = info.sampleRate
            $0.audioChannels = info.channelCount
            $0.audioBitrateKbps = info.bitrateKbps
        }
    }

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, info: AC3FrameInfo) {
        audioRenderer.enqueue(sampleBuffer: sampleBuffer)
        audioBuffersPreRolledCount += 1

        if !isAudioClockStarted && audioBuffersPreRolledCount >= 3 {
            let pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer)
            if pts.isValid {
                audioRenderer.setRate(1.0, time: pts)
                isAudioClockStarted = true
                let clockLog = "[1080i50-CLOCK] ⏱️ Master Audio Clock started at PTS: \(String(format: "%.3f", pts.seconds))s"
                print(clockLog)
                logger.notice("\(clockLog, privacy: .public)")
                TelemetryServer.shared.log(clockLog)

                telemetry.mutate {
                    $0.isAudioMasterClockActive = true
                }
            }
        }
    }

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String) {
        telemetry.mutate { $0.decodeErrors += 1 }
    }

    // MARK: - PESPacketAssemblerDelegate

    public func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        accessUnitAssembler.feed(payload: payload)
    }

    public func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        telemetry.mutate { $0.pesErrors += 1 }
    }

    // MARK: - H264AccessUnitAssemblerDelegate

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        if paramsReadyTime == 0 {
            paramsReadyTime = CACurrentMediaTime()
            let base = psiParsedTime > 0 ? psiParsedTime : (firstDataTime > 0 ? firstDataTime : requestStartTime)
            let paramMs = (paramsReadyTime - base) * 1000.0
            telemetry.mutate { $0.ttfpParamSetsMs = paramMs }
        }

        decoder.configure(with: formatDescription)

        // Drives whether the render view bob-deinterlaces or passes through.
        let interlaced = info.isInterlaced
        DispatchQueue.main.async { [weak self] in
            self?.renderView?.sourceIsInterlaced = interlaced
        }

        let logMsg = "[1080i50-CODEC] Format: \(info.width)x\(info.height) | Interlaced: \(info.isInterlaced) | TFF: \(info.isTopFieldFirst)"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)
        telemetry.mutate {
            $0.videoWidth = info.width
            $0.videoHeight = info.height
            $0.isInterlaced = info.isInterlaced
            $0.fieldOrder = info.isInterlaced ? (info.isTopFieldFirst ? "TFF" : "BFF") : "Progressive"
            $0.vtSessionActive = true

            if info.isInterlaced {
                $0.isDirect1080iVerified = true
                $0.validationWarning = nil
            } else {
                $0.isDirect1080iVerified = false
                $0.validationWarning = "⚠️ WARNUNG: Stream ist PROGRESSIV (\(info.width)x\(info.height)p) – Server-Transcode aktiv!"
            }
        }
    }

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isIDR: Bool, structure: H264PictureStructure) {
        if firstIdrTime == 0 {
            firstIdrTime = CACurrentMediaTime()
            let base = paramsReadyTime > 0 ? paramsReadyTime : (psiParsedTime > 0 ? psiParsedTime : requestStartTime)
            let idrMs = (firstIdrTime - base) * 1000.0
            telemetry.mutate { $0.ttfpIdrMs = idrMs }
        }

        // One PTS per coded picture is what a well-formed stream carries. Access
        // units arriving without one mean the assembler split a picture, which
        // inflates the decode rate above the broadcast frame rate.
        let hasPTS = CMSampleBufferGetPresentationTimeStamp(sampleBuffer).isValid
        telemetry.mutate {
            $0.sampleBuffersEmittedCount += 1
            if !hasPTS { $0.accessUnitsWithoutPTS += 1 }
        }

        decoder.decode(sampleBuffer: sampleBuffer, structure: structure)
    }

    // MARK: - HardwareVideoDecoderDelegate

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        if firstDecodedTime == 0 {
            firstDecodedTime = CACurrentMediaTime()
            let base = firstIdrTime > 0 ? firstIdrTime : (paramsReadyTime > 0 ? paramsReadyTime : requestStartTime)
            let decMs = (firstDecodedTime - base) * 1000.0
            telemetry.mutate { $0.ttfpDecodeMs = decMs }
        }

        telemetry.mutate { $0.sampleBuffersDecodedCount += 1 }

        decodedFrameCounter += 1
        let now = Date()
        if now.timeIntervalSince(lastDecodedRateCheck) >= 1.0 {
            let elapsed = now.timeIntervalSince(lastDecodedRateCheck)
            let rate = Double(decodedFrameCounter) / elapsed
            telemetry.mutate { $0.decodedFramesPerSec = rate }

            let snapshot = telemetry.snapshot()
            let decLog = "[1080i50-DECODER] HW: \(snapshot.hwDecodeActive) | Decoded AU/s: \(String(format: "%.1f", rate)) | Source: \(String(format: "%.1f", snapshot.sourceFrameRate)) fps (PTS delta \(String(format: "%.1f", snapshot.ptsProgressionMs))ms) | AUs w/o PTS: \(snapshot.accessUnitsWithoutPTS)"
            print(decLog)
            logger.notice("\(decLog, privacy: .public)")
            TelemetryServer.shared.log(decLog)

            decodedFrameCounter = 0
            lastDecodedRateCheck = now
        }

        // `ptsProgressionMs` is measured by the render view instead: VideoToolbox
        // emits in decode order, so consecutive deltas here run backwards across
        // every B-frame and only the reorder buffer sees the true cadence.
        renderView?.enqueueFrame(frame)
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        telemetry.mutate { $0.hwDecodeActive = isHWActive }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool) {
        telemetry.mutate { $0.vtDeinterlaceAccepted = isAccepted }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        let isEarly = firstPictureDeliveredTime > 0
            && CACurrentMediaTime() - firstPictureDeliveredTime <= 2.0
        telemetry.mutate {
            $0.decodeErrors += 1
            if isEarly {
                $0.earlyStabilityIssues += 1
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

        telemetry.mutate {
            $0.ttfpTotalMs = totalMs
            $0.ttfpRenderMs = renderMs
            $0.ttfpRating = rating
            $0.isFirstPicturePresented = true
        }

        let snapshot = telemetry.snapshot()
        let ttfpLog = "[1080i50-TTFP] Total: \(String(format: "%.1f", totalMs))ms | Net: \(String(format: "%.1f", snapshot.ttfpNetworkMs))ms | PSI: \(String(format: "%.1f", snapshot.ttfpPsiMs))ms | Params: \(String(format: "%.1f", snapshot.ttfpParamSetsMs))ms | FirstAU: \(String(format: "%.1f", snapshot.ttfpIdrMs))ms | Dec: \(String(format: "%.1f", snapshot.ttfpDecodeMs))ms | Render: \(String(format: "%.1f", renderMs))ms"
        print(ttfpLog)
        logger.notice("\(ttfpLog, privacy: .public)")
        TelemetryServer.shared.log(ttfpLog)
    }

    private func handleFirstFrameActuallyPresented(screenTimestamp: Double) {
        guard requestStartTime > 0 else { return }
        let gpuDoneMs = (screenTimestamp - requestStartTime) * 1000.0
        telemetry.mutate { $0.ttfpGpuCompletedMs = gpuDoneMs }
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

        telemetry.mutate {
            $0.thermalState = thermalString
            $0.memoryUsageMB = memMB
        }
        // Low Power Mode caps ProMotion at 60 Hz regardless of the display link's
        // preferred range, so it has to be visible before a 60 Hz reading gets
        // blamed on the Info.plist key or the frame-rate request.
        let lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
        let sysLog = "[1080i50-SYSTEM] Thermal: \(thermalString) | RAM: \(String(format: "%.1f", memMB)) MB | LowPower: \(lowPower)"
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
