// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import OSLog
import UIKit

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "telemetry")

private final class PipelineSessionState: @unchecked Sendable {
    let generation: Int
    private let lock = NSLock()

    var requestStartTime: CFTimeInterval = 0
    var firstDataTime: CFTimeInterval = 0
    var psiParsedTime: CFTimeInterval = 0
    var paramsReadyTime: CFTimeInterval = 0
    var firstIdrTime: CFTimeInterval = 0
    var firstDecodedTime: CFTimeInterval = 0
    var firstPictureDeliveredTime: CFTimeInterval = 0

    var decodedFrameCounter: Int = 0
    var lastDecodedRateCheck: Date = Date()

    /// Socket delivery cadence. A live TS arrives continuously, so a gap here is
    /// the network or the server stalling — indistinguishable, from inside the
    /// app, from a decode problem unless it is measured separately.
    /// When the HTTP request actually went out.
    ///
    /// Separate from `requestStartTime`, which is when the app was asked to
    /// tune: between the two sit the previous stream's teardown and the audio
    /// session activation, and charging those to the network made the receiver
    /// look slower than it is.
    var requestIssuedTime: CFTimeInterval = 0

    /// What is known about this channel's audio, and when the picture started.
    ///
    /// Kept here rather than in a plain property because it is written from the
    /// parse queue and read from the decoder callback.
    var audioTracksKnown = false
    var hasDecodableAudio = false
    var firstVideoFieldTime: CFTimeInterval = 0

    var lastDataArrival: CFTimeInterval = 0
    var stallCount: Int = 0
    var longestStallMs: Double = 0

    init(generation: Int, requestStartTime: CFTimeInterval = CACurrentMediaTime()) {
        self.generation = generation
        self.requestStartTime = requestStartTime
    }

    func mutate<T>(_ block: (PipelineSessionState) -> T) -> T {
        lock.lock()
        defer { lock.unlock() }
        return block(self)
    }
}

public enum DecodeGateState: Sendable, Equatable {
    case closed(reason: DecodeGateCloseReason)
    case open
}

public enum DecodeGateCloseReason: Sendable, Equatable {
    case startup
    case decoderRecovery
    case formatReconfiguration
    case backgrounded
}

/// Coordinates the end-to-end native DVB TS $\rightarrow$ VideoToolbox $\rightarrow$ Metal Deinterlace pipeline
/// and synchronized Audio Engine (AC-3/E-AC-3/AAC $\rightarrow$ AVSampleBufferAudioRenderer).
public final class NativeTSVideoPipeline: NSObject, ObservableObject, @unchecked Sendable,
    TSPacketParserDelegate,
    PESPacketAssemblerDelegate,
    H264AccessUnitAssemblerDelegate,
    VideoAccessUnitAssemblerDelegate,
    HardwareVideoDecoderDelegate,
    AudioPESAssemblerDelegate,
    AudioSampleBufferAssemblerDelegate,
    NativeTSAudioRendererDelegate,
    URLSessionDataDelegate {

    public let telemetry = StreamTelemetry()

    private let tsParser = TSPacketParser()
    private let pesAssembler = PESPacketAssembler()
    private let accessUnitAssembler = H264AccessUnitAssembler()

    /// Assembler for a stream that is not H.264, chosen when the PMT names its
    /// codec. `nil` on an H.264 channel, which is the overwhelming majority and
    /// keeps its dedicated path untouched.
    private var alternateAssembler: (any VideoAccessUnitAssembling)?
    private let decoder = HardwareVideoDecoder()

    // Audio Engine Subsystems
    private let audioPesAssembler = AudioPESAssembler()
    private let ac3FrameParser = AC3FrameParser()
    private let aacFrameParser = AACADTSFrameParser()
    private let audioSampleBufferAssembler = AudioSampleBufferAssembler()
    /// The audio side of this session.
    ///
    /// Typed as the protocol so a test can substitute a controllable implementation.
    /// Production always gets `NativeTSAudioRenderer`, and therefore the real renderer,
    /// synchronizer and audio session.
    public let audioRenderer: PlaybackAudioOutput

    public private(set) var selectedAudioPID: UInt16?
    public private(set) var selectedAudioCodec: AudioStreamCodec = .ac3
    public private(set) var availableAudioTracks: [AudioTrackInfo] = []
    private var isAudioClockStarted = false
    private var audioBuffersPreRolledCount = 0

    /// Audio buffered before the master clock starts, and therefore the cushion
    /// playback keeps for the rest of the stream: the renderer is fed at the rate
    /// the broadcast delivers, so this depth never grows back once spent.
    ///
    /// 1.2 s is chosen against how the receiver actually delivers, measured
    /// directly off the box with no app involved: it writes in bursts of over
    /// 30 Mbps and is idle 92 % of the time, leaving gaps of up to 872 ms on an
    /// otherwise completely unused box, 939 ms across every run taken. Five
    /// measurements at zero, two and three concurrent clients put the worst gap
    /// at 603–939 ms with no relation to the load, so this is what the hardware
    /// does rather than a symptom of something else using it.
    ///
    /// Every gap longer than this cushion is an audible dropout, and 500 ms sat
    /// below most of them: one device session logged 228 underruns while video,
    /// sitting behind a queue of up to 180 fields, showed nothing at all. That
    /// asymmetry is the whole reason picture and sound behaved differently.
    ///
    /// The cost is about 400 ms more before sound and motion begin. It is not
    /// 400 ms of black screen — the first field carries `DisplayImmediately` and
    /// is on screen at once — it is 400 ms longer holding that first picture.
    /// When true, enables the two-phase early motion experiment with explicit audio buffer
    /// pruning and milestone recovery telemetry.
    public nonisolated(unsafe) static var enableEarlyMotionExperiment: Bool = true

    private static let audioPreRollSeconds: Double = 0.9
    private static let videoOnlyCushionSeconds: Double = 0.8

    /// How long a channel may go without naming any audio before it is taken to
    /// have none. Long enough that a late PMT is not mistaken for a silent
    /// service.
    private static let videoOnlyClockDelay: Double = 3.0

    /// How long a picture may sit on screen without moving before that is said
    /// out loud. Well past any legitimate cushion.
    private static let motionWatchdogSeconds: Double = 5.0

    /// The only route to the visible surface.
    ///
    /// A session holds no reference to the render view or the presenter. It cannot
    /// attach a synchronizer, flush a queue or reset a generation, which is what makes
    /// preparing one beside a playing one safe.
    public weak var presentationContext: PresentationContext? {
        didSet { surfaceOutlet = presentationContext?.outlet }
    }

    /// Where decoded pictures go. Held directly because they are produced on the
    /// decoder's queue and the context is main-actor.
    private var surfaceOutlet: PresentationContext.SurfaceOutlet?

    /// Stamps everything this session emits. Issued by the presentation context.
    ///
    /// The decoder is restamped with it, so it does not matter whether the generation
    /// arrives before or after the stream is started. It arrived after in the first
    /// two-session test, and every decoded picture was silently discarded as belonging
    /// to a generation the session no longer had.
    public var presentationGeneration: PresentationGeneration = .none {
        didSet { decoder.decodeGeneration = presentationGeneration.rawValue }
    }

    /// When the last anchor rejection was reported, so a stream that cannot anchor says
    /// so without filling the log.
    private var lastAnchorRejectionLog: CFTimeInterval = 0

    /// Reports why a start anchor could not be chosen.
    ///
    /// A channel that never anchors never starts, and until now that looked identical to
    /// a channel still arriving. The numbers that decide it are worth a line a second.
    private func noteAnchorRejected(reason: String, anchorSeconds: Double, firstAudio: Double, cushion: Double) {
        let now = CACurrentMediaTime()
        guard now - lastAnchorRejectionLog >= 1.0 else { return }
        lastAnchorRejectionLog = now
        let msg = "[1080i50-ANCHOR] ⏸ no start anchor: \(reason) | anchor \(String(format: "%.3f", anchorSeconds))s | first audio \(String(format: "%.3f", firstAudio))s | latest picture \(String(format: "%.3f", latestVideoPTS.seconds))s | cushion \(String(format: "%.0f", cushion * 1000))ms"
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    private let recoveryLock = NSLock()
    private var _lifecycle: PlaybackLifecycle = .stable
    private var _recoveryEpoch: RecoveryEpoch = .initial

    /// Whether this session is currently rebuilding itself.
    ///
    /// Set the instant a failure or a timeline change is seen, not when the rebuilding
    /// gets around to running. The reset happens asynchronously on the ingest queue, and
    /// anything anchored between the failure and the reset was anchored on a timeline
    /// that no longer exists - so the session has to be known to be unusable from the
    /// failure onwards, not from the reset onwards.
    public var lifecycle: PlaybackLifecycle {
        recoveryLock.lock(); defer { recoveryLock.unlock() }
        return _lifecycle
    }

    /// Which recovery attempt is current.
    public var recoveryEpoch: RecoveryEpoch {
        recoveryLock.lock(); defer { recoveryLock.unlock() }
        return _recoveryEpoch
    }

    /// Opens a recovery attempt and returns its epoch.
    @discardableResult
    private func beginRecovery(_ reason: String) -> RecoveryEpoch {
        recoveryLock.lock()
        _recoveryEpoch = _recoveryEpoch.next()
        _lifecycle = .recovering
        let epoch = _recoveryEpoch
        recoveryLock.unlock()

        let msg = "[1080i50-RECOVERY] ↻ \(epoch) opened: \(reason)"
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
        return epoch
    }

    /// Whether an asynchronous step still belongs to the current attempt.
    ///
    /// A second failure can arrive before the first attempt has finished. Without this,
    /// the late half of the first would clear what the second had just rebuilt.
    private func isCurrentRecovery(_ epoch: RecoveryEpoch) -> Bool {
        recoveryLock.lock(); defer { recoveryLock.unlock() }
        return _recoveryEpoch == epoch
    }

    /// Closes the current attempt.
    ///
    /// Deliberately not called when the reset finishes. Recovery is over when the
    /// session can be used again - old timeline discarded, a new audio anchor
    /// established and a picture anchored to it - which is exactly the moment a start
    /// anchor is chosen. Reporting it any earlier would let a session be committed
    /// while it still had nothing to show.
    private func completeRecoveryIfNeeded() {
        recoveryLock.lock()
        let wasRecovering = _lifecycle == .recovering
        _lifecycle = .stable
        let epoch = _recoveryEpoch
        recoveryLock.unlock()
        guard wasRecovering else { return }

        let msg = "[1080i50-RECOVERY] ✔ \(epoch) closed: a start anchor exists again"
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    /// The first picture this session decoded, whether or not it was ever shown.
    ///
    /// Session-local on purpose: it is what says the decoder is producing, which a
    /// preparing session has to be able to establish without the screen.
    private var firstDecodedPicturePTS: CMTime?

    /// The timestamp the clock will start on when this session is committed.
    ///
    /// Chosen while preparing, from the picture the surface will show first, so the
    /// commit itself has nothing left to decide.
    private var commitAnchor: CMTime?

    /// Set when the stream is presented through AVFoundation rather than through
    /// our own drawable, which is what makes Picture in Picture and the system's
    /// video features available. See `SystemVideoPresenter`.

    private var telemetryForegroundObserver: NSObjectProtocol?
    private var appBackgroundObserver: NSObjectProtocol?
    private var audioInterruptionObserver: NSObjectProtocol?
    private var audioRouteChangeObserver: NSObjectProtocol?
    private var audioFlushedObserver: NSObjectProtocol?
    private var isAudioInterrupted = false

    private var urlSession: URLSession?
    private var streamTask: URLSessionDataTask?
    
    /// TTFP stage timestamps and decode-rate counters for the *current* stream.
    ///
    /// One object per `startStreaming`, replaced wholesale on a zap: callbacks
    /// still in flight for the previous stream then write into the retired state
    /// instead of contaminating the new stream's measurements. The reference is
    /// swapped from the caller's thread and read from the URLSession delegate
    /// queue, the ingest queue and the VideoToolbox callback thread, so the swap
    /// itself is locked too — the per-field locking inside `PipelineSessionState`
    /// protects its contents, not the pointer to it.
    private var _sessionState = PipelineSessionState(generation: 0)
    private let sessionStateLock = NSLock()

    private var sessionState: PipelineSessionState {
        get {
            sessionStateLock.lock()
            defer { sessionStateLock.unlock() }
            return _sessionState
        }
        set {
            sessionStateLock.lock()
            _sessionState = newValue
            sessionStateLock.unlock()
        }
    }

    private var bytesReceived: Int = 0
    private var lastBitrateCheck: Date = Date()
    private var systemMonitoringTimer: Timer?
    private var thermalObserver: NSObjectProtocol?

    /// Watches the master clock for the first field's timestamp.
    ///
    /// The synchronizer is rebuilt on every zap, so the observer is stored with
    /// the instance it was registered on — removing it from the replacement
    /// would silently do nothing and leak the old one.
    private var firstPictureObserver: (token: Any, synchronizer: AVSampleBufferRenderSynchronizer)?

    /// Owns the parse chain: TS → PES → access units → VideoToolbox.
    ///
    /// Serial, and deliberately not the URLSession delegate queue. Parsing and
    /// decode submission used to run inline on that queue, which accepts no new
    /// data while a delegate callback is executing — so every slow stretch
    /// stalled the socket and the backlog then arrived as a burst. Picture
    /// delivery measured 0–49/s against a 25/s source because of it.
    private let ingestQueue = DispatchQueue(label: "io.github.manugh.xg2g.ingest", qos: .userInitiated)
    private let ingestStateLock = NSLock()
    private let zapLock = NSLock()
    public private(set) var currentZapId: Int = 0
    /// Bumped on stop so feeds queued for a previous stream bail out instead of
    /// corrupting the assembler state of the next one.
    private var ingestGeneration: Int = 0
    private var pendingIngestBytes: Int = 0
    private var currentChannelKey: String = ""

    /// Ingest thread safety and decode gate state.
    /// Only touched from `ingestQueue`.
    public private(set) var decodeGateState: DecodeGateState = .closed(reason: .startup)
    private var gatedAccessUnitCount = 0
    private var firstAccessUnitTime: CFTimeInterval = 0

    /// Ceiling on how long the gate may hold pictures back during initial startup ONLY.
    private static let decodeGateTimeout: Double = 2.0

    public var useNativeVTDeinterlace: Bool {
        get { decoder.useNativeVTDeinterlace }
        set {
            decoder.useNativeVTDeinterlace = newValue
            let mode = newValue ? "VideoToolbox Native (Path B)" : "Metal Shader (Path A)"
            telemetry.mutate { $0.activeDecoderMode = mode }
        }
    }

    /// - Parameter audioOutput: substituted only by tests. Everything that decides a
    ///   start anchor, a re-anchor or a commit is this class's own; the renderer behind
    ///   this is Apple's, and proving the two together is what made the commit proof
    ///   depend on whether the simulator's media services happened to survive the run.
    public convenience override init() {
        self.init(audioOutput: NativeTSAudioRenderer())
    }

    public init(audioOutput: PlaybackAudioOutput) {
        self.audioRenderer = audioOutput
        super.init()
        self.finishInit()
    }

    private func finishInit() {
        tsParser.delegate = self
        pesAssembler.delegate = self
        accessUnitAssembler.delegate = self
        decoder.delegate = self
        audioPesAssembler.delegate = self
        ac3FrameParser.delegate = audioSampleBufferAssembler
        aacFrameParser.delegate = audioSampleBufferAssembler
        audioSampleBufferAssembler.delegate = self
        audioRenderer.delegate = self

        setupAudioNotificationObservers()
        setupLifecycleNotificationObservers()

        TelemetryServer.shared.start()
        if telemetryForegroundObserver == nil {
            telemetryForegroundObserver = NotificationCenter.default.addObserver(
                forName: UIApplication.didBecomeActiveNotification,
                object: nil,
                queue: .main
            ) { _ in
                TelemetryServer.shared.restartAfterForeground()
            }
        }
        // The telemetry and screenshot endpoints answer for the channel on screen, and
        // a session does not know whether it is that channel. Sessions are built one
        // beside another now, so a session that pointed the endpoints at itself would
        // be reporting a channel still being prepared while the viewer watches another
        // one — and would blank both endpoints on its way out. Whoever owns the surface
        // installs them.
    }

    private func setupLifecycleNotificationObservers() {
        if appBackgroundObserver == nil {
            appBackgroundObserver = NotificationCenter.default.addObserver(
                forName: UIApplication.didEnterBackgroundNotification,
                object: nil,
                queue: nil
            ) { [weak self] _ in
                self?.handleAppDidEnterBackground()
            }
        }
    }

    private func handleAppDidEnterBackground() {
        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            self.decoder.invalidateSessionForBackground()
            self.decodeGateState = .closed(reason: .backgrounded)
            self.gatedAccessUnitCount = 0
            self.firstAccessUnitTime = CACurrentMediaTime()
            self.telemetry.mutate {
                $0.vtSessionActive = false
                $0.hwDecodeActive = false
            }
            let msg = "[1080i50-LIFECYCLE] 📱 App entered background: Decoder session invalidated, gate closed (.backgrounded, strict IDR only)"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)
        }
    }

    deinit {
        stopStreaming()
        if let obs = audioInterruptionObserver { NotificationCenter.default.removeObserver(obs) }
        if let obs = audioRouteChangeObserver { NotificationCenter.default.removeObserver(obs) }
        if let obs = audioFlushedObserver { NotificationCenter.default.removeObserver(obs) }
        if let obs = telemetryForegroundObserver { NotificationCenter.default.removeObserver(obs) }
        if let obs = appBackgroundObserver { NotificationCenter.default.removeObserver(obs) }
    }

    /// Identifier sent with a channel change so the backend timeline and this one
    /// can be lined up. Prefixed by install so two devices zapping at once do not
    /// produce colliding identifiers in the same server log.
    static func zapIdentifier(_ zapId: Int) -> String {
        "ios-\(installIdentifier)-\(zapId)"
    }

    /// Stable for the lifetime of the install, and not derived from anything that
    /// identifies the device or its owner.
    private static let installIdentifier: String = {
        let key = "io.github.manugh.xg2g.zapInstallIdentifier"
        if let existing = UserDefaults.standard.string(forKey: key) { return existing }
        let generated = String(UUID().uuidString.prefix(8)).lowercased()
        UserDefaults.standard.set(generated, forKey: key)
        return generated
    }()

    /// - Parameter requestedAt: when the user asked for this, which is not the
    ///   same as when this function runs. Callers that do work first — resolving
    ///   a URL, dismissing a screen — should stamp their own start so the figure
    ///   covers what the viewer waited through rather than what was left of it.
    public func startStreaming(url: URL, requestedAt: CFTimeInterval = CACurrentMediaTime()) {
        zapLock.lock()
        currentZapId += 1
        let zapId = currentZapId
        zapLock.unlock()

        let preMem = Self.currentMemoryStats()
        let prePresenterQ = 0 // presenter queue depth is main-actor state; the presenter logs its own
        let preVTInFlight = decoder.inFlightFrames
        ingestStateLock.lock()
        let preBacklogKiB = pendingIngestBytes / 1024
        ingestStateLock.unlock()

        let preZapLog = "[ZAP-#\(zapId)-PRE] 🎬 Zap initiated -> \(url.lastPathComponent) | Pre-State: PresenterQ=\(prePresenterQ), VTInFlight=\(preVTInFlight), IngestBacklog=\(preBacklogKiB)KiB, Resident=\(String(format: "%.1f", preMem.residentMB))MB, Footprint=\(String(format: "%.1f", preMem.footprintMB))MB"
        print(preZapLog)
        logger.notice("\(preZapLog, privacy: .public)")
        TelemetryServer.shared.log(preZapLog)

        let teardownStart = CACurrentMediaTime()
        stopStreaming()
        let teardownMs = (CACurrentMediaTime() - teardownStart) * 1000.0
        telemetry.reset()

        ingestStateLock.lock()
        let currentGen = ingestGeneration
        ingestStateLock.unlock()

        let teardownLog = "[ZAP-#\(zapId)-TEARDOWN] ⏹️ Old session cancelled & flushed in \(String(format: "%.1f", teardownMs))ms | PresenterQ=0, AudioQ=0, IngestGen=\(currentGen)"
        print(teardownLog)
        logger.notice("\(teardownLog, privacy: .public)")
        TelemetryServer.shared.log(teardownLog)

        // Two different generations, deliberately.
        //
        // The session's own counter says "this session has started a stream", which is
        // what the recovery paths test before they act. The presentation generation
        // says which stream the surface belongs to, and is what every decoded picture
        // is stamped with, because that is the number the surface compares. Folding
        // them into one made an unbound session look like it had never started.
        sessionState = PipelineSessionState(generation: zapId, requestStartTime: requestedAt)
        decoder.decodeGeneration = presentationGeneration.rawValue

        // AVAudioSession is process-global policy and is activated once for the player,
        // not once per channel change. A playback session that owned it would configure
        // the whole process every time it was prepared.
        let audioSessionMs = 0.0

        let targetURL = normalizeStreamURL(url)
        let channelKey = targetURL.absoluteString
        currentChannelKey = channelKey
        accessUnitAssembler.channelKey = channelKey
        ChannelJitterProfiler.shared.noteZap(for: channelKey)

        if let cached = H264ParameterSetCache.shared.parameterSets(for: channelKey) {
            accessUnitAssembler.primeWithParameterSets(sps: cached.sps, pps: cached.pps)

            sessionState.mutate { state in
                state.paramsReadyTime = CACurrentMediaTime()
            }

            let logMsg = "[ZAP-#\(zapId)-PARAMS] ⚡ Primed decoder with cached SPS/PPS for instant tuning!"
            print(logMsg)
            logger.notice("\(logMsg, privacy: .public)")
            TelemetryServer.shared.log(logMsg)
        }

        let config = URLSessionConfiguration.ephemeral
        config.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        config.timeoutIntervalForRequest = 5.0
        config.waitsForConnectivity = false
        config.allowsCellularAccess = true
        config.allowsExpensiveNetworkAccess = true
        config.allowsConstrainedNetworkAccess = true
        config.httpShouldUsePipelining = true

        let opQueue = OperationQueue()
        opQueue.maxConcurrentOperationCount = 1
        opQueue.qualityOfService = .userInteractive
        let session = URLSession(configuration: config, delegate: self, delegateQueue: opQueue)
        self.urlSession = session

        // Nothing here touches the presenter, the render view or the global telemetry
        // provider. Binding the visible surface is the presentation context's job and
        // happens at commit; a session that reached in here would black out whatever
        // channel is currently playing, which is exactly what a preparation must not do.

        var request = URLRequest(url: targetURL)
        request.setValue("xg2g-ios-native-poc/1.0", forHTTPHeaderField: "User-Agent")
        // Stamps this channel change so one zap can be followed across both sides of
        // the wire. The backend logs it as a field and never as a metric label, so it
        // only has to be unique within this app run - not globally.
        request.setValue(Self.zapIdentifier(zapId), forHTTPHeaderField: "X-Xg2g-Zap-Id")

        let task = session.dataTask(with: request)
        self.streamTask = task
        task.resume()

        let issuedAt = CACurrentMediaTime()
        sessionState.mutate { $0.requestIssuedTime = issuedAt }
        let setupMs = (issuedAt - requestedAt) * 1000.0
        telemetry.mutate {
            $0.ttfpSetupMs = setupMs
            $0.ttfpTeardownMs = teardownMs
            $0.ttfpAudioSessionMs = audioSessionMs
        }
        let setupLog = "[ZAP-#\(zapId)-SETUP] 🛠️ Setup \(String(format: "%.1f", setupMs))ms before request (teardown \(String(format: "%.1f", teardownMs))ms, audio \(String(format: "%.1f", audioSessionMs))ms)"
        print(setupLog)
        logger.notice("\(setupLog, privacy: .public)")
        TelemetryServer.shared.log(setupLog)

        let startLog = "[ZAP-#\(zapId)-NET] ▶️ Requesting \(targetURL.absoluteString) | Timeout: \(config.timeoutIntervalForRequest)s | ZapID: \(Self.zapIdentifier(zapId))"
        print(startLog)
        logger.notice("\(startLog, privacy: .public)")
        TelemetryServer.shared.log(startLog)

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

    public func feedData(_ data: Data) {
        ingestQueue.sync {
            self.tsParser.feed(data: data)
        }
    }

    /// True when this channel will not produce sound on this device.
    ///
    /// Either it named its audio and none of it is decodable here — MPEG Layer II
    /// is the common case, carried by most German public broadcasters and with no
    /// decoder on iOS — or it has named no audio at all for long enough that a
    /// late table is no longer the explanation.
    /// The rule itself, kept apart from the state it reads so it can be pinned
    /// by tests rather than inferred from a decoder callback.
    ///
    /// - Parameters:
    ///   - audioTracksKnown: whether a PMT has named this channel's audio yet.
    ///   - hasDecodableAudio: whether any named track can be decoded here.
    ///   - secondsSinceFirstField: how long there has been a picture.
    ///   - pictureBufferedSeconds: how much picture is queued ahead of the anchor.
    static func shouldStartClockOnPictureAlone(
        audioTracksKnown: Bool,
        hasDecodableAudio: Bool,
        secondsSinceFirstField: Double,
        pictureBufferedSeconds: Double
    ) -> Bool {
        guard pictureBufferedSeconds >= videoOnlyCushionSeconds else { return false }
        // Named its audio: the answer is known now and waiting adds nothing.
        if audioTracksKnown { return !hasDecodableAudio }
        // Named none yet, which at tune-in is ordinary rather than final.
        return secondsSinceFirstField >= videoOnlyClockDelay
    }

    /// Starts the clock on the picture when sound cannot come.
    ///
    /// The clock only ever started on audio, so a channel with nothing playable
    /// did not play silently — it stopped. Measured on one: video decoded at
    /// 50 fps, the display layer was never once ready for more (121 pulls, ready
    /// 0), the field queue filled to its cap and shed 1394 fields against 13
    /// delivered, and the screen held the single field `DisplayImmediately` had
    /// put there. Every counter in the app read healthy. The warning that goes
    /// with it says playback "will be silent", which was not true either.
    ///
    /// Deliberately narrow. It fires only where sound is impossible, never where
    /// a decodable track has merely been slow: starting the clock on a stream
    /// that does have audio puts every later buffer in the past, and the renderer
    /// discards those — a permanently silent channel, which is a worse fault than
    /// the one being fixed.
    ///
    /// Anchored at the first field rather than the newest, which also means any
    /// audio that does turn up later is ahead of the clock and still plays.
    private func startVideoOnlyClockIfNeeded() {
        guard !isAudioClockStarted else { return }
        guard let anchor = firstVideoFieldPTS, anchor.isValid, latestVideoPTS.isValid else { return }
        let (known, decodable, since) = sessionState.mutate { state -> (Bool, Bool, Double) in
            let elapsed = state.firstVideoFieldTime > 0
                ? CACurrentMediaTime() - state.firstVideoFieldTime
                : 0
            return (state.audioTracksKnown, state.hasDecodableAudio, elapsed)
        }
        guard Self.shouldStartClockOnPictureAlone(
            audioTracksKnown: known,
            hasDecodableAudio: decodable,
            secondsSinceFirstField: since,
            pictureBufferedSeconds: latestVideoPTS.seconds - anchor.seconds
        ) else { return }

        commitAnchor = anchor
        completeRecoveryIfNeeded()

        // Same rule as the ordinary clock start: only the session that owns the surface
        // runs a clock. A prepared session with no playable audio records its anchor
        // and waits for the commit like any other.
        guard surfaceOutlet?.owns(presentationGeneration) == true else { return }

        audioRenderer.setRate(1.0, time: anchor)
        isAudioClockStarted = true
        notePlaybackStateChanged()

        let msg = "[1080i50-CLOCK] 🔇 No audio this device can play — clock started on the picture at PTS \(String(format: "%.3f", anchor.seconds))s with \(String(format: "%.0f", (latestVideoPTS.seconds - anchor.seconds) * 1000.0))ms of picture buffered"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)

        telemetry.mutate { $0.isAudioMasterClockActive = false }
    }

    /// Records a fault the pipeline recognised in itself.
    ///
    /// Kept apart from the error counters, which measure parts. These name the
    /// shape: a stream can decode at full rate with every counter at zero and
    /// still not be playing, and until now nothing said so.
    public func reportWarning(_ text: String) {
        var isNew = false
        telemetry.mutate {
            if !$0.pipelineWarnings.contains(text) {
                $0.pipelineWarnings.append(text)
                isNew = true
            }
        }
        guard isNew else { return }
        let msg = "[1080i50-WATCHDOG] ⚠️ \(text)"
        print(msg)
        logger.error("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    /// A picture on screen that never starts moving.
    ///
    /// What is left of the freeze once the picture can drive the clock itself: a
    /// channel that names a decodable audio track and then never delivers it.
    /// That case is deliberately not acted on — starting the clock on a stream
    /// that may yet produce sound puts every later buffer in the past, where the
    /// renderer discards it — but it is exactly what a viewer reads as a hang,
    /// and saying so costs nothing.
    private func warnIfMotionNeverStarted() {
        guard !isAudioClockStarted else { return }
        let elapsed = sessionState.mutate { state -> Double in
            state.firstVideoFieldTime > 0 ? CACurrentMediaTime() - state.firstVideoFieldTime : 0
        }
        guard elapsed >= Self.motionWatchdogSeconds else { return }
        let pid = selectedAudioPID.map(String.init) ?? "none selected"
        reportWarning("Picture has been on screen \(String(format: "%.0f", elapsed))s without moving — the clock is still waiting for audio (PID \(pid)).")
    }

    /// Tells the PiP window that playback state changed.
    ///
    /// AVKit caches what the delegate told it and only re-reads when invalidated,
    /// so without this the PiP controls keep the state they were built with.
    public func notePlaybackStateChanged() {
        // The context is captured, not `self`. This runs from `stopStreaming`, which
        // `deinit` calls, and forming a weak reference to an object that is already
        // deallocating is a crash rather than a nil. The context still needs to know
        // which session is asking, so it is handed an unowned-unsafe reference that is
        // only ever compared for identity, never dereferenced.
        let context = presentationContext
        guard let context else { return }
        let asking = unsafeBitCast(self, to: UInt.self)
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                context.notePlaybackStateChanged(fromSessionIdentity: asking)
            }
        }
    }

    public func stopStreaming() {
        streamTask?.cancel()
        notePlaybackStateChanged()
        streamTask = nil
        urlSession?.invalidateAndCancel()
        urlSession = nil

        stopSystemMonitoring()
        removeFirstPictureObserver()

        audioRenderer.reset()
        isAudioClockStarted = false
        audioBuffersPreRolledCount = 0
        firstAudioPTS = nil
        firstVideoFieldPTS = nil
        firstDecodedPicturePTS = nil
        commitAnchor = nil
        latestVideoPTS = .invalid
        preRollStartTime = 0
        audioContinuity.reset()
        selectedAudioPID = nil
        availableAudioTracks.removeAll()

        zapLock.lock()
        let currentZap = currentZapId
        zapLock.unlock()

        // The parse chain is owned by `ingestQueue`; resetting it from here while
        // a feed is in flight would corrupt the assembler state mid-packet.
        ingestQueue.sync {
            decodeGateState = .closed(reason: .startup)
            gatedAccessUnitCount = 0
            firstAccessUnitTime = 0
            lastSyncPTS = .invalid
            syncCount = 0
            framesSinceLastSync = 0
            tsParser.reset()
            pesAssembler.reset()
            accessUnitAssembler.reset()
            alternateAssembler?.reset()
            alternateAssembler = nil
            decoder.reset(generation: currentZap)
            audioPesAssembler.reset()
            ac3FrameParser.reset()
            aacFrameParser.reset()
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
        let received = task.countOfBytesReceived
        let elapsed = sessionState.mutate { state -> Double in
            state.requestStartTime > 0 ? (CACurrentMediaTime() - state.requestStartTime) : 0
        }

        if let error = error as NSError? {
            if error.code == NSURLErrorCancelled {
                let msg = "[1080i50-NET] Stream cancelled after \(String(format: "%.1f", elapsed))s | Received: \(received / 1024) KiB"
                print(msg)
                logger.notice("\(msg, privacy: .public)")
                TelemetryServer.shared.log(msg)
            } else {
                let rating = "❌ Connection Error: \(error.localizedDescription)"
                telemetry.mutate { $0.ttfpRating = rating }
                let msg = "[1080i50-NET] ❌ Stream failed after \(String(format: "%.1f", elapsed))s | \(error.domain) \(error.code): \(error.localizedDescription) | Received: \(received / 1024) KiB"
                print(msg)
                logger.error("\(msg, privacy: .public)")
                TelemetryServer.shared.log(msg)
            }
            return
        }

        // A live stream has no natural end. Reaching here means the server closed
        // the socket, which the app otherwise only notices as video stopping.
        let msg = "[1080i50-NET] ⚠️ Server closed stream after \(String(format: "%.1f", elapsed))s | Received: \(received / 1024) KiB"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    /// Breaks the connection setup cost down into its phases.
    ///
    /// `ttfpNetworkMs` measures request-to-first-byte as one opaque number — 769 ms
    /// on the first measured run, well over a third of the whole TTFP budget, with
    /// nothing to say where it went. These metrics separate DNS from TCP from the
    /// server's own think time, which are three different problems with three
    /// different fixes.
    public func urlSession(_ session: URLSession, task: URLSessionTask, didFinishCollecting metrics: URLSessionTaskMetrics) {
        for transaction in metrics.transactionMetrics {
            func ms(_ from: Date?, _ to: Date?) -> String {
                guard let from = from, let to = to else { return "—" }
                return String(format: "%.1f", to.timeIntervalSince(from) * 1000.0)
            }

            let dns = ms(transaction.domainLookupStartDate, transaction.domainLookupEndDate)
            let tcp = ms(transaction.connectStartDate, transaction.connectEndDate)
            let tls = ms(transaction.secureConnectionStartDate, transaction.secureConnectionEndDate)
            let request = ms(transaction.requestStartDate, transaction.requestEndDate)
            let serverThink = ms(transaction.requestEndDate, transaction.responseStartDate)
            let total = ms(transaction.fetchStartDate, transaction.responseStartDate)

            let netLog = "[1080i50-NET] Setup: \(total)ms total | DNS: \(dns)ms | TCP: \(tcp)ms | TLS: \(tls)ms | Request: \(request)ms | ServerThink: \(serverThink)ms | Proto: \(transaction.networkProtocolName ?? "?") | Reused: \(transaction.isReusedConnection) | Cellular: \(transaction.isCellular) | Host: \(transaction.request.url?.host ?? "?")"
            print(netLog)
            logger.notice("\(netLog, privacy: .public)")
            TelemetryServer.shared.log(netLog)
        }
    }

    public func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        let (netMs, stallMs) = sessionState.mutate { state -> (Double?, Double?) in
            let now = CACurrentMediaTime()

            var firstByteMs: Double?
            if state.firstDataTime == 0 && state.requestStartTime > 0 {
                state.firstDataTime = now
                // From when the request left, not from when the app was asked to
                // tune. Everything before it is ours and is reported as setup.
                let base = state.requestIssuedTime > 0 ? state.requestIssuedTime : state.requestStartTime
                firstByteMs = (now - base) * 1000.0
            }

            var gapMs: Double?
            if state.lastDataArrival > 0 {
                let gap = (now - state.lastDataArrival) * 1000.0
                if gap >= 250.0 {
                    state.stallCount += 1
                    state.longestStallMs = max(state.longestStallMs, gap)
                    if gap >= 600.0 {
                        gapMs = gap
                    }
                }
            }
            state.lastDataArrival = now

            return (firstByteMs, gapMs)
        }

        if let netMs = netMs {
            telemetry.mutate { $0.ttfpNetworkMs = netMs }
        }

        if let stallMs = stallMs {
            ChannelJitterProfiler.shared.recordStall(for: currentChannelKey, stallMs: stallMs)
            let msg = "[1080i50-NET] ⚠️ Socket stall: \(String(format: "%.0f", stallMs))ms with no data"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)
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
            let (stalls, longestStall) = sessionState.mutate { state -> (Int, Double) in
                (state.stallCount, state.longestStallMs)
            }

            // The audio cushion, reported alongside the network figures that erode
            // it — a stall and the dropout it causes belong on the same line.
            let audio = audioRenderer.consumeFlowStats()
            telemetry.mutate {
                $0.audioUnderruns = audio.underruns
                $0.audioLeadMs = audio.currentLeadMs
                $0.audioMinLeadMs = audio.minLeadMs
            }

            let qualityLog = "[1080i50-QUALITY] Bitrate: \(String(format: "%.1f", kbps)) kbps | VideoPID: \(snapshot.videoPID) | ContinuityErr: \(snapshot.continuityErrors) | PESErr: \(snapshot.pesErrors) | Scrambled: \(snapshot.scrambledPackets) (V \(snapshot.scrambledVideoPackets) / A \(snapshot.scrambledAudioPackets), clear run \(snapshot.videoClearRun)) | DecErrors: \(snapshot.decodeErrors) | Backlog: \(backlog / 1024) KiB | Stalls: \(stalls) (worst \(String(format: "%.0f", longestStall))ms) | AudioLead: \(String(format: "%.0f", audio.currentLeadMs))ms (min \(String(format: "%.0f", audio.minLeadMs))ms) | Underruns: \(audio.underruns) | AudioQueue: \(audio.pendingBuffers)"
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
            self.pendingIngestBytes = max(0, self.pendingIngestBytes - data.count)
            let isStale = generation != self.ingestGeneration
            self.ingestStateLock.unlock()

            guard !isStale else { return }
            self.tsParser.feed(data: data)
        }
    }

    // MARK: - TSPacketParserDelegate

    public func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        let psiMs = sessionState.mutate { state -> Double? in
            if state.psiParsedTime == 0 {
                state.psiParsedTime = CACurrentMediaTime()
                let base = state.firstDataTime > 0 ? state.firstDataTime : state.requestStartTime
                return (state.psiParsedTime - base) * 1000.0
            }
            return nil
        }
        if let psiMs = psiMs {
            telemetry.mutate { $0.ttfpPsiMs = psiMs }
        }

        telemetry.mutate { $0.videoPID = pid }
    }

    /// Names what DVB flagged the track as, for logs that have to be read by a
    /// person deciding whether the right one was chosen.
    static func audioTypeLabel(for audioType: UInt8) -> String {
        switch audioType {
        case 0x01: return ", clean effects"
        case 0x02: return ", hearing impaired"
        case 0x03: return ", audio description"
        default: return ""
        }
    }

    /// Which of a channel's audio tracks to play.
    ///
    /// Three rules, in this order, and the order is the whole point.
    ///
    /// **Decodable before anything else.** A language nobody can hear is not a
    /// preference worth honouring. ZDF carries MPEG Layer II and AC-3 both
    /// tagged `deu`, with Layer II first in the PMT; iOS has no Layer II
    /// decoder, so preferring by language alone produced a stream that played
    /// perfectly, in silence, with nothing in any log to say why.
    ///
    /// **Then the main programme, not an accessibility mix.** DVB flags these
    /// in the ISO 639 descriptor and it is the only thing in the stream that
    /// tells them apart: `0x03` is visual-impaired commentary — a narrator
    /// talking over the programme — and `0x02` is a hearing-impaired mix. Both
    /// are ordinary decodable AC-3 and both may carry the main track's language
    /// code, so every rule that ignores this field is choosing between them by
    /// accident. One measured channel offered `qae` and `qaf`, both AC-3,
    /// neither `deu`, and the pick fell to whichever the PMT listed first.
    ///
    /// **Then language, then codec.** Only once the pool is down to things that
    /// can be heard and are meant to be listened to.
    ///
    /// Each narrowing is skipped when it would empty the pool: a narrated
    /// soundtrack beats silence, and an undecodable track still selected keeps
    /// the rest of the pipeline reporting normally instead of losing the PID.
    static func preferredAudioTrack(from tracks: [AudioTrackInfo]) -> AudioTrackInfo? {
        guard !tracks.isEmpty else { return nil }

        let decodable = tracks.filter { $0.codec.isDecodableOnDevice }
        let pool = decodable.isEmpty ? tracks : decodable

        let mainProgramme = pool.filter { $0.audioType != 0x02 && $0.audioType != 0x03 }
        let ranked = mainProgramme.isEmpty ? pool : mainProgramme

        if let deu = ranked.first(where: { $0.language == "deu" }) {
            return deu
        }
        if let ac3 = ranked.first(where: { $0.codec == .ac3 || $0.codec == .eac3 }) {
            return ac3
        }
        return ranked.first
    }

    public func tsParser(_ parser: TSPacketParser, didDetermineVideoCodec codec: VideoStreamCodec) {
        let playable = codec.isDecodableOnDevice
        telemetry.mutate {
            $0.codec = codec.description
            $0.unplayableVideoCodec = playable ? nil : codec.viewerDescription
        }

        // The codec decides which assembler cuts pictures out of the stream.
        // H.264 keeps its own path; the others get theirs and route through the
        // shared delegate.
        switch codec {
        case .h264, .mpeg1, .unknown:
            alternateAssembler = nil
        case .mpeg2:
            let assembler = MPEG2AccessUnitAssembler()
            assembler.assemblerDelegate = self
            alternateAssembler = assembler
        case .hevc:
            let assembler = HEVCAccessUnitAssembler()
            assembler.assemblerDelegate = self
            alternateAssembler = assembler
        }

        let zapId = currentZapId
        let msg = playable
            ? "[ZAP-#\(zapId)-PMT] 🎬 Video codec: \(codec) (PID: \(parser.videoPID ?? 0))"
            : "[ZAP-#\(zapId)-PMT] ⛔️ Video codec \(codec) cannot be assembled here — no picture will follow"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    public func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        self.availableAudioTracks = tracks
        sessionState.mutate { state in
            state.audioTracksKnown = true
            state.hasDecodableAudio = tracks.contains { $0.codec.isDecodableOnDevice }
        }
        let zapId = currentZapId
        let trackListLog = tracks.map { "PID \($0.pid) [\($0.codec), \($0.language ?? "und")\(Self.audioTypeLabel(for: $0.audioType))]" }.joined(separator: ", ")
        let logMsg = "[ZAP-#\(zapId)-PMT] 🎵 Discovered \(tracks.count) audio tracks: \(trackListLog)"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)

        // Track selection: playable first, then language.
        //
        // Re-selected when the current choice is gone, and also when it is one that
        // cannot be decoded and something that can has since appeared. Audio layouts
        // are not fixed for the life of a channel: broadcasters signal a change with
        // a PMT version bump, and several services here carry MPEG Layer II during
        // the day and add a Dolby track only for a film or a match. Holding the
        // undecodable track because its PID is still listed kept such a channel
        // silent for the whole programme while the sound it needed was on the wire.
        let currentTrack = tracks.first(where: { $0.pid == selectedAudioPID })
        let heldTrackCannotBeDecoded = currentTrack.map { !$0.codec.isDecodableOnDevice } ?? false
        let somethingDecodableExists = tracks.contains { $0.codec.isDecodableOnDevice }

        if selectedAudioPID == nil || currentTrack == nil
            || (heldTrackCannotBeDecoded && somethingDecodableExists) {
            let playable = tracks.filter { $0.codec.isDecodableOnDevice }

            if playable.isEmpty && !tracks.isEmpty {
                let codecs = tracks.map { $0.codec.description }.joined(separator: ", ")
                let warn = "[ZAP-#\(zapId)-AUDIO] ⚠️ No decodable audio track on this channel (offered: \(codecs)) — playback will be silent"
                print(warn)
                logger.error("\(warn, privacy: .public)")
                TelemetryServer.shared.log(warn)
            }

            let preferred = Self.preferredAudioTrack(from: tracks)

            if let track = preferred {
                self.selectedAudioPID = track.pid
                self.selectedAudioCodec = track.codec
                let selLog = "[ZAP-#\(zapId)-AUDIO] 🎯 Selected audio track: PID \(track.pid) (\(track.codec), lang: \(track.language ?? "und")\(Self.audioTypeLabel(for: track.audioType)))"
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

    /// Selects an audio track by PID dynamically during live playback without interrupting the video stream.
    ///
    /// Thread-safe: dispatches to `ingestQueue` to ensure parser and demux state are mutated atomically
    /// without racing against incoming network packets. Flushes queued buffers of the old track from the renderer.
    public func selectAudioTrack(pid: UInt16) {
        guard let track = availableAudioTracks.first(where: { $0.pid == pid }) else { return }

        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            guard self.selectedAudioPID != pid else { return }

            self.selectedAudioPID = track.pid
            self.selectedAudioCodec = track.codec

            // 1. Reset all audio assemblers & parsers to discard partial old track frames
            self.audioPesAssembler.reset()
            self.aacFrameParser.reset()
            self.ac3FrameParser.reset()
            self.audioSampleBufferAssembler.reset()

            // 2. Flush stale samples of previous track from audio renderer
            self.audioRenderer.flush()

            // 3. Update telemetry with new track parameters
            self.telemetry.mutate {
                $0.audioPID = track.pid
                $0.audioCodec = track.codec.description
                $0.audioLanguage = track.language ?? "und"
            }

            let zapId = self.currentZapId
            let logMsg = "[ZAP-#\(zapId)-AUDIO] 🔀 User switched audio track to: PID \(track.pid) (\(track.displayName))"
            print(logMsg)
            logger.notice("\(logMsg, privacy: .public)")
            TelemetryServer.shared.log(logMsg)
        }
    }

    /// A channel can be silent because nothing in its PMT could be classified. That
    /// case produces no track and therefore no selection, so without this it left no
    /// trace at all - the failure looked identical to a channel that simply has no
    /// sound. Logged, never selected: choosing a stream whose codec is unknown would
    /// be guessing.
    public func tsParser(_ parser: TSPacketParser, didObserveUnclassifiedStreams streams: [UnclassifiedStreamInfo]) {
        let zapId = currentZapId
        let detail = streams.map {
            let tags = $0.descriptorTags.map { String(format: "0x%02X", $0) }.joined(separator: "+")
            return "PID \($0.pid) type 0x\(String(format: "%02X", $0.streamType)) [\(tags.isEmpty ? "no descriptors" : tags), \($0.language ?? "und")]"
        }.joined(separator: ", ")

        let hasTrack = !availableAudioTracks.isEmpty
        let msg = hasTrack
            ? "[ZAP-#\(zapId)-PMT] ℹ️ \(streams.count) unclassified stream(s) alongside the selected audio: \(detail)"
            : "[ZAP-#\(zapId)-AUDIO] ⚠️ No audio track could be classified — the PMT names \(streams.count) stream(s) this build cannot identify: \(detail) — playback will be silent"
        print(msg)
        if hasTrack {
            logger.notice("\(msg, privacy: .public)")
        } else {
            logger.error("\(msg, privacy: .public)")
        }
        TelemetryServer.shared.log(msg)

        telemetry.mutate { $0.unclassifiedAudioStreams = streams.count }
    }

    public func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        if let vPid = tsParser.videoPID, pid == vPid {
            // Reaching here means the packet was clear: the parser drops scrambled
            // payload before delivery. The run is what says the stream is clear now,
            // as opposed to having been clear at some point.
            telemetry.mutate { $0.videoClearRun += 1 }
            pesAssembler.feed(payload: data, unitStart: unitStart)
        } else if let aPid = selectedAudioPID, pid == aPid {
            audioPesAssembler.feed(payload: data, pid: pid, unitStart: unitStart)
        }
    }

    public func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        telemetry.mutate { $0.continuityErrors += 1 }
    }

    public func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16) {
        let isVideo = pid == parser.videoPID
        telemetry.mutate {
            $0.scrambledPackets += 1
            if isVideo {
                $0.scrambledVideoPackets += 1
                $0.videoClearRun = 0
            } else {
                $0.scrambledAudioPackets += 1
            }
        }
    }

    // MARK: - AudioPESAssemblerDelegate

    public func audioPESAssembler(_ assembler: AudioPESAssembler, didEmitAudioPES payload: AudioPESData) {
        if selectedAudioCodec == .aac {
            aacFrameParser.feed(data: payload.payload, pts: payload.pts, pts90k: payload.pts90k)
        } else {
            ac3FrameParser.feed(data: payload.payload, pts: payload.pts, pts90k: payload.pts90k)
        }
    }

    public func audioPESAssembler(_ assembler: AudioPESAssembler, didEncounterPESError reason: String, onPID pid: UInt16) {
        telemetry.mutate { $0.pesErrors += 1 }
    }

    // MARK: - AudioSampleBufferAssemblerDelegate

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int) {
        let logMsg = "[1080i50-AUDIO] Format: \(codec) | \(sampleRate) Hz | \(channels) ch | \(bitrateKbps > 0 ? "\(bitrateKbps) kbps" : "VBR")"
        print(logMsg)
        logger.notice("\(logMsg, privacy: .public)")
        TelemetryServer.shared.log(logMsg)

        telemetry.mutate {
            $0.audioCodec = codec.description
            $0.audioSampleRate = sampleRate
            $0.audioChannels = channels
            $0.audioBitrateKbps = bitrateKbps
        }
    }

    private var firstAudioPTS: CMTime?

    /// Presentation timestamp of the first video field the renderer produced.
    ///
    /// The clock is anchored here rather than on the first audio timestamp. Audio
    /// is decodable from the first PES; video is not decodable until parameter
    /// sets and a sync sample have arrived, which measured 1.4 s on a cold tune.
    /// Everything in between is audio for pictures that cannot be shown, and
    /// anchoring at the start of it made the clock begin that far behind the
    /// first picture — measured at 2657 ms of a 5298 ms tune, spent playing sound
    /// over a black screen.
    ///
    /// It does not end there. The offset never closes: the clock keeps running
    /// that far behind the stream, so every field the renderer produces is
    /// seconds early, `AVSampleBufferDisplayLayer` stays permanently full, and
    /// the presenter's queue sheds what it cannot hand over — 4793 fields
    /// dropped against 397 delivered in one measured run, which is what the
    /// stutter was.
    ///
    /// Written from the main actor by the render view's callback, read on the
    /// ingest queue by the pre-roll below.
    private var _firstVideoFieldPTS: CMTime?
    private let firstVideoFieldLock = NSLock()

    private var firstVideoFieldPTS: CMTime? {
        get {
            firstVideoFieldLock.lock()
            defer { firstVideoFieldLock.unlock() }
            return _firstVideoFieldPTS
        }
        set {
            firstVideoFieldLock.lock()
            _firstVideoFieldPTS = newValue
            firstVideoFieldLock.unlock()
        }
    }

    /// Newest decoded picture timestamp, for measuring the video cushion.
    ///
    /// Written on the VideoToolbox callback thread, read on the ingest queue.
    private var _latestVideoPTS: CMTime = .invalid
    private var latestVideoPTS: CMTime {
        get {
            firstVideoFieldLock.lock()
            defer { firstVideoFieldLock.unlock() }
            return _latestVideoPTS
        }
        set {
            firstVideoFieldLock.lock()
            _latestVideoPTS = newValue
            firstVideoFieldLock.unlock()
        }
    }

    /// Video buffered ahead of the clock before playback starts.
    ///
    /// The cushion has to be measured on video, because video is what visibly
    /// stalls. Anchoring the clock on the first picture — which is what removed
    /// 3.2 s of black from tune-in — also removed every millisecond of lead, and
    /// a source that pauses for up to 828 ms between writes then freezes the
    /// picture on each pause. Measured on this box: 28 gaps over 250 ms in 15
    /// seconds, worst 824 ms, against a median gap of 1.7 ms.
    ///
    /// One second covers the worst measured gap with room to spare and costs
    /// that much tuning latency, against the 3.2 s it replaces.
    private static let videoPreRollSeconds: Double = 1.0

    /// When the pre-roll began, so the wait for a first picture can be bounded.
    private var preRollStartTime: CFTimeInterval = 0

    /// How long the clock waits for a picture on a service that advertises video
    /// before giving up and starting on audio alone.
    ///
    /// Only reached when the PMT names a video PID and nothing decodable comes of
    /// it — a codec the decoder refuses, or video that never arrives. A service
    /// with no video PID at all does not wait at all; it has nothing to wait for.
    /// Long enough that an ordinary tune — 1.4 s to parameter sets on the
    /// measured source — reaches its first picture well inside it.
    private static let videoAnchorTimeout: Double = 2.5

    /// Watches the master timeline for jumps once it is running.
    ///
    /// Only the audio side needs to decide this. It owns the clock, so a jump
    /// there is a jump in what everything else is scheduled against; the video
    /// side follows whatever the clock does.
    private var audioContinuity = PTSContinuityMonitor()

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime) {
        guard !isAudioInterrupted else { return }
        let pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer)

        // Checked before the buffer is handed over, so the flush that follows
        // cannot drop the first buffer of the new timeline along with the old
        // ones. The monitor runs during pre-roll too — it has to, or the first
        // timestamp after the clock starts would have nothing to compare against.
        // Handled whether or not the clock is running. It used to be conditional on a
        // started clock, which meant a session being prepared ignored the jump entirely
        // and kept the first timestamp of a timeline that no longer exists - so its
        // start anchor was computed against audio it would never receive again, and it
        // could never become presentable. A preparing session is exactly the one that
        // must survive its source retuning underneath it.
        if let delta = audioContinuity.jump(for: pts) {
            handleAudioTimelineJump(to: pts, delta: delta, codec: codec)
        }

        if isAudioClockStarted {
            audioRenderer.enqueue(sampleBuffer: sampleBuffer)
            return
        }

        audioRenderer.enqueue(sampleBuffer: sampleBuffer)
        audioBuffersPreRolledCount += 1

        guard pts.isValid else { return }

        if !isAudioClockStarted {
            if let first = firstAudioPTS {
                // If a stray packet arrived at t0 with a radically different timestamp (> 2.0s jump), reset
                if abs(pts.seconds - first.seconds) > 2.0 {
                    firstAudioPTS = pts
                    audioBuffersPreRolledCount = 1
                    audioRenderer.flush()
                    return
                }
            } else {
                firstAudioPTS = pts
                let zapId = currentZapId
                let audioLog = "[ZAP-#\(zapId)-AUDIO] 🎵 First audio sample enqueued at PTS \(String(format: "%.3f", pts.seconds))s (\(selectedAudioCodec.description))"
                print(audioLog)
                logger.notice("\(audioLog, privacy: .public)")
                TelemetryServer.shared.log(audioLog)
            }

            guard let firstPTS = firstAudioPTS else { return }
            if preRollStartTime == 0 { preRollStartTime = CACurrentMediaTime() }

            var requiresCushion = true
            var cushionSource = "audio"

            let anchorPTS: CMTime
            let anchorSource: String
            // The surface's report of a submitted field when there is one, and this
            // session's own first decoded picture otherwise. They name the same picture;
            // only the first requires owning the screen, and a session being prepared
            // never gets it - so anchoring on it alone meant a preparation could not
            // choose a start anchor at all and was never committable.
            if let videoPTS = firstVideoFieldPTS ?? firstDecodedPicturePTS {
                let effectiveVideoPreRoll = Self.enableEarlyMotionExperiment ? 0.20 : Self.videoPreRollSeconds

                let effectiveAudioPreRoll: Double
                if Self.enableEarlyMotionExperiment {
                    let (profilePreRoll, reason) = ChannelJitterProfiler.shared.recommendedAudioPreRoll(for: currentChannelKey)
                    let longestStallMs = sessionState.mutate { $0.longestStallMs }
                    let observedStallCushion = longestStallMs > 0 ? (longestStallMs / 1000.0) + 0.15 : 0.35
                    let isSmoothed = currentChannelKey.contains("/stream/live") || currentChannelKey.contains("/stream/smooth")
                    effectiveAudioPreRoll = isSmoothed ? profilePreRoll : max(profilePreRoll, observedStallCushion)
                    cushionSource = "adaptive-learned(\(String(format: "%.0f", effectiveAudioPreRoll * 1000))ms | \(reason))"
                } else {
                    effectiveAudioPreRoll = Self.audioPreRollSeconds
                    cushionSource = "video+audio"
                }

                let audioCeiling = pts.seconds - effectiveAudioPreRoll
                let anchorSeconds = min(videoPTS.seconds, audioCeiling)

                // Anchoring before the first audio we hold would start the clock
                // in a region no track can serve.
                guard anchorSeconds >= firstPTS.seconds else {
                    noteAnchorRejected(
                        reason: "anchor precedes the first audio held",
                        anchorSeconds: anchorSeconds,
                        firstAudio: firstPTS.seconds,
                        cushion: effectiveAudioPreRoll
                    )
                    return
                }

                // And enough video past the anchor to survive a source pause;
                // this box writes in bursts with gaps up to 824 ms.
                let videoBuffered = latestVideoPTS.isValid
                    ? latestVideoPTS.seconds - anchorSeconds
                    : 0
                guard videoBuffered >= effectiveVideoPreRoll else {
                    noteAnchorRejected(
                        reason: "only \(String(format: "%.2f", videoBuffered))s of picture past the anchor, \(String(format: "%.2f", effectiveVideoPreRoll))s required",
                        anchorSeconds: anchorSeconds,
                        firstAudio: firstPTS.seconds,
                        cushion: effectiveAudioPreRoll
                    )
                    return
                }

                anchorPTS = CMTime(seconds: anchorSeconds, preferredTimescale: 90_000)
                anchorSource = anchorSeconds >= videoPTS.seconds - 0.001
                    ? "first picture"
                    : "audio ceiling (picture \(String(format: "%.0f", (videoPTS.seconds - anchorSeconds) * 1000))ms ahead)"
                requiresCushion = false
            } else if tsParser.videoPID == nil {
                anchorPTS = firstPTS
                anchorSource = "audio only, no video service"
            } else if CACurrentMediaTime() - preRollStartTime >= Self.videoAnchorTimeout {
                anchorPTS = firstPTS
                anchorSource = "no picture within \(String(format: "%.1f", Self.videoAnchorTimeout))s"
            } else {
                return
            }

            let effectiveAudioPreRoll: Double
            if Self.enableEarlyMotionExperiment {
                let (profilePreRoll, _) = ChannelJitterProfiler.shared.recommendedAudioPreRoll(for: currentChannelKey)
                let longestStallMs = sessionState.mutate { $0.longestStallMs }
                let observedStallCushion = longestStallMs > 0 ? (longestStallMs / 1000.0) + 0.15 : 0.35
                let isSmoothed = currentChannelKey.contains("/stream/live") || currentChannelKey.contains("/stream/smooth")
                effectiveAudioPreRoll = isSmoothed ? profilePreRoll : max(profilePreRoll, observedStallCushion)
            } else {
                effectiveAudioPreRoll = Self.audioPreRollSeconds
            }
            // Recorded as soon as the anchor is final, whether or not the clock starts
            // here. It is what a commit needs: the instant picture and audio share, so
            // the commit itself has nothing left to decide.
            commitAnchor = anchorPTS
            completeRecoveryIfNeeded()

            let buffered = pts.seconds - anchorPTS.seconds
            if !requiresCushion || buffered >= effectiveAudioPreRoll {
                let zapId = currentZapId
                if Self.enableEarlyMotionExperiment {
                    let pruneResult = self.audioRenderer.pruneBuffersBefore(time: anchorPTS)
                    let lastPrunedStr = pruneResult.lastPrunedPTS.map { String(format: "%.3f", $0.seconds) } ?? "none"
                    let firstKeptStr = pruneResult.firstKeptPTS.map { String(format: "%.3f", $0.seconds) } ?? "none"
                    let pruneLog = "[EARLY-EXP] ✂️ Zap #\(zapId) Anchor: \(String(format: "%.3f", anchorPTS.seconds))s | Pruned: \(pruneResult.prunedCount) buffers (last pruned: \(lastPrunedStr)s) | First kept: \(firstKeptStr)s | Remaining audio lead: \(String(format: "%.0f", pruneResult.remainingLeadMs))ms"
                    print(pruneLog)
                    logger.notice("\(pruneLog, privacy: .public)")
                    TelemetryServer.shared.log(pruneLog)

                    self.scheduleEarlyMotionMilestones(zapId: zapId, anchorPTS: anchorPTS)
                }

                // Only the session that owns the surface starts a clock. A prepared one
                // records its anchor here and is started by the commit instead, which is
                // what keeps it silent and invisible until then.
                guard surfaceOutlet?.owns(presentationGeneration) == true else { return }
                audioRenderer.setAudible(true)
                audioRenderer.setRate(1.0, time: anchorPTS)
                isAudioClockStarted = true
                // Paused until this instant, as far as PiP is concerned.
                notePlaybackStateChanged()
                let skippedMs = (anchorPTS.seconds - firstPTS.seconds) * 1000.0
                let videoCushionMs = latestVideoPTS.isValid
                    ? (latestVideoPTS.seconds - anchorPTS.seconds) * 1000.0
                    : Double.nan
                let clockLog = "[ZAP-#\(zapId)-LOCK] ⏱️ Master clock started at PTS: \(String(format: "%.3f", anchorPTS.seconds))s via \(anchorSource) (\(codec), gated on \(cushionSource) cushion | video ahead: \(String(format: "%.0f", videoCushionMs))ms | audio ahead: \(String(format: "%.0f", buffered * 1000))ms | skipped \(String(format: "%.0f", skippedMs))ms of pre-picture audio)"
                print(clockLog)
                logger.notice("\(clockLog, privacy: .public)")
                TelemetryServer.shared.log(clockLog)

                // How long the viewer spent looking at a still picture. Nothing
                // measured this: the figure that used to mean "motion" now means
                // "the first field went up", and the distance between them is
                // exactly what the cushion costs.
                let motionMs = sessionState.mutate { state -> Double? in
                    guard state.requestStartTime > 0 else { return nil }
                    return (CACurrentMediaTime() - state.requestStartTime) * 1000.0
                }

                telemetry.mutate {
                    $0.isAudioMasterClockActive = true
                    if let motionMs = motionMs, $0.ttfpMotionMs == 0 {
                        $0.ttfpMotionMs = motionMs
                    }
                }

                if let motionMs = motionMs {
                    // The clock can be ready before the first field exists, in
                    // which case the picture arrives already moving and there is
                    // no still frame to account for.
                    let visibleMs = telemetry.snapshot().ttfpVisibleMs
                    let held = visibleMs > 0
                        ? "picture held still for \(String(format: "%.1f", motionMs - visibleMs))ms waiting for the audio cushion"
                        : "clock was ready before the first field, nothing held"
                    let motionLog = "[1080i50-TTFP] ▶️ Motion starts at \(String(format: "%.1f", motionMs))ms | \(held)"
                    print(motionLog)
                    logger.notice("\(motionLog, privacy: .public)")
                    TelemetryServer.shared.log(motionLog)
                }

                // Anchoring on the first picture means the clock *starts* at its
                // timestamp, and a boundary observer needs a crossing to fire —
                // so the metric it feeds stayed at zero for exactly the tunes
                // this is meant to measure. Due at rate-change time counts as
                // visible.
                if let videoPTS = firstVideoFieldPTS, anchorPTS >= videoPTS {
                    removeFirstPictureObserver()
                    recordFirstPictureVisible()
                }
            }
        }
    }

    private func scheduleEarlyMotionMilestones(zapId: Int, anchorPTS: CMTime) {
        let milestones: [Double] = [0.5, 1.0, 3.0, 5.0]
        for delay in milestones {
            DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [weak self] in
                guard let self = self, self.currentZapId == zapId, self.isAudioClockStarted else { return }
                let stats = self.audioRenderer.consumeFlowStats()
                let presenterQ = 0 // see above
                let status = self.audioRenderer.status
                let statusStr = status == .rendering ? "rendering" : (status == .failed ? "FAILED" : "other")
                let lead = stats.currentLeadMs
                let minLead = stats.minLeadMs
                let underruns = stats.underruns

                let isFailure = (minLead < 150.0 && minLead > 0) || underruns > 0 || presenterQ > 130 || status == .failed
                let tag = isFailure ? "❌ EXPERIMENT ALERT" : "📈 Milestone"
                let log = "[EARLY-EXP] \(tag) +\(String(format: "%.1f", delay))s: AudioLead=\(String(format: "%.0f", lead))ms (min=\(String(format: "%.0f", minLead))ms), Underruns=\(underruns), PresenterQ=\(presenterQ), Status=\(statusStr)"
                print(log)
                TelemetryServer.shared.log(log)
            }
        }
    }

    /// Re-anchors everything onto the timeline that just started.
    ///
    /// Deliberately the same sequence a channel zap uses, because it is the same
    /// situation: every buffer in flight, every queued field and the clock they
    /// were all scheduled against belong to a timeline that no longer exists.
    /// The clock is parked on the new timestamp rather than stopped outright, so
    /// the pre-roll path below restarts it exactly as it does on a cold tune —
    /// one way to start the clock, not two.
    private func handleAudioTimelineJump(to pts: CMTime, delta: Double, codec: AudioStreamCodec) {
        // Marked before anything is torn down. Everything anchored to the timeline that
        // just ended is about to be discarded, and until a new anchor exists this
        // session has nothing that could be committed.
        beginRecovery("timeline jumped \(String(format: "%+.3f", delta))s")

        telemetry.mutate {
            $0.ptsDiscontinuities += 1
            $0.isAudioMasterClockActive = false
        }

        audioRenderer.flush()
        audioRenderer.setRate(0.0, time: pts)

        isAudioClockStarted = false
        firstAudioPTS = pts
        audioBuffersPreRolledCount = 0
        // The recorded field belongs to the timeline that just ended. Clearing it
        // makes the re-anchor wait for a picture from the new one, exactly as a
        // cold tune does; `resetForChannelZap` below re-arms the callback.
        firstVideoFieldPTS = nil
        // Cleared with it, for the same reason: a picture timestamp kept across a
        // re-anchor pins the start anchor to an instant the new timeline never
        // reaches, and the session can then never anchor again.
        firstDecodedPicturePTS = nil
        commitAnchor = nil
        preRollStartTime = 0

        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.presentationContext?.requestReset(from: self)
        }

        let msg = "[1080i50-CLOCK] ⚠️ PTS discontinuity: timeline jumped \(String(format: "%+.3f", delta))s to \(String(format: "%.3f", pts.seconds))s (\(codec)) — flushed and re-anchoring"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    public func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String) {
        telemetry.mutate { $0.decodeErrors += 1 }
    }

    // MARK: - NativeTSAudioRendererDelegate & Audio Interruption Recovery

    private func setupAudioNotificationObservers() {
        if audioInterruptionObserver == nil {
            audioInterruptionObserver = NotificationCenter.default.addObserver(
                forName: AVAudioSession.interruptionNotification,
                object: nil,
                queue: nil
            ) { [weak self] notif in
                self?.handleAudioInterruption(notif)
            }
        }
        if audioRouteChangeObserver == nil {
            audioRouteChangeObserver = NotificationCenter.default.addObserver(
                forName: AVAudioSession.routeChangeNotification,
                object: nil,
                queue: nil
            ) { [weak self] notif in
                self?.handleAudioRouteChange(notif)
            }
        }
        if audioFlushedObserver == nil {
            audioFlushedObserver = NotificationCenter.default.addObserver(
                forName: .AVSampleBufferAudioRendererWasFlushedAutomatically,
                object: nil,
                queue: nil
            ) { [weak self] notif in
                self?.handleAudioRendererWasFlushedAutomatically(notif)
            }
        }
    }

    private func handleAudioInterruption(_ notification: Notification) {
        guard let userInfo = notification.userInfo,
              let typeValue = userInfo[AVAudioSessionInterruptionTypeKey] as? UInt,
              let type = AVAudioSession.InterruptionType(rawValue: typeValue) else { return }

        switch type {
        case .began:
            let msg = "[1080i50-AUDIO] ⏸️ Audio session interruption began (e.g. phone call / Siri)"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)

            ingestQueue.async { [weak self] in
                guard let self = self else { return }
                self.isAudioInterrupted = true
                self.audioRenderer.stopClock()
                self.isAudioClockStarted = false
                self.notePlaybackStateChanged()
            }

        case .ended:
            let optionsValue = userInfo[AVAudioSessionInterruptionOptionKey] as? UInt ?? 0
            let options = AVAudioSession.InterruptionOptions(rawValue: optionsValue)
            let shouldResume = options.contains(.shouldResume)

            let msg = "[1080i50-AUDIO] ▶️ Audio session interruption ended (shouldResume: \(shouldResume))"
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)

            guard sessionState.generation > 0 else { return }

            AudioSessionManager.shared.configureForPlayback()

            ingestQueue.async { [weak self] in
                guard let self = self else { return }
                self.isAudioInterrupted = false

                if self.audioRenderer.status == .failed {
                    let recoverMsg = "[1080i50-AUDIO] 🔄 Audio renderer was in failed state after interruption — resetting"
                    print(recoverMsg)
                    logger.notice("\(recoverMsg, privacy: .public)")
                    TelemetryServer.shared.log(recoverMsg)

                    self.audioRenderer.reset()
                    DispatchQueue.main.async { [weak self] in
                        guard let self = self else { return }
                        self.presentationContext?.requestReattach(from: self)
                    }
                } else {
                    self.audioRenderer.flush()
                    DispatchQueue.main.async { [weak self] in
                        guard let self = self else { return }
                        self.presentationContext?.requestReset(from: self)
                    }
                }

                self.firstAudioPTS = nil
                self.firstVideoFieldPTS = nil
            // Cleared with it. A recovery re-anchors audio onto a new timeline, and a
            // picture timestamp kept from before it pins the start anchor to an instant
            // the audio no longer reaches - after which no anchor can ever be chosen
            // again and the session sits frozen for good.
            self.firstDecodedPicturePTS = nil
            self.commitAnchor = nil
                self.audioBuffersPreRolledCount = 0
                self.preRollStartTime = 0
                self.isAudioClockStarted = false
            }

        @unknown default:
            break
        }
    }

    private func handleAudioRouteChange(_ notification: Notification) {
        guard let userInfo = notification.userInfo,
              let reasonValue = userInfo[AVAudioSessionRouteChangeReasonKey] as? UInt,
              let reason = AVAudioSession.RouteChangeReason(rawValue: reasonValue) else { return }

        let msg = "[1080i50-AUDIO] 🎧 Audio route changed: reason \(reasonValue)"
        logger.notice("\(msg, privacy: .public)")

        // Every one of these leaves the session pointed at hardware with a
        // different channel count, sample rate, and output latency than the one
        // it was configured for. `.newDeviceAvailable` is the case that matters
        // most in practice — headphones connected mid-playback — and it used to
        // fall through here, so the session kept the multichannel configuration
        // of the built-in speaker while the audio was already going out over a
        // two-channel Bluetooth link.
        switch reason {
        case .newDeviceAvailable, .oldDeviceUnavailable, .override,
             .categoryChange, .routeConfigurationChange:
            AudioSessionManager.shared.configureForPlayback()
        default:
            AudioSessionManager.shared.logCurrentRoute()
        }
    }

    private func handleAudioRendererWasFlushedAutomatically(_ notification: Notification) {
        let msg = "[1080i50-AUDIO] ⚠️ Audio renderer was flushed automatically by system — re-anchoring"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)

        guard sessionState.generation > 0 else { return }

        let recovery = beginRecovery("renderer flushed by the system")
        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            guard self.isCurrentRecovery(recovery) else { return }
            self.isAudioClockStarted = false
            self.firstAudioPTS = nil
            // The picture anchor goes with it. Re-anchoring audio while keeping a
            // picture timestamp from the old timeline pins the start anchor where the
            // new audio never reaches, and nothing can be anchored again.
            self.firstDecodedPicturePTS = nil
            self.firstVideoFieldPTS = nil
            self.commitAnchor = nil
            self.audioBuffersPreRolledCount = 0
            self.preRollStartTime = 0

            DispatchQueue.main.async { [weak self] in
                guard let self = self else { return }
                self.presentationContext?.requestReset(from: self)
            }
        }
    }

    public func audioRendererDidEncounterError(_ renderer: NativeTSAudioRenderer, error: Error) {
        let msg = "[1080i50-AUDIO] ❌ Audio renderer error: \(error.localizedDescription) — resetting"
        print(msg)
        logger.error("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)

        guard sessionState.generation > 0 else { return }

        let recovery = beginRecovery("audio renderer error")
        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            guard self.isCurrentRecovery(recovery) else { return }
            self.audioRenderer.reset()
            self.isAudioClockStarted = false
            self.firstAudioPTS = nil
            // The picture anchor goes with it. Re-anchoring audio while keeping a
            // picture timestamp from the old timeline pins the start anchor where the
            // new audio never reaches, and nothing can be anchored again.
            self.firstDecodedPicturePTS = nil
            self.firstVideoFieldPTS = nil
            self.commitAnchor = nil
            self.audioBuffersPreRolledCount = 0
            self.preRollStartTime = 0

            DispatchQueue.main.async { [weak self] in
                guard let self = self else { return }
                self.presentationContext?.requestReattach(from: self)
            }
        }
    }

    public func audioRendererDidChangeStatus(_ renderer: NativeTSAudioRenderer, status: AVQueuedSampleBufferRenderingStatus) {
        if status == .failed && sessionState.generation > 0 {
            audioRendererDidEncounterError(renderer, error: renderer.audioRenderer.error ?? NSError(domain: "AVFoundation", code: -1, userInfo: [NSLocalizedDescriptionKey: "Audio renderer status failed"]))
        }
    }

    // MARK: - PESPacketAssemblerDelegate

    public func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        if let alternate = alternateAssembler {
            alternate.feed(payload: payload)
        } else {
            accessUnitAssembler.feed(payload: payload)
        }
    }

    public func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        telemetry.mutate { $0.pesErrors += 1 }
    }

    // MARK: - H264AccessUnitAssemblerDelegate

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        handleVideoFormat(formatDescription, info: info)
    }

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didDiscoverAFD afd: VideoGeometry.ActiveFormatDescription) {
        telemetry.mutate {
            $0.afdDescription = afd.description
        }
    }

    // MARK: - VideoAccessUnitAssemblerDelegate

    public func videoAssembler(didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        handleVideoFormat(formatDescription, info: info)
    }

    public func videoAssembler(didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
        handleVideoSampleBuffer(sampleBuffer, isSyncSample: isSyncSample, structure: structure)
    }

    /// Everything that happens when a format arrives, whichever assembler
    /// produced it. The codec decides how a picture is cut out of the stream
    /// and nothing after that.
    private func handleVideoFormat(_ formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        let paramMs = sessionState.mutate { state -> Double? in
            if state.paramsReadyTime == 0 {
                state.paramsReadyTime = CACurrentMediaTime()
                let base = state.psiParsedTime > 0 ? state.psiParsedTime : (state.firstDataTime > 0 ? state.firstDataTime : state.requestStartTime)
                return (state.paramsReadyTime - base) * 1000.0
            }
            return nil
        }
        if let paramMs = paramMs {
            telemetry.mutate { $0.ttfpParamSetsMs = paramMs }
        }

        decoder.configure(with: formatDescription)

        // Drives whether the render view bob-deinterlaces or passes through.
        let interlaced = info.isInterlaced
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.presentationContext?.setSourceInterlaced(interlaced, from: self)
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

            $0.isDirect1080iVerified = info.isInterlaced
            $0.validationWarning = nil
        }
    }

    public func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
        handleVideoSampleBuffer(sampleBuffer, isSyncSample: isSyncSample, structure: structure)
    }

    private var lastSyncPTS: CMTime = .invalid
    private var syncCount: Int = 0
    private var framesSinceLastSync: Int = 0

    private func handleVideoSampleBuffer(_ sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
        framesSinceLastSync += 1

        if isSyncSample, sampleBuffer.presentationTimeStamp.isValid {
            if lastSyncPTS.isValid {
                let gopSec = sampleBuffer.presentationTimeStamp.seconds - lastSyncPTS.seconds
                let msg = "[1080i50-GOP] 🔑 Sync/Keyframe AU #\(syncCount + 1) @ PTS \(String(format: "%.3f", sampleBuffer.presentationTimeStamp.seconds))s | GOP: \(String(format: "%.2f", gopSec))s (\(framesSinceLastSync) frames)"
                print(msg)
                logger.notice("\(msg, privacy: .public)")
                TelemetryServer.shared.log(msg)
            } else {
                let msg = "[1080i50-GOP] 🔑 First Sync/Keyframe AU @ PTS \(String(format: "%.3f", sampleBuffer.presentationTimeStamp.seconds))s"
                print(msg)
                logger.notice("\(msg, privacy: .public)")
                TelemetryServer.shared.log(msg)
            }
            lastSyncPTS = sampleBuffer.presentationTimeStamp
            framesSinceLastSync = 0
            syncCount += 1
        }

        if case .closed(let reason) = decodeGateState {
            let now = CACurrentMediaTime()
            if firstAccessUnitTime == 0 {
                firstAccessUnitTime = now
            }

            if isSyncSample {
                if !decoder.hasActiveSession {
                    _ = decoder.ensureSession()
                }

                if decoder.hasActiveSession {
                    decodeGateState = .open
                    let zapId = currentZapId
                    let msg = "[ZAP-#\(zapId)-SYNC] 🔓 Decode gate opened on a sync sample (reason: \(reason)) after \(gatedAccessUnitCount) discarded AU(s), \(String(format: "%.0f", (now - firstAccessUnitTime) * 1000.0))ms"
                    print(msg)
                    logger.notice("\(msg, privacy: .public)")
                    TelemetryServer.shared.log(msg)
                    telemetry.mutate { $0.vtSessionActive = true }
                } else {
                    gatedAccessUnitCount += 1
                    telemetry.mutate { $0.gatedAccessUnits = self.gatedAccessUnitCount }
                    return
                }
            } else if reason == .startup && (now - firstAccessUnitTime >= Self.decodeGateTimeout) {
                // Startup fail-open: better a damaged first second than a channel that stays black.
                if !decoder.hasActiveSession {
                    _ = decoder.ensureSession()
                }
                decodeGateState = .open
                let msg = "[1080i50-GATE] ⚠️ Startup timeout: No sync sample within \(String(format: "%.1f", Self.decodeGateTimeout))s — opening decode gate anyway after \(gatedAccessUnitCount) discarded AU(s); expect artefacts until the next intra picture"
                print(msg)
                logger.error("\(msg, privacy: .public)")
                TelemetryServer.shared.log(msg)
                telemetry.mutate { $0.vtSessionActive = decoder.hasActiveSession }
            } else {
                // Under .decoderRecovery, .formatReconfiguration, or .backgrounded:
                // STRICT IDR ONLY — NEVER timeout fail-open on non-sync frames!
                gatedAccessUnitCount += 1
                let ptsSec = sampleBuffer.presentationTimeStamp.isValid ? String(format: "%.3f", sampleBuffer.presentationTimeStamp.seconds) : "invalid"
                let elapsedMs = String(format: "%.0f", (now - firstAccessUnitTime) * 1000.0)
                let discardLog = "[1080i50-GATE-DISCARD] 🚫 AU #\(gatedAccessUnitCount) discarded (reason: \(reason), isSync: \(isSyncSample)) @ PTS: \(ptsSec)s (elapsed: \(elapsedMs)ms)"
                print(discardLog)
                TelemetryServer.shared.log(discardLog)
                telemetry.mutate { $0.gatedAccessUnits = self.gatedAccessUnitCount }
                return
            }

            telemetry.mutate { $0.gatedAccessUnits = self.gatedAccessUnitCount }
        }

        let idrMs = sessionState.mutate { state -> Double? in
            if state.firstIdrTime == 0 {
                state.firstIdrTime = CACurrentMediaTime()
                let base = max(state.paramsReadyTime, state.psiParsedTime, state.firstDataTime, state.requestStartTime)
                return (state.firstIdrTime - base) * 1000.0
            }
            return nil
        }
        if let idrMs = idrMs {
            telemetry.mutate { $0.ttfpIdrMs = idrMs }
        }

        let hasPTS = CMSampleBufferGetPresentationTimeStamp(sampleBuffer).isValid
        telemetry.mutate {
            $0.sampleBuffersEmittedCount += 1
            if !hasPTS { $0.accessUnitsWithoutPTS += 1 }
        }

        decoder.decode(sampleBuffer: sampleBuffer, structure: structure)
    }

    // MARK: - HardwareVideoDecoderDelegate

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        // Compared against the presentation generation, which is what the decoder was
        // stamped with and what the surface admits by. Comparing against this session's
        // local zap counter worked only while there was one session and the two numbers
        // coincided; a second session counts its own first zap as one while carrying a
        // different presentation generation, and every one of its pictures was
        // discarded here - a prepared channel that would have committed to black.
        guard frame.generation == presentationGeneration.rawValue else {
            // Stale frame from a retired generation -> discard immediately
            return
        }

        let (decMs, rate) = sessionState.mutate { state -> (Double?, Double?) in
            var computedDecMs: Double?
            if state.firstDecodedTime == 0 {
                state.firstDecodedTime = CACurrentMediaTime()
                let base = state.firstIdrTime > 0 ? state.firstIdrTime : (state.paramsReadyTime > 0 ? state.paramsReadyTime : state.requestStartTime)
                computedDecMs = (state.firstDecodedTime - base) * 1000.0
            }

            state.decodedFrameCounter += 1
            let now = Date()
            var computedRate: Double?

            if now.timeIntervalSince(state.lastDecodedRateCheck) >= 1.0 {
                let elapsed = now.timeIntervalSince(state.lastDecodedRateCheck)
                computedRate = Double(state.decodedFrameCounter) / elapsed
                state.decodedFrameCounter = 0
                state.lastDecodedRateCheck = now
            }

            return (computedDecMs, computedRate)
        }

        if let decMs = decMs {
            telemetry.mutate { $0.ttfpDecodeMs = decMs }
        }

        telemetry.mutate { $0.sampleBuffersDecodedCount += 1 }

        if frame.pts.isValid {
            // Recorded on the session's own decode path, not on the surface's report of
            // a submitted field. A session being prepared never receives that report -
            // it does not own the surface - and readiness that depended on it could
            // only ever be reached after a commit, which is the wrong way round.
            if firstDecodedPicturePTS == nil {
                firstDecodedPicturePTS = frame.pts
            }
            latestVideoPTS = frame.pts
            startVideoOnlyClockIfNeeded()
            warnIfMotionNeverStarted()
        }

        if let rate = rate {
            telemetry.mutate { $0.decodedFramesPerSec = rate }
            let snapshot = telemetry.snapshot()
            let lat = decoder.decodeLatencyStats
            let decLog = "[1080i50-DECODER] HW: \(snapshot.hwDecodeActive) | Latency: \(String(format: "%.1f", lat.lastMs))ms (mean \(String(format: "%.1f", lat.meanMs))ms, max \(String(format: "%.1f", lat.maxMs))ms) | Decoded AU/s: \(String(format: "%.1f", rate)) | Source: \(String(format: "%.1f", snapshot.sourceFrameRate)) fps (PTS delta \(String(format: "%.1f", snapshot.ptsProgressionMs))ms) | AUs w/o PTS: \(snapshot.accessUnitsWithoutPTS)"
            print(decLog)
            logger.notice("\(decLog, privacy: .public)")
            TelemetryServer.shared.log(decLog)
        }

        surfaceOutlet?.enqueue(frame)
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        telemetry.mutate { $0.hwDecodeActive = isHWActive }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool) {
        telemetry.mutate { $0.vtDeinterlaceAccepted = isAccepted }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        let isEarly = sessionState.mutate { state -> Bool in
            state.firstPictureDeliveredTime > 0 && CACurrentMediaTime() - state.firstPictureDeliveredTime <= 2.0
        }
        telemetry.mutate {
            $0.decodeErrors += 1
            if isEarly {
                $0.earlyStabilityIssues += 1
            }
        }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didInvalidateSessionWithFatalError error: OSStatus) {
        let isEarly = sessionState.mutate { state -> Bool in
            state.firstPictureDeliveredTime > 0 && CACurrentMediaTime() - state.firstPictureDeliveredTime <= 2.0
        }
        telemetry.mutate {
            $0.decodeErrors += 1
            $0.decoderRecoveries += 1
            $0.vtSessionActive = false
            $0.hwDecodeActive = false
            if isEarly {
                $0.earlyStabilityIssues += 1
            }
        }

        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            self.decodeGateState = .closed(reason: .decoderRecovery)
            self.gatedAccessUnitCount = 0
            self.firstAccessUnitTime = CACurrentMediaTime()
            let msg = "[1080i50-DEC-RECOVER] 🔄 Hardware video decoder session invalidated (fatal error \(error)). Decode gate closed (.decoderRecovery, strict IDR only); waiting for next IDR..."
            print(msg)
            logger.error("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)
        }
    }

    public func hardwareDecoder(_ decoder: HardwareVideoDecoder, didRequestSessionReconfiguration error: OSStatus) {
        telemetry.mutate {
            $0.vtSessionActive = false
            $0.hwDecodeActive = false
        }

        ingestQueue.async { [weak self] in
            guard let self = self else { return }
            self.decodeGateState = .closed(reason: .formatReconfiguration)
            self.gatedAccessUnitCount = 0
            self.firstAccessUnitTime = CACurrentMediaTime()
            let msg = "[1080i50-DEC-RECONFIG] 🔄 Video decoder reconfiguration requested (status \(error)). Decode gate closed (.formatReconfiguration); waiting for next IDR..."
            print(msg)
            logger.notice("\(msg, privacy: .public)")
            TelemetryServer.shared.log(msg)
        }
    }

    // MARK: - TTFP Completion Handler

    private func handleFirstFrameRendered() {
        let (totalMs, renderMs) = sessionState.mutate { state -> (Double?, Double?) in
            guard state.firstPictureDeliveredTime == 0, state.requestStartTime > 0 else { return (nil, nil) }
            state.firstPictureDeliveredTime = CACurrentMediaTime()
            let tMs = (state.firstPictureDeliveredTime - state.requestStartTime) * 1000.0
            let renderBase = state.firstDecodedTime > 0 ? state.firstDecodedTime : (state.firstIdrTime > 0 ? state.firstIdrTime : state.requestStartTime)
            let rMs = (state.firstPictureDeliveredTime - renderBase) * 1000.0
            return (tMs, rMs)
        }

        guard let totalMs = totalMs, let renderMs = renderMs else { return }

        // Graded against what this source can do, and by which side owns the cost.
        //
        // The old scale called anything over 800 ms a miss. No tune on this
        // hardware has ever reached that or can: the receiver alone takes
        // 661–1079 ms to answer the HTTP request, measured over nine tunes,
        // before a single byte exists to parse. A physically optimal tune was
        // being reported as a problem, which sends tuning effort at stages with
        // nothing left to give — and this pipeline has had plenty of that.
        //
        // Everything the player itself owns — PSI parse, decode, render —
        // measured between 42 and 99 ms across those same nine tunes. The rest
        // is the box answering and the stream reaching a random access point,
        // which is 179 ms on one channel and 5627 ms on another and is not ours
        // to shorten. So the grade names the dominant side rather than only the
        // sum: a slow player is worth acting on at any total, a slow stream is
        // worth knowing about and leaving alone.
        let stages = telemetry.snapshot()
        let playerBoundMs = stages.ttfpPsiMs + stages.ttfpDecodeMs + renderMs
        let streamBoundMs = max(0, totalMs - playerBoundMs)

        let rating: String
        if playerBoundMs > 300.0 {
            rating = "🟠 Player: \(String(format: "%.0f", playerBoundMs))ms statt der üblichen 42–99 ms"
        } else if totalMs <= 1500.0 {
            rating = "🎯 Am Boden dieser Quelle (≤ 1,5 s)"
        } else if totalMs <= 3000.0 {
            rating = "🟢 Gut für diese Quelle (≤ 3 s)"
        } else {
            rating = "⏳ \(String(format: "%.1f", streamBoundMs / 1000.0))s Warten auf Sender/Receiver, nicht auf den Player"
        }

        telemetry.mutate {
            $0.ttfpTotalMs = totalMs
            $0.ttfpRenderMs = renderMs
            $0.ttfpRating = rating
            $0.isFirstPicturePresented = true
        }

        let snapshot = telemetry.snapshot()
        let ttfpLog = "[1080i50-TTFP] Total: \(String(format: "%.1f", totalMs))ms | Setup: \(String(format: "%.1f", snapshot.ttfpSetupMs))ms | Net: \(String(format: "%.1f", snapshot.ttfpNetworkMs))ms | PSI: \(String(format: "%.1f", snapshot.ttfpPsiMs))ms | Params: \(String(format: "%.1f", snapshot.ttfpParamSetsMs))ms | FirstAU: \(String(format: "%.1f", snapshot.ttfpIdrMs))ms | Dec: \(String(format: "%.1f", snapshot.ttfpDecodeMs))ms | Render: \(String(format: "%.1f", renderMs))ms"
        print(ttfpLog)
        logger.notice("\(ttfpLog, privacy: .public)")
        TelemetryServer.shared.log(ttfpLog)
    }

    /// Records when the master clock reaches the first field, which is when the
    /// display layer puts it on screen.
    ///
    /// Registering an observer is not enough on its own: if the clock is already
    /// past the timestamp — which happens whenever audio pre-rolled while video
    /// was still being decoded — a boundary observer never fires, and the metric
    /// would stay at zero for exactly the streams that started fastest.
    private func observeFirstPictureVisible(at pts: CMTime) {
        removeFirstPictureObserver()
        guard pts.isValid else { return }

        let synchronizer = audioRenderer.synchronizer
        if CMTimebaseGetRate(synchronizer.timebase) > 0 {
            let now = CMTimebaseGetTime(synchronizer.timebase)
            if now.isValid && now >= pts {
                recordFirstPictureVisible()
                return
            }
        }

        let token = synchronizer.addBoundaryTimeObserver(
            forTimes: [NSValue(time: pts)],
            queue: .main
        ) { [weak self] in
            self?.recordFirstPictureVisible()
            self?.removeFirstPictureObserver()
        }
        firstPictureObserver = (token, synchronizer)
    }

    private func removeFirstPictureObserver() {
        let observer = firstPictureObserver
        firstPictureObserver = nil
        guard let observer = observer else { return }
        if Thread.isMainThread {
            observer.synchronizer.removeTimeObserver(observer.token)
        } else {
            DispatchQueue.main.async {
                observer.synchronizer.removeTimeObserver(observer.token)
            }
        }
    }

    /// The first field went up on its attachment instead of on the clock.
    ///
    /// Without this the tuning figure reports when the timeline *reached* that
    /// field, which is now seconds after the screen showed it: the measurement
    /// would charge the pipeline for a wait that no longer happens, and hide
    /// the very improvement it exists to show.
    private func handleFirstPictureShownImmediately() {
        removeFirstPictureObserver()
        recordFirstPictureVisible(viaImmediateDisplay: true)
    }

    private func recordFirstPictureVisible(viaImmediateDisplay: Bool = false) {
        let visibleMs = sessionState.mutate { state -> Double? in
            guard state.requestStartTime > 0 else { return nil }
            return (CACurrentMediaTime() - state.requestStartTime) * 1000.0
        }
        guard let visibleMs = visibleMs else { return }

        // Only the first one. A discontinuity re-anchors the clock and replays
        // this path, which would otherwise overwrite the tuning figure with the
        // time since the stream started.
        var isFirst = false
        telemetry.mutate {
            if $0.ttfpVisibleMs == 0 {
                $0.ttfpVisibleMs = visibleMs
                isFirst = true
            }
        }
        guard isFirst else { return }

        let snapshot = telemetry.snapshot()
        // The submit-to-visible gap is the part nothing could see before: it is
        // owned by the clock start, not by decode or render.
        // Two different measurements share this number, and reading them as one
        // is how a fixed startup still looks broken: on the clock path the gap
        // is the wait for the timeline to arrive at the field, on the immediate
        // path there is no wait and it is only the handoff to the renderer.
        let via = viaImmediateDisplay ? "shown immediately" : "reached by master clock"
        // On the immediate path the handoff happens inside the submit that is
        // still recording its own total — about half a millisecond before it
        // lands. Reporting a gap against a total of zero printed `0.0ms` and a
        // NaN; the breakdown follows on the next line anyway.
        let tail: String
        if snapshot.ttfpTotalMs > 0 {
            let gapLabel = viaImmediateDisplay ? "Submit to handoff" : "Waiting on master clock"
            let waitMs = visibleMs - snapshot.ttfpTotalMs
            tail = " | Pipeline ready at \(String(format: "%.1f", snapshot.ttfpTotalMs))ms | \(gapLabel): \(String(format: "%.1f", waitMs))ms"
        } else {
            tail = " | Pipeline breakdown follows"
        }
        let zapId = currentZapId
        let msg = "[ZAP-#\(zapId)-FIRST-PIC] 👁️ Picture visible after \(String(format: "%.1f", visibleMs))ms (\(via))\(tail)"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    private func handleFirstFrameActuallyPresented(screenTimestamp: Double) {
        let gpuDoneMs = sessionState.mutate { state -> Double? in
            guard state.requestStartTime > 0 else { return nil }
            return (screenTimestamp - state.requestStartTime) * 1000.0
        }
        if let gpuDoneMs = gpuDoneMs {
            // Drawing into our own drawable, the GPU finishing *is* the picture
            // appearing — there is no layer holding it back for a clock.
            telemetry.mutate {
                $0.ttfpGpuCompletedMs = gpuDoneMs
                if $0.ttfpVisibleMs == 0 { $0.ttfpVisibleMs = gpuDoneMs }
            }
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
            self.thermalObserver = NotificationCenter.default.addObserver(
                forName: ProcessInfo.thermalStateDidChangeNotification,
                object: nil,
                queue: .main
            ) { [weak self] _ in
                self?.handleThermalStateChanged()
            }
        }
    }

    private func handleThermalStateChanged() {
        let state = ProcessInfo.processInfo.thermalState
        let thermalString: String
        switch state {
        case .nominal: thermalString = "Nominal 🟢"
        case .fair: thermalString = "Fair 🟡"
        case .serious: thermalString = "Serious 🟠"
        case .critical: thermalString = "Critical 🔴"
        @unknown default: thermalString = "Unknown"
        }
        let msg = "[1080i50-THERMAL-CHANGE] 🌡️ ProcessInfo thermal state changed to: \(thermalString)"
        print(msg)
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
        updateSystemMetrics()
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

        var vmInfo = task_vm_info_data_t()
        var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<natural_t>.size)
        let kerr: kern_return_t = withUnsafeMutablePointer(to: &vmInfo) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
                task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count)
            }
        }
        let footprintMB = (kerr == KERN_SUCCESS) ? Double(vmInfo.phys_footprint) / (1024.0 * 1024.0) : 0.0
        let residentMB = (kerr == KERN_SUCCESS) ? Double(vmInfo.resident_size) / (1024.0 * 1024.0) : 0.0

        // Compute Total Process CPU across all active Mach threads
        var threadList: thread_act_array_t?
        var threadCount: mach_msg_type_number_t = 0
        let threadErr = task_threads(mach_task_self_, &threadList, &threadCount)
        var totalCpuUsage: Double = 0.0
        if threadErr == KERN_SUCCESS, let threadList = threadList {
            for i in 0..<Int(threadCount) {
                var threadInfo = thread_basic_info()
                var threadInfoCount = mach_msg_type_number_t(THREAD_INFO_MAX)
                let infoResult = withUnsafeMutablePointer(to: &threadInfo) {
                    $0.withMemoryRebound(to: integer_t.self, capacity: 1) {
                        thread_info(threadList[i], thread_flavor_t(THREAD_BASIC_INFO), $0, &threadInfoCount)
                    }
                }
                if infoResult == KERN_SUCCESS {
                    if (threadInfo.flags & TH_FLAGS_IDLE) == 0 {
                        totalCpuUsage += Double(threadInfo.cpu_usage) / Double(TH_USAGE_SCALE) * 100.0
                    }
                }
                // Release the send right created by task_threads to prevent Mach port leaks
                mach_port_deallocate(mach_task_self_, threadList[i])
            }
            vm_deallocate(mach_task_self_, vm_address_t(UInt(bitPattern: threadList)), vm_size_t(threadCount * UInt32(MemoryLayout<thread_t>.size)))
        }

        let inFlight = decoder.inFlightFrames
        let liveFrames = DecodedVideoFrame.liveCount.withLock { $0 }
        let presenterQ = 0 // see above
        // 1920x1080 NV12 BiPlanar frame is ~3.11 MB
        let approxFrameMB = Double(liveFrames) * 3.11
        let approxPresenterMB = Double(presenterQ) * 3.11
        let audioQ = audioRenderer.consumeFlowStats().pendingBuffers

        ingestStateLock.lock()
        let backlogKiB = pendingIngestBytes / 1024
        ingestStateLock.unlock()

        let currentFootprint = footprintMB > 0 ? footprintMB : residentMB
        let peakMB = telemetry.mutate { values -> Double in
            values.thermalState = thermalString
            values.memoryUsageMB = currentFootprint
            values.peakMemoryFootprintMB = max(values.peakMemoryFootprintMB, currentFootprint)
            values.processCpuUsagePercent = totalCpuUsage
            values.vtInFlightFrames = inFlight
            return values.peakMemoryFootprintMB
        }

        // Low Power Mode caps ProMotion at 60 Hz regardless of the display link's
        // preferred range, so it has to be visible before a 60 Hz reading gets
        // blamed on the Info.plist key or the frame-rate request.
        let lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled

        let sysLog = "[1080i50-SYSTEM] Thermal: \(thermalString) | Footprint: \(String(format: "%.1f", footprintMB)) MB (Peak: \(String(format: "%.1f", peakMB)) MB, Resident: \(String(format: "%.1f", residentMB)) MB) | Process CPU: \(String(format: "%.1f", totalCpuUsage))% | LiveFrames: \(liveFrames) (~\(String(format: "%.0f", approxFrameMB))MB) | PresenterQ: \(presenterQ) (~\(String(format: "%.0f", approxPresenterMB))MB) | AudioQ: \(audioQ) | VT InFlight: \(inFlight) | Ingest: \(backlogKiB) KiB | LowPower: \(lowPower)"
        print(sysLog)
        logger.notice("\(sysLog, privacy: .public)")
        TelemetryServer.shared.log(sysLog)
    }

    private func stopSystemMonitoring() {
        let timer = systemMonitoringTimer
        systemMonitoringTimer = nil
        let observer = thermalObserver
        thermalObserver = nil
        if Thread.isMainThread {
            timer?.invalidate()
            if let obs = observer {
                NotificationCenter.default.removeObserver(obs)
            }
        } else {
            DispatchQueue.main.async {
                timer?.invalidate()
                if let obs = observer {
                    NotificationCenter.default.removeObserver(obs)
                }
            }
        }
    }

    static func currentMemoryStats() -> (footprintMB: Double, residentMB: Double) {
        var vmInfo = task_vm_info_data_t()
        var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<natural_t>.size)
        let kerr: kern_return_t = withUnsafeMutablePointer(to: &vmInfo) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
                task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count)
            }
        }
        let footprintMB = (kerr == KERN_SUCCESS) ? Double(vmInfo.phys_footprint) / (1024.0 * 1024.0) : 0.0
        let residentMB = (kerr == KERN_SUCCESS) ? Double(vmInfo.resident_size) / (1024.0 * 1024.0) : 0.0
        return (footprintMB, residentMB)
    }
}

// MARK: - PresentablePlaybackSession

extension NativeTSVideoPipeline: PresentablePlaybackSession {
    public var presentationSynchronizer: AVSampleBufferRenderSynchronizer {
        audioRenderer.synchronizer
    }

    /// Whether this session could be put on screen right now.
    ///
    /// Deliberately not "a frame was decoded". A session that has a picture but no
    /// usable audio at the same instant would be committed and then hold that picture
    /// still while audio caught up - which is the 0.6 to 1 second of frozen picture
    /// this whole rebuild exists to remove, moved behind the commit rather than
    /// removed. So all three have to hold together:
    ///
    ///   - a decoded picture exists, and the anchor the clock will start on is known,
    ///   - audio frames exist that are decodable from that same anchor,
    ///   - their timestamps are coherent with it, which is what says the leading
    ///     orphan audio can be trimmed once rather than resynchronised forever.
    public var isPresentable: Bool {
        // Never while rebuilding. A session mid-recovery may still be holding an anchor
        // and a picture from the timeline it is discarding, and committing to that is
        // committing to a stream that has already gone.
        guard lifecycle == .stable else { return false }
        guard let anchor = commitAnchor, anchor.isValid else { return false }
        guard firstDecodedPicturePTS != nil else { return false }
        return audioRenderer.hasBuffersCovering(anchor)
    }

    /// Starts this session's clock at the anchor its picture and audio share.
    ///
    /// The leading audio - everything that precedes the picture the surface will show
    /// first - is dropped here, once. Nothing resynchronises afterwards: the clock is
    /// anchored to a timestamp both streams already carry.
    public func becomeAudible(_ grant: AudibleGrant) {
        guard grant.presentationGeneration == presentationGeneration else {
            reportWarning("audible grant named \(grant.presentationGeneration), session is \(presentationGeneration)")
            return
        }
        guard let anchor = commitAnchor, anchor.isValid else {
            reportWarning("made audible with no start anchor")
            return
        }
        guard !isAudioClockStarted else { return }

        let pruned = audioRenderer.pruneBuffersBefore(time: anchor)
        audioRenderer.setAudible(true)
        audioRenderer.setRate(1.0, time: anchor)
        isAudioClockStarted = true

        let msg = "[COMMIT-\(presentationGeneration)] ⏱️ clock started at \(String(format: "%.3f", anchor.seconds))s | trimmed \(pruned.prunedCount) leading audio buffer(s) | remaining lead \(String(format: "%.0f", pruned.remainingLeadMs))ms"
        logger.notice("\(msg, privacy: .public)")
        TelemetryServer.shared.log(msg)
    }

    public func silence() {
        audioRenderer.setAudible(false)
        guard isAudioClockStarted else { return }
        audioRenderer.setRate(0.0, time: .invalid)
        isAudioClockStarted = false
        logger.notice("[Presentation] \(self.presentationGeneration.description, privacy: .public) silenced")
    }

    public func surfaceDidSubmitFirstField(pts: CMTime) {
        if firstVideoFieldPTS == nil, pts.isValid {
            firstVideoFieldPTS = pts
            sessionState.mutate { $0.firstVideoFieldTime = CACurrentMediaTime() }
        }
        observeFirstPictureVisible(at: pts)
    }

    public func surfaceDidRenderFirstFrame() { handleFirstFrameRendered() }

    public func surfaceDidPresentFirstFrame(atScreenTime screenTime: CFTimeInterval) {
        handleFirstFrameActuallyPresented(screenTimestamp: screenTime)
    }

    public func surfaceDidPresentFirstFieldImmediately() { handleFirstPictureShownImmediately() }

    public func surfaceDidWarn(_ text: String) { reportWarning(text) }
}
