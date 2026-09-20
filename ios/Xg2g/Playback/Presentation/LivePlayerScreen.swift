// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import SwiftUI
import UIKit

/// Production Live TV broadcast player powered by Phase 1 1080i50 -> 1080p50 VideoToolbox + Metal.
public struct LivePlayerScreen: View {

    @Environment(\.dismiss) private var dismiss
    /// The catalogue, when the caller has one.
    private let model: AppModel?
    private var playbackManager: PlaybackManager
    private var coordinator: ZapCoordinator { playbackManager.coordinator }

    /// The readouts of the channel actually on screen.
    private var tele: TelemetryValues { coordinator.playing?.telemetry.display ?? TelemetryValues() }
    /// The stream currently selected, empty until a channel is chosen.
    ///
    /// This used to open on a hard-coded receiver on a specific LAN, which made
    /// the screen appear to work on exactly one network and silently fail
    /// everywhere else.
    @State private var streamURLString: String = ""
    @State private var currentChannelName: String = ""
    private enum StreamRouteMode: String, CaseIterable {
        case livePipeline = "LIVE (v3 Ingest)"
        case direct = "DIRECT (Vu+:8001)"
        case legacySmoother = "SMOOTHER (legacy)"
    }
    public enum PlaybackEngineMode: Equatable {
        case nativeDirectLive
        case timeshiftHLS
    }

    @State private var engineMode: PlaybackEngineMode = .nativeDirectLive
    @State private var timeshiftPlayer: AVPlayer?
    @State private var isTimeshiftLoading: Bool = false
    @State private var timeshiftOffsetSeconds: Double = 0
    @State private var timeshiftTotalDuration: Double = 0
    @State private var timeshiftCurrentPosition: Double = 0
    @State private var timeshiftObserverToken: Any?
    @State private var isTimeshiftScrubbing: Bool = false

    @State private var streamRouteMode: StreamRouteMode = .livePipeline
    @State private var isStreaming: Bool = false
    @State private var isPlaying: Bool = true
    @State private var showHUD: Bool = false
    @State private var presentationPath: MetalVideoView.PresentationPath = .systemLayer
    @State private var viewPreset: VideoViewPreset = .standard
    @State private var showControls: Bool = true
    @State private var showLandscapeZapBar: Bool = false
    @State private var showPortraitDrawer: Bool = false
    @State private var autoHideControlsTask: Task<Void, Never>?
    @State private var zapToast: String?
    @State private var hideZapToastTask: Task<Void, Never>?
    @State private var currentSubtitleImage: CGImage?
    @State private var verticalDragOffset: CGFloat = 0
    @State private var isDraggingDown: Bool = false
#if os(tvOS)
    @FocusState private var isPlayPauseFocused: Bool
#endif

    private struct ChannelPreset: Identifiable, Hashable {
        var id: String { serviceRef }
        let name: String
        let serviceRef: String
        let url: String
        let epgNow: String
        let category: String
    }


    let initialChannel: Channel?

    init(model: AppModel? = nil, playbackManager: PlaybackManager? = nil, channel: Channel? = nil) {
        self.model = model
        self.initialChannel = channel
        let pm = playbackManager ?? model?.playbackManager ?? PlaybackManager(
            preparationsProvider: { [weak model] in
                model?.makeZapPreparationClient()
            },
            streamURL: { [weak model] serviceRef in
                model?.liveStreamURL(for: serviceRef)
            }
        )
        self.playbackManager = pm
        let initialURL = channel.flatMap {
            model?.directStreamURL(for: $0)?.absoluteString
                ?? model?.liveStreamURL(for: $0.serviceRef)?.absoluteString
        }
        if let channel {
            _currentChannelName = State(initialValue: channel.name)
        }
        if let initialURL {
            _streamURLString = State(initialValue: initialURL)
        }
    }

    /// The catalogue logo for a preset, matched on `serviceRef` or `name`.
    private func logoURL(for serviceRef: String) -> URL? {
        model?.channels.first { $0.serviceRef == serviceRef }?.logoURL
    }

    private func logoURL(forPreset preset: ChannelPreset) -> URL? {
        if let match = model?.channels.first(where: { $0.serviceRef == preset.serviceRef || $0.name == preset.name }) {
            return match.logoURL
        }
        return nil
    }

    /// The serviceRef currently having actual visual presentation on screen.
    private var activePresentedServiceRef: String {
        coordinator.displayedServiceRef ?? coordinator.presentedServiceRef ?? currentServiceRef
    }

    /// The preset currently committed and presented on screen.
    /// SINGLE SOURCE OF TRUTH for header, logo, EPG, audio HUD and Now Playing.
    private var presentedPreset: ChannelPreset? {
        presets.first { $0.serviceRef == activePresentedServiceRef }
            ?? presets.first { $0.url == streamURLString || $0.name == currentChannelName }
    }

    /// The preset currently being prepared in-flight (if any).
    private var requestedPreset: ChannelPreset? {
        guard let reqRef = coordinator.requestedServiceRef else { return nil }
        return presets.first { $0.serviceRef == reqRef }
            ?? presets.first { $0.url.contains(reqRef) }
    }

    private var currentLogoURL: URL? {
        if let preset = presentedPreset, let url = logoURL(forPreset: preset) {
            return url
        }
        if let match = model?.channels.first(where: { $0.name == (presentedPreset?.name ?? currentChannelName) }) {
            return match.logoURL
        }
        return initialChannel?.logoURL
    }

    private var displayedChannelName: String {
        if !currentChannelName.isEmpty {
            return currentChannelName
        }
        if let name = presentedPreset?.name, !name.isEmpty {
            return name
        }
        if let name = initialChannel?.name, !name.isEmpty {
            return name
        }
        return "Live TV"
    }

    private var zapChannels: [Channel] {
        if let channels = model?.filteredChannels, !channels.isEmpty {
            return channels
        }
        if let channels = model?.channels, !channels.isEmpty {
            return channels
        }
        return presets.map { preset in
            Channel(
                id: preset.serviceRef.isEmpty ? preset.name : preset.serviceRef,
                name: preset.name,
                number: nil,
                serviceRef: preset.serviceRef,
                logoURL: logoURL(forPreset: preset)
            )
        }
    }

    private var currentChannel: Channel {
        let name = presentedPreset?.name ?? (!currentChannelName.isEmpty ? currentChannelName : (initialChannel?.name ?? ""))
        let sref = presentedPreset?.serviceRef ?? currentServiceRef
        if let model, let match = model.channels.first(where: { $0.serviceRef == sref || $0.name == name }) {
            return match
        }
        return Channel(
            id: sref.isEmpty ? "native_lab" : sref,
            name: name,
            number: nil,
            serviceRef: sref,
            logoURL: currentLogoURL
        )
    }

    private func switchToChannel(_ channel: Channel) {
        if let match = presets.first(where: { $0.serviceRef == channel.serviceRef || $0.name == channel.name }) {
            switchTo(preset: match)
        } else {
            guard let url = model?.directStreamURL(for: channel)?.absoluteString
                ?? model?.liveStreamURL(for: channel.serviceRef)?.absoluteString
            else { return }
            let newPreset = ChannelPreset(
                name: channel.name,
                serviceRef: channel.serviceRef,
                url: url,
                epgNow: model?.schedule[channel.serviceRef]?.now?.title ?? "",
                category: ""
            )
            switchTo(preset: newPreset)
        }
    }

    private func closePlayer() {
        if engineMode == .timeshiftHLS {
            teardownTimeshift()
            engineMode = .nativeDirectLive
            startCurrentPreset()
        }
        verticalDragOffset = 0
        isDraggingDown = false
        Haptics.shared.impact(.medium)
        playbackManager.minimize()
    }

    /// The channels this screen can tune, newest EPG title included.
    ///
    /// Only the real catalogue. There used to be a hard-coded bench list behind
    /// this, addressed at one receiver on one network; a build with no server
    /// configured showed eight channels that could never play.
    private var presets: [ChannelPreset] {
        guard let model, !model.channels.isEmpty else { return [] }
        let catalogue = model.filteredChannels.isEmpty ? model.channels : model.filteredChannels
        return catalogue.compactMap { channel -> ChannelPreset? in
            guard let url = model.directStreamURL(for: channel)?.absoluteString
                ?? model.liveStreamURL(for: channel.serviceRef)?.absoluteString
            else { return nil }
            return ChannelPreset(
                name: channel.name,
                serviceRef: channel.serviceRef,
                url: url,
                epgNow: model.schedule[channel.serviceRef]?.now?.title ?? "",
                category: ""
            )
        }
    }

    private var currentServiceRef: String {
        URL(string: streamURLString)?.lastPathComponent ?? streamURLString
    }

    private func isCurrentPreset(_ preset: ChannelPreset) -> Bool {
        if let presented = presentedPreset {
            return preset.serviceRef == presented.serviceRef || preset.name == presented.name
        }
        return preset.url == streamURLString ||
            preset.serviceRef == currentServiceRef ||
            preset.name == currentChannelName
    }

    /// The preset currently streaming, for anything that needs more than its URL.
    private var currentPreset: ChannelPreset? {
        presets.first { isCurrentPreset($0) }
    }

    public var body: some View {
        GeometryReader { geometry in
            let isLandscape = geometry.size.width > geometry.size.height

            ZStack(alignment: .top) {
                Color.black.ignoresSafeArea()

                VStack(spacing: 0) {
                    if !isLandscape && !showPortraitDrawer {
                        Spacer()
                    }

                    // MARK: - Video Stage Container
                    ZStack(alignment: .topLeading) {
                        // 1. Native Metal 1080p50 Hardware Stage
                        MetalVideoStageView(
                            telemetry: coordinator.playing?.telemetry,
                            presenter: coordinator.surface,
                            presentationContext: coordinator.context,
                            presentationPath: presentationPath,
                            scalingMode: viewPreset.scalingMode,
                            aspectRatioOverride: viewPreset.aspectRatio
                        )
                        .ignoresSafeArea(edges: isLandscape ? .all : [])
                        .opacity(engineMode == .nativeDirectLive ? 1.0 : 0.0)

                        // 1b. HLS Timeshift Player Stage (AVPlayer)
                        if let tsPlayer = timeshiftPlayer {
                            NativeVideoPlayerView(
                                player: tsPlayer,
                                showsPlaybackControls: false,
                                onDismiss: { closePlayer() }
                            )
                            .ignoresSafeArea(edges: isLandscape ? .all : [])
                            .opacity(engineMode == .timeshiftHLS ? 1.0 : 0.0)
                        }

                        // 1c. Timeshift Loading Indicator Overlay
                        if isTimeshiftLoading {
                            ZStack {
                                Color.black.opacity(0.4)
                                HStack(spacing: 8) {
                                    ProgressView()
                                        .tint(Theme.Colors.accentLive)
                                        .scaleEffect(0.9)
                                    Text("Timeshift wird vorbereitet…")
                                        .font(.app(size: 12, weight: .semibold))
                                        .foregroundStyle(.white)
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 8)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                                .shadow(color: Color.black.opacity(0.4), radius: 8)
                            }
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .transition(.opacity)
                        }

                        // 1d. Synchronized Native DVB Subtitle Overlay
                        if let subImage = currentSubtitleImage, engineMode == .nativeDirectLive {
                            Image(decorative: subImage, scale: 1.0)
                                .resizable()
                                .aspectRatio(16/9, contentMode: .fit)
                                .frame(maxWidth: .infinity, maxHeight: .infinity)
                                .allowsHitTesting(false)
                        }

                        // 2. Format Notice
                        if let unplayable = tele.unplayableVideoCodec {
                            UnplayableFormatNotice(
                                formatDescription: unplayable,
                                channelName: currentChannelName
                            )
                        }
                    }
                    .frame(
                        maxWidth: .infinity,
                        maxHeight: isLandscape ? .infinity : (geometry.size.width * 9.0 / 16.0)
                    )
                    .background(Color.black)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        if showLandscapeZapBar {
                            withAnimation(.easeInOut(duration: 0.2)) {
                                showLandscapeZapBar = false
                            }
                            scheduleControlsAutoHide()
                        } else if showPortraitDrawer {
                            withAnimation(.easeInOut(duration: 0.28)) {
                                showPortraitDrawer = false
                            }
                            scheduleControlsAutoHide()
                        } else {
                            withAnimation(.easeInOut(duration: 0.2)) {
                                showControls.toggle()
                            }
                            if showControls {
                                scheduleControlsAutoHide()
                            }
                        }
                    }
#if os(iOS)
                    .onContinuousHover { phase in
                        switch phase {
                        case .active:
                            if !showControls {
                                withAnimation(.easeInOut(duration: 0.2)) {
                                    showControls = true
                                }
                            }
                            scheduleControlsAutoHide()
                        case .ended:
                            scheduleControlsAutoHide()
                        }
                    }
#endif
#if os(iOS)
                    .gesture(
                        DragGesture(minimumDistance: 15)
                            .onChanged { value in
                                if value.translation.height > 0 && abs(value.translation.height) > abs(value.translation.width) {
                                    isDraggingDown = true
                                    verticalDragOffset = value.translation.height
                                }
                            }
                            .onEnded { value in
                                if isDraggingDown {
                                    if value.translation.height > 80 || value.predictedEndTranslation.height > 160 {
                                        closePlayer()
                                    } else {
                                        withAnimation(.spring(response: 0.3, dampingFraction: 0.82)) {
                                            verticalDragOffset = 0
                                            isDraggingDown = false
                                        }
                                    }
                                } else {
                                    if value.translation.width < -50 {
                                        Haptics.shared.impact(.medium)
                                        zapRelative(delta: 1)
                                    } else if value.translation.width > 50 {
                                        Haptics.shared.impact(.medium)
                                        zapRelative(delta: -1)
                                    }
                                }
                            }
                    )
#endif
                    .overlay(alignment: .topTrailing) {
                        if let requested = requestedPreset {
                            HStack(spacing: 8) {
                                ProgressView()
                                    .progressViewStyle(CircularProgressViewStyle(tint: .white))
                                    .scaleEffect(0.75)
                                    .frame(width: 14, height: 14)

                                VStack(alignment: .leading, spacing: 1) {
                                    Text(requested.name)
                                        .font(.app(size: 13, weight: .semibold))
                                        .foregroundStyle(.white)
                                    Text("Wird vorbereitet…")
                                        .font(.app(size: 10, weight: .regular))
                                        .foregroundStyle(.white.opacity(0.8))
                                }
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                            .shadow(color: Color.black.opacity(0.4), radius: 6, x: 0, y: 2)
                            .padding(.top, isLandscape ? 16 : 8)
                            .padding(.trailing, isLandscape ? 20 : 12)
                            .transition(.opacity.combined(with: .scale(scale: 0.95)))
                        }
                    }
                    .overlay(alignment: .center) {
                        if let zapToast {
                            Text(zapToast)
                                .font(.subheadline.weight(.bold))
                                .foregroundStyle(.white)
                                .padding(.horizontal, 16)
                                .padding(.vertical, 8)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                                .shadow(color: Color.black.opacity(0.4), radius: 8)
                                .transition(.scale(scale: 0.9).combined(with: .opacity))
                        }
                    }

                    // MARK: - Portrait Interactive Channel & Control Drawer
                    if !isLandscape {
                        if showPortraitDrawer {
                            portraitControlsDrawer
                                .transition(.move(edge: .bottom).combined(with: .opacity))
                        } else {
                            Spacer()
                        }
                    }
                }
                .animation(.easeInOut(duration: 0.28), value: showPortraitDrawer)

                // 2. Full-Screen On-Screen Display Controls
                if showControls && (!isLandscape || !showLandscapeZapBar) && !showPortraitDrawer {
                    videoOverlayControls(isLandscape: isLandscape, safeInsets: geometry.safeAreaInsets)
                        .transition(.opacity)
                }

#if os(tvOS)
                // Siri Remote background receiver when controls are hidden
                if !showControls && !showHUD && !showLandscapeZapBar {
                    Button {
                        withAnimation(.easeInOut(duration: 0.25)) {
                            showControls = true
                        }
                        scheduleControlsAutoHide()
                    } label: {
                        Color.clear
                    }
                    .buttonStyle(.plain)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .ignoresSafeArea()
                    .onMoveCommand { direction in
                        switch direction {
                        case .left:
                            zapRelative(delta: -1)
                        case .right:
                            zapRelative(delta: 1)
                        case .up, .down:
                            withAnimation(.easeInOut(duration: 0.25)) {
                                showControls = true
                            }
                            scheduleControlsAutoHide()
                        @unknown default:
                            break
                        }
                    }
                    .onPlayPauseCommand {
                        togglePlayPause()
                    }
                }
#endif

                // 3. Floating Telemetry Inspector Modal
                if showHUD {
                    telemetryHUDView
                        .padding(.top, isLandscape ? 48 : max(geometry.safeAreaInsets.top, 8) + 40)
                        .padding(.leading, max(geometry.safeAreaInsets.leading, 12))
                        .transition(.scale(scale: 0.9).combined(with: .opacity))
                }

                // 4. Landscape Quick-Zap Channel Carousel
                if isLandscape && showLandscapeZapBar {
                    VStack {
                        Spacer()
                        LandscapeQuickZapBar(
                            channels: zapChannels,
                            currentChannel: currentChannel,
                            schedule: model?.schedule ?? [:],
                            onSelect: { ch in
                                switchToChannel(ch)
                                withAnimation(.easeInOut(duration: 0.25)) {
                                    showLandscapeZapBar = false
                                }
                                scheduleControlsAutoHide()
                            },
                            onClose: {
                                withAnimation(.easeInOut(duration: 0.25)) {
                                    showLandscapeZapBar = false
                                }
                                scheduleControlsAutoHide()
                            }
                        )
                        .padding(.horizontal, max(geometry.safeAreaInsets.leading, geometry.safeAreaInsets.trailing, 16))
                        .padding(.bottom, max(12, geometry.safeAreaInsets.bottom))
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }

#if os(iOS)
                LivePlayerKeyboardShortcuts(
                    isLandscape: isLandscape,
                    engineMode: engineMode,
                    presentationPath: presentationPath,
                    togglePlayPause: {
                        togglePlayPause()
                        scheduleControlsAutoHide()
                    },
                    zapRelative: { delta in
                        zapRelative(delta: delta)
                        scheduleControlsAutoHide()
                    },
                    seekTimeshiftRelative: { delta in
                        seekTimeshiftRelative(delta)
                        scheduleControlsAutoHide()
                    },
                    enterTimeshift: { secs in
                        enterTimeshift(seekBackSeconds: secs)
                        scheduleControlsAutoHide()
                    },
                    displayZapToast: { msg in
                        displayZapToast(msg)
                    },
                    toggleZapDrawer: { isLand in
                        handleKeyboardToggleZapDrawer(isLandscape: isLand)
                    },
                    cycleViewPreset: {
                        cycleViewPreset()
                    },
                    toggleHUD: {
                        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                            showHUD.toggle()
                        }
                    },
                    startPiP: {
                        Haptics.shared.impact(.light)
                        coordinator.surface.startPictureInPicture()
                    },
                    handleEscape: {
                        handleKeyboardEscape()
                    }
                )
#endif
            }
#if !os(tvOS)
            .statusBarHidden(isLandscape)
#endif
            .persistentSystemOverlays(isLandscape ? .hidden : .automatic)
            .offset(y: max(0, verticalDragOffset))
            .scaleEffect(isDraggingDown ? max(0.85, 1.0 - (verticalDragOffset / 1200)) : 1.0)
            .clipShape(RoundedRectangle(cornerRadius: isDraggingDown ? min(32, verticalDragOffset / 4) : 0, style: .continuous))
            .animation(.interactiveSpring(response: 0.25, dampingFraction: 0.85), value: verticalDragOffset)
        }
#if os(tvOS)
        .onExitCommand {
            if showLandscapeZapBar {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showLandscapeZapBar = false
                }
            } else if showHUD {
                withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                    showHUD = false
                }
            } else if showControls {
                withAnimation(.easeInOut(duration: 0.25)) {
                    showControls = false
                }
            } else {
                closePlayer()
            }
        }
        .onChange(of: showControls) { _, isShowing in
            if isShowing {
                isPlayPauseFocused = true
            }
        }
#endif
        .onAppear {
            setupPlayback()
#if os(tvOS)
            isPlayPauseFocused = true
#endif
        }
        .onDisappear {
            if playbackManager.presentationMode == .hidden {
                teardownPlayback()
            } else if engineMode == .timeshiftHLS {
                teardownTimeshift()
                engineMode = .nativeDirectLive
                startCurrentPreset()
            }
        }
        .onChange(of: coordinator.playing) { _, newPipeline in
            currentSubtitleImage = nil
            newPipeline?.onSubtitleFrameEmitted = { [self] frame in
                Task { @MainActor in
                    self.currentSubtitleImage = frame?.image
                }
            }
        }
        .onChange(of: coordinator.displayedServiceRef ?? coordinator.presentedServiceRef) { _, newServiceRef in
            guard let newServiceRef else { return }
            if let preset = presets.first(where: { $0.serviceRef == newServiceRef }) {
                currentChannelName = preset.name
                streamURLString = preset.url
            }
            if let ch = model?.channels.first(where: { $0.serviceRef == newServiceRef }) {
                if playbackManager.currentChannel?.serviceRef != newServiceRef {
                    playbackManager.updateLiveChannel(ch)
                    model?.recordChannelPlayback(ch)
                }
            }
            announceNowPlaying()
        }
        .onChange(of: coordinator.phase) { _, newPhase in
            switch newPhase {
            case .failed(let serviceRef, let reason):
                let name = presets.first(where: { $0.serviceRef == serviceRef })?.name ?? currentChannelName

                // Smart Fallback: If direct receiver failed due to network/unreachable and we have an xg2g backend fallback
                let isNetworkFailure = reason.contains("connection") || reason.contains("unreachable") || reason.contains("timed out") || reason.contains("refused") || reason.contains("lost track")
                if isNetworkFailure,
                   let fallbackURL = model?.fallbackServerStreamURL(for: serviceRef),
                   effectiveStreamURL(for: streamURLString) != fallbackURL {
                    displayZapToast("Box nicht erreichbar – wechsle auf xg2g-Server...")
                    Task {
                        await coordinator.play(unprepared: fallbackURL, requestedAt: CACurrentMediaTime())
                    }
                    return
                }

                let userFriendly: String
                if reason.contains("scrambled") || reason.contains("descrambled") {
                    userFriendly = "Verschlüsselt (Smartcard erforderlich)"
                } else if reason.contains("admission") || reason.contains("tuner") {
                    userFriendly = "Alle Tuner belegt"
                } else if reason.contains("unpresentable") || reason.contains("attach") {
                    userFriendly = "Kein Signal / Stream nicht empfangbar"
                } else if isNetworkFailure {
                    userFriendly = "Empfänger nicht erreichbar (WLAN/VPN prüfen)"
                } else {
                    userFriendly = reason
                }
                displayZapToast("\(name): \(userFriendly)")
            case .warming, .buffering, .idle:
                break
            }
        }
    }

    // MARK: - Video Overlay Controls (Portrait & Landscape)

    @ViewBuilder
    private func videoOverlayControls(isLandscape: Bool, safeInsets: EdgeInsets) -> some View {
        let sideInset = isLandscape ? max(safeInsets.leading, safeInsets.trailing, 16) : 12

        ZStack {
            // Vignette Gradient
            LinearGradient(
                colors: [
                    Color.black.opacity(0.75),
                    Color.clear,
                    Color.clear,
                    Color.black.opacity(0.75)
                ],
                startPoint: .top,
                endPoint: .bottom
            )
            .ignoresSafeArea()
            .allowsHitTesting(false)

            VStack(spacing: 0) {
                // Top Action Bar
                HStack(spacing: 8) {
#if !os(tvOS)
                    // 1. Dismiss Button
                    Button {
                        closePlayer()
                    } label: {
                        Image(systemName: isLandscape ? "xmark.circle.fill" : "chevron.down")
                            .font(.app(size: isLandscape ? 20 : 14, weight: .bold))
                            .foregroundStyle(.white)
                            .frame(width: 36, height: 36)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())
                    .appHoverEffect(.highlight)
#endif

                    // 2. Channel Info (Compact & Non-overflowing)
                    ChannelLogo(url: currentLogoURL, name: displayedChannelName, size: isLandscape ? 32 : 28)

                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: 5) {
                            if let num = currentChannel.number {
                                Text(num)
                                    .font(.app(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(Theme.Colors.accentAction.opacity(0.2), in: RoundedRectangle(cornerRadius: 3, style: .continuous))
                            }

                            Text(displayedChannelName)
                                .font(.app(size: 15, weight: .bold))
                                .foregroundStyle(.white)
                                .lineLimit(1)

                            if engineMode == .nativeDirectLive {
                                Text("LIVE")
                                    .font(.app(size: 10, weight: .black, design: .rounded))
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentLive.opacity(0.2), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                            } else {
                                HStack(spacing: 3) {
                                    let isLiveHLS = abs(timeshiftOffsetSeconds) <= 5
                                    Text(isLiveHLS ? "LIVE" : "TIMESHIFT")
                                        .font(.app(size: 10, weight: .black, design: .rounded))
                                        .foregroundStyle(isLiveHLS ? Theme.Colors.accentLive : Theme.Colors.statusWarning)
                                        .padding(.horizontal, 6)
                                        .padding(.vertical, 2)
                                        .background((isLiveHLS ? Theme.Colors.accentLive : Theme.Colors.statusWarning).opacity(0.25), in: RoundedRectangle(cornerRadius: 4, style: .continuous))

                                    if !isLiveHLS {
                                        Text(formattedTimeshiftOffset)
                                            .font(.app(size: 10, weight: .bold, design: .monospaced))
                                            .foregroundStyle(.white)
                                            .padding(.horizontal, 5)
                                            .padding(.vertical, 2)
                                            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                                    }
                                }
                            }
                        }

                        let epgTitle = presentedPreset?.epgNow ?? presets.first(where: { $0.url == streamURLString })?.epgNow ?? ""
                        if !epgTitle.isEmpty {
                            Text(epgTitle)
                                .font(.app(size: 12, weight: .medium))
                                .foregroundStyle(.white.opacity(0.85))
                                .lineLimit(1)
                        }
                    }

                    Spacer(minLength: 4)

                    // Timeshift Direct Return Button (Header Quick Access)
                    if engineMode == .timeshiftHLS {
                        Button {
                            jumpToLiveEdge()
                        } label: {
                            HStack(spacing: 4) {
                                PulsingLiveDot(size: 5)
                                Text("Zur Live-Kante")
                                    .font(.app(size: 11, weight: .bold))
                            }
                            .padding(.horizontal, 9)
                            .padding(.vertical, 6)
                            .background(Theme.Colors.accentLive, in: Capsule())
                            .foregroundStyle(Color.black)
                            .shadow(color: Theme.Colors.accentLive.opacity(0.4), radius: 6)
                        }
                        .buttonStyle(.plain)
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                    }

#if os(tvOS)
                    // Dedicated TV Action Buttons
                    if let playing = coordinator.playing, !playing.availableAudioTracks.isEmpty {
                        Menu {
                            ForEach(playing.availableAudioTracks) { track in
                                Button {
                                    Haptics.shared.impact(.light)
                                    playing.selectAudioTrack(pid: track.pid)
                                } label: {
                                    HStack {
                                        Text(track.displayName)
                                        if playing.selectedAudioPID == track.pid {
                                            Image(systemName: "checkmark")
                                        }
                                    }
                                }
                            }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: playing.hasDecodableAudio ? "waveform" : "speaker.slash")
                                Text("Ton")
                            }
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundStyle(.white)
                        }
                        .buttonStyle(TVPlayerPillButtonStyle())
                    }

                    if let playing = coordinator.playing, !playing.availableSubtitleTracks.isEmpty {
                        Menu {
                            Button {
                                playing.selectSubtitleTrack(nil)
                            } label: {
                                HStack {
                                    Text("Aus")
                                    if playing.selectedSubtitleTrack == nil {
                                        Image(systemName: "checkmark")
                                    }
                                }
                            }
                            ForEach(playing.availableSubtitleTracks) { track in
                                Button {
                                    playing.selectSubtitleTrack(track)
                                } label: {
                                    HStack {
                                        Text(track.displayName)
                                        if playing.selectedSubtitleTrack?.id == track.id {
                                            Image(systemName: "checkmark")
                                        }
                                    }
                                }
                            }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "captions.bubble")
                                Text("Untertitel")
                            }
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundStyle(.white)
                        }
                        .buttonStyle(TVPlayerPillButtonStyle())
                    }

                    Button {
                        cycleViewPreset()
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: viewPreset.scalingMode == .fill ? "arrow.up.left.and.arrow.down.right" : "aspectratio")
                            Text(viewPreset.shortLabel)
                        }
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(.white)
                    }
                    .buttonStyle(TVPlayerPillButtonStyle())

                    Button {
                        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                            showHUD.toggle()
                        }
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: showHUD ? "chart.bar.fill" : "chart.bar")
                            Text("Info")
                        }
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(.white)
                    }
                    .buttonStyle(TVPlayerPillButtonStyle(isActive: showHUD))
#else
                    // 3. Aspect Ratio Preset Button (Primary Control, 44pt hit target)
                    Button {
                        cycleViewPreset()
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: viewPreset.scalingMode == .fill ? "arrow.up.left.and.arrow.down.right" : "aspectratio")
                                .font(.app(size: 11, weight: .bold))
                            Text(viewPreset.shortLabel)
                                .font(.app(size: 11, weight: .bold, design: .monospaced))
                        }
                        .fixedSize()
                        .foregroundStyle(.white)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
                    .appHoverEffect(.highlight)

                    if isLandscape {
                        // 4. Picture in Picture Button (Landscape direct access, 44x44 hitbox)
                        Button {
                            Haptics.shared.impact(.light)
                            coordinator.surface.startPictureInPicture()
                        } label: {
                            Image(systemName: "pip.enter")
                                .font(.app(size: 14, weight: .semibold))
                                .foregroundStyle(.white)
                                .frame(width: 36, height: 36)
                                .background(.ultraThinMaterial, in: Circle())
                                .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                        }
                        .buttonStyle(.plain)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                        .appHoverEffect(.highlight)
                        .disabled(presentationPath != .systemLayer)
                        .opacity(presentationPath == .systemLayer ? 1.0 : 0.4)

                        // 5. AirPlay Route Picker Button (Landscape direct access, 44x44 hitbox)
                        AirPlayButton()
                            .frame(width: 44, height: 44)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }

                    // 6. Overflow / Diagnose Menu ("..." 44x44 hitbox)
                    Menu {
                        if !isLandscape {
                            // Picture-in-Picture in Portrait
                            Button {
                                Haptics.shared.impact(.light)
                                coordinator.surface.startPictureInPicture()
                            } label: {
                                Label("Bild-in-Bild starten", systemImage: "pip.enter")
                            }
                            .disabled(presentationPath != .systemLayer)
                        }

                        // Audio-Spuren (Mehrsprachigkeit, Dolby Digital, Audiodeskription)
                        if let playing = coordinator.playing, !playing.availableAudioTracks.isEmpty {
                            Menu {
                                if playing.hasAudioTracksKnown && !playing.hasDecodableAudio {
                                    Section("Audio-Kompatibilität") {
                                        Text("⚠️ Kein AAC/Dolby (nur MPEG Audio)")
                                        if let fallbackURL = model?.fallbackServerStreamURL(for: activePresentedServiceRef),
                                           effectiveStreamURL(for: streamURLString) != fallbackURL {
                                            Button {
                                                Haptics.shared.impact(.medium)
                                                displayZapToast("Transkodiere Audio über xg2g-Server...")
                                                Task {
                                                    await coordinator.play(unprepared: fallbackURL, requestedAt: CACurrentMediaTime())
                                                }
                                            } label: {
                                                Label("Ton über xg2g transkodieren", systemImage: "arrow.triangle.2.circlepath")
                                            }
                                        }
                                    }
                                }

                                ForEach(playing.availableAudioTracks) { track in
                                    Button {
                                        Haptics.shared.impact(.light)
                                        playing.selectAudioTrack(pid: track.pid)
                                    } label: {
                                        HStack {
                                            Text(track.displayName)
                                            if playing.selectedAudioPID == track.pid {
                                                Image(systemName: "checkmark")
                                            }
                                        }
                                    }
                                }
                            } label: {
                                Label("Tonspuren (\(playing.availableAudioTracks.count))", systemImage: playing.hasDecodableAudio ? "waveform" : "speaker.slash")
                            }
                        }

                        // Untertitel Auswahl (DVB & Teletext)
                        if let playing = coordinator.playing, !playing.availableSubtitleTracks.isEmpty {
                            Menu {
                                Button {
                                    playing.selectSubtitleTrack(nil)
                                } label: {
                                    HStack {
                                        Text("Aus")
                                        if playing.selectedSubtitleTrack == nil {
                                            Image(systemName: "checkmark")
                                        }
                                    }
                                }
                                ForEach(playing.availableSubtitleTracks) { track in
                                    Button {
                                        playing.selectSubtitleTrack(track)
                                    } label: {
                                        HStack {
                                            Text(track.displayName)
                                            if playing.selectedSubtitleTrack?.id == track.id {
                                                Image(systemName: "checkmark")
                                            }
                                        }
                                    }
                                }
                            } label: {
                                Label("Untertitel (\(playing.availableSubtitleTracks.count))", systemImage: "captions.bubble")
                            }
                        }

                        // Stream-Info & Telemetrie Toggle
                        Button {
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                                showHUD.toggle()
                            }
                        } label: {
                            Label(
                                showHUD ? "Entwickler-Statistiken verbergen" : "Entwickler-Statistiken einblenden",
                                systemImage: showHUD ? "chart.bar.fill" : "chart.bar"
                            )
                        }

                        // Erweiterte Optionen (Labor / Diagnostik)
                        Menu {
                            // Presentation Path Toggle (Layer vs Metal)
                            Button {
                                Haptics.shared.impact(.light)
                                presentationPath = (presentationPath == .systemLayer) ? .metalDrawable : .systemLayer
                            } label: {
                                Label(
                                    presentationPath == .systemLayer ? "Renderpfad: System Layer" : "Renderpfad: Metal Direct",
                                    systemImage: presentationPath == .systemLayer ? "rectangle.on.rectangle" : "cpu"
                                )
                            }

                            // Stream-Routing (Labor / Bench A/B Test)
                            Menu("Stream-Routing") {
                                ForEach(StreamRouteMode.allCases, id: \.self) { mode in
                                    Button {
                                        streamRouteMode = mode
                                        if isStreaming { startCurrentPreset() }
                                    } label: {
                                        HStack {
                                            Text(mode.rawValue)
                                            if streamRouteMode == mode {
                                                Image(systemName: "checkmark")
                                            }
                                        }
                                    }
                                }
                            }
                        } label: {
                            Label("Erweiterte Optionen", systemImage: "wrench.and.screwdriver")
                        }
                    } label: {
                        Image(systemName: showHUD ? "ellipsis.circle.fill" : "ellipsis.circle")
                            .font(.app(size: 16, weight: .semibold))
                            .foregroundStyle(showHUD ? Theme.Colors.accentLive : Color.white)
                            .frame(width: 36, height: 36)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(showHUD ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())
                    .appHoverEffect(.highlight)
#endif
                }
                .padding(.horizontal, sideInset)
                .padding(.top, isLandscape ? 12 : max(safeInsets.top, 8))

                Spacer()

                // Center Transport Controls
                HStack(spacing: 24) {
                    // Previous Channel
                    Button {
                        zapRelative(delta: -1)
                    } label: {
                        Image(systemName: "backward.end.fill")
                            .font(.app(size: 20, weight: .bold))
                            .foregroundStyle(.white)
#if !os(tvOS)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
#endif
                    }
#if os(tvOS)
                    .buttonStyle(TVPlayerTransportButtonStyle(size: 56, isPrimary: false))
#else
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)
#endif

                    // Timeshift Rewind 30s
                    Button {
                        if engineMode == .timeshiftHLS {
                            seekTimeshiftRelative(-30)
                        } else {
                            enterTimeshift(seekBackSeconds: 30)
                        }
                    } label: {
                        Image(systemName: "gobackward.30")
                            .font(.app(size: 20, weight: .bold))
                            .foregroundStyle(.white)
#if !os(tvOS)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
#endif
                    }
#if os(tvOS)
                    .buttonStyle(TVPlayerTransportButtonStyle(size: 56, isPrimary: false))
#else
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)
#endif

                    // Play / Pause Toggle
                    Button {
                        togglePlayPause()
                    } label: {
                        Image(systemName: isPlaying ? "pause.fill" : "play.fill")
                            .font(.app(size: 28, weight: .bold))
                            .foregroundStyle(.white)
#if !os(tvOS)
                            .padding(18)
                            .background(Theme.Colors.accentAction.opacity(0.9), in: Circle())
                            .shadow(color: Theme.Colors.accentAction.opacity(0.5), radius: 10)
#endif
                    }
#if os(tvOS)
                    .focused($isPlayPauseFocused)
                    .buttonStyle(TVPlayerTransportButtonStyle(size: 72, isPrimary: true))
#else
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)
#endif

                    // Timeshift Forward 30s (active in Timeshift)
                    Button {
                        if engineMode == .timeshiftHLS {
                            seekTimeshiftRelative(30)
                        } else {
                            displayZapToast("Bereits an der Live-Kante")
                        }
                    } label: {
                        Image(systemName: "goforward.30")
                            .font(.app(size: 20, weight: .bold))
                            .foregroundStyle(engineMode == .timeshiftHLS ? .white : .white.opacity(0.35))
#if !os(tvOS)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
#endif
                    }
#if os(tvOS)
                    .buttonStyle(TVPlayerTransportButtonStyle(size: 56, isPrimary: false))
#else
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)
#endif
                    .disabled(engineMode != .timeshiftHLS)

                    // Next Channel
                    Button {
                        zapRelative(delta: 1)
                    } label: {
                        Image(systemName: "forward.end.fill")
                            .font(.app(size: 20, weight: .bold))
                            .foregroundStyle(.white)
#if !os(tvOS)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
#endif
                    }
#if os(tvOS)
                    .buttonStyle(TVPlayerTransportButtonStyle(size: 56, isPrimary: false))
#else
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)
#endif
                }

                // Timeshift Timeline Bar (Visible when in Timeshift mode)
                if engineMode == .timeshiftHLS {
                    TimeshiftTimelineBar(
                        currentOffset: formattedTimeshiftOffset,
                        progress: timeshiftCurrentPosition,
                        durationSeconds: timeshiftTotalDuration,
                        onSeekProgress: { fraction in
                            isTimeshiftScrubbing = true
                            timeshiftCurrentPosition = fraction
                        },
                        onCommitSeek: { fraction in
                            isTimeshiftScrubbing = false
                            commitTimeshiftSeek(progress: fraction)
                        },
                        onJumpLive: {
                            jumpToLiveEdge()
                        }
                    )
                    .padding(.horizontal, sideInset)
                    .padding(.top, 10)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }

                Spacer()

                // Bottom Stream Info Bar in Landscape
                if isLandscape {
                    HStack(spacing: 12) {
                        HStack(spacing: 6) {
                            Image(systemName: "waveform")
                                .font(.app(size: 11, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentAction)
                            Text("\(tele.audioCodec) (\(tele.audioChannels == 6 ? "5.1 Surround" : "\(tele.audioChannels) ch"))")
                                .font(.app(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.9))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.ultraThinMaterial, in: Capsule())

                        HStack(spacing: 6) {
                            Image(systemName: "tv")
                                .font(.app(size: 11, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentLive)
                            let renderMode = tele.isInterlaced ? "HW Bob" : "HW Direct"
                            let scanText = tele.videoScanSummary != "—" ? "\(tele.videoScanSummary) \(renderMode)" : "Video \(renderMode)"
                            Text(scanText)
                                .font(.app(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.9))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.ultraThinMaterial, in: Capsule())

                        Text(String(format: "%.1f Mbps", tele.tsBitrateKbps / 1000.0))
                            .font(.app(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(.white.opacity(0.7))

                        Spacer()

                        // Quick Zap Channel Drawer Button
                        Button {
                            withAnimation(.easeInOut(duration: 0.25)) {
                                showLandscapeZapBar.toggle()
                            }
                        } label: {
                            HStack(spacing: 6) {
                                Image(systemName: "list.bullet")
                                    .font(.app(size: 12, weight: .bold))
                                Text("Sender")
                                    .font(.app(size: 12, weight: .bold))
                            }
                            .foregroundStyle(.white)
#if !os(tvOS)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 6)
                            .background(Theme.Colors.accentAction.opacity(0.85), in: Capsule())
                            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
#endif
                        }
#if os(tvOS)
                        .buttonStyle(TVPlayerPillButtonStyle())
#else
                        .buttonStyle(.plain)
                        .appHoverEffect(.highlight)
#endif
                    }
                    .padding(.horizontal, sideInset)
                    .padding(.bottom, max(safeInsets.bottom, 12))
                } else {
                    // Portrait Bottom Controls: EPG Programme Pill & Prominent Senderliste Button
                    if !showPortraitDrawer {
                        VStack(spacing: 12) {
                            if let preset = presets.first(where: { $0.url == streamURLString }), !preset.epgNow.isEmpty {
                                HStack(spacing: 6) {
                                    Image(systemName: "tv")
                                        .font(.app(size: 11, weight: .bold))
                                        .foregroundStyle(Theme.Colors.accentLive)
                                    Text(preset.epgNow)
                                        .font(.app(size: 12, weight: .semibold))
                                        .foregroundStyle(.white.opacity(0.9))
                                        .lineLimit(1)
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 7)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                            }

                            Button {
                                Haptics.shared.impact(.light)
                                withAnimation(.easeInOut(duration: 0.28)) {
                                    showPortraitDrawer = true
                                }
                            } label: {
                                HStack(spacing: 8) {
                                    Image(systemName: "list.bullet")
                                        .font(.app(size: 14, weight: .bold))
                                    Text("Senderliste")
                                        .font(.app(size: 14, weight: .bold))
                                }
                                .foregroundStyle(.white)
                                .padding(.horizontal, 20)
                                .padding(.vertical, 10)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                                .shadow(color: Color.black.opacity(0.4), radius: 10, y: 4)
                            }
                            .buttonStyle(.plain)
                        }
                        .padding(.horizontal, sideInset)
                        .padding(.bottom, max(safeInsets.bottom, 24))
                    }
                }
            }
        }
    }

    // MARK: - Portrait Bottom Drawer (Quick Zap & Stats)

    private var portraitControlsDrawer: some View {
        VStack(spacing: 0) {
            // Header Bar
            HStack {
                HStack(spacing: 6) {
                    Circle()
                        .fill(Theme.Colors.accentLive)
                        .frame(width: 6, height: 6)
                    Text("SENDERLISTE")
                        .font(.app(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textPrimary)
                }

                Spacer()

                Button {
                    Haptics.shared.impact(.light)
                    withAnimation(.easeInOut(duration: 0.28)) {
                        showPortraitDrawer = false
                    }
                } label: {
                    Image(systemName: "chevron.down.circle.fill")
                        .font(.app(size: 20))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 16)
            .padding(.top, 10)
            .padding(.bottom, 6)

            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                // Channel Info Card
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 12) {
                        let activeName = presentedPreset?.name ?? currentChannelName
                        ChannelLogo(url: currentLogoURL, name: activeName, size: 44)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(activeName)
                                .font(.title3.weight(.bold))
                                .foregroundStyle(.white)

                            if let preset = presentedPreset {
                                Text(preset.epgNow)
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }

                        Spacer()

                        Button(isStreaming ? "Stoppen" : "Starten") {
                            if isStreaming {
                                Task { await coordinator.stop() }
                                isStreaming = false
                                isPlaying = false
                            } else {
                                startCurrentPreset()
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(isStreaming ? Theme.Colors.statusError : Theme.Colors.accentLive)
                    }

                    if let entry = model?.schedule[currentChannel.serviceRef]?.now, let progress = entry.progress(at: .now) {
                        PlaybackProgressView(
                            progress: progress,
                            startTime: entry.formattedStartTime,
                            endTime: entry.formattedEndTime,
                            remainingText: entry.remainingMinutes(at: .now).map { "noch \($0) Min" }
                        )
                        .padding(.top, 2)
                    }

                    // Format Status Badges (Runtime Plan Transparency)
                    HStack(spacing: 8) {
                        let plan = coordinator.playing?.runtimePlan
                        let scanBadge = plan != nil ? plan!.videoBadge : (tele.videoScanSummary != "—" ? "\(tele.videoScanSummary) \(tele.isInterlaced ? "HW Bob" : "HW Direct")" : "Video HW")
                        badgeItem(icon: "sparkles.tv", label: scanBadge, color: .green)

                        let audioBadge = plan != nil ? plan!.audioBadge : (tele.audioChannels > 0 ? "\(tele.audioCodec) \(tele.audioChannels == 6 ? "5.1" : "\(tele.audioChannels)ch")" : tele.audioCodec)
                        badgeItem(icon: "speaker.wave.3.fill", label: audioBadge, color: .blue)

                        let modeBadge = plan?.userSummary ?? "Direkt"
                        badgeItem(icon: "bolt.fill", label: modeBadge, color: .purple)

                        badgeItem(icon: "thermometer.medium", label: tele.thermalState, color: .orange)
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Quick Zap Channel Presets
                VStack(alignment: .leading, spacing: 8) {
                    Text("SENDER")
                        .font(.app(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)

                    ForEach(presets) { preset in
                        let isPresented = (presentedPreset?.serviceRef == preset.serviceRef || (presentedPreset == nil && preset.name == currentChannelName))
                        let isRequested = (coordinator.requestedServiceRef == preset.serviceRef)

                        Button {
                            switchTo(preset: preset)
                        } label: {
                            HStack(spacing: 12) {
                                ChannelLogo(url: logoURL(forPreset: preset), name: preset.name, size: 36)

                                VStack(alignment: .leading, spacing: 2) {
                                    Text(preset.name)
                                        .font(.subheadline.weight(isPresented ? .bold : .medium))
                                        .foregroundStyle(isPresented ? .white : Theme.Colors.textPrimary)

                                    Text(preset.epgNow)
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                        .lineLimit(1)
                                }

                                Spacer()

                                if isRequested {
                                    HStack(spacing: 4) {
                                        ProgressView()
                                            .controlSize(.mini)
                                            .tint(Theme.Colors.accentLive)
                                        Text("WÄRMT…")
                                            .font(.app(size: 10, weight: .bold, design: .monospaced))
                                            .foregroundStyle(Theme.Colors.accentLive)
                                    }
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                                } else if isPresented {
                                    Text("AKTIV")
                                        .font(.app(size: 10, weight: .bold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.accentLive)
                                        .padding(.horizontal, 6)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                                }
                            }
                            .padding(.vertical, 8)
                            .padding(.horizontal, 12)
                            .background(isPresented ? Color.white.opacity(0.08) : Color.clear, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Custom Stream URL Bar
                VStack(alignment: .leading, spacing: 8) {
                    Text("BENUTZERDEFINIERTE STREAM-URL")
                        .font(.app(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)

                    HStack(spacing: 8) {
                        TextField(ServerAddress.streamURLPlaceholder, text: $streamURLString)
#if os(tvOS)
                            .textFieldStyle(.plain)
#else
                            .textFieldStyle(.roundedBorder)
#endif
                            .font(.caption.monospaced())
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled(true)

                        Button("Laden") {
                            startCurrentPreset()
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(Theme.Colors.accentAction)
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .padding(12)
            }
        }
        .background(Color.black.opacity(0.85))
    }

    private func badgeItem(icon: String, label: String, color: Color) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon)
                .font(.app(size: 10, weight: .bold))
                .foregroundStyle(color)
            Text(label)
                .font(.app(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(.white.opacity(0.9))
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color.white.opacity(0.06), in: Capsule())
    }

    // MARK: - Telemetry HUD Floating Card

    private var telemetryHUDView: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                hudHeader

                if let warning = tele.validationWarning {
                    HStack(spacing: 6) {
                        Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.yellow)
                        Text(warning).font(.caption.weight(.bold)).foregroundStyle(.white)
                    }
                    .padding(8)
                    .background(Color.red.opacity(0.85), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                }

                Group {
                    // 1. VIDEO (SOURCE & BITSTREAM)
                    hudSection(title: "VIDEO (QUELL-STREAM)") {
                        let w = tele.videoWidth
                        let h = tele.videoHeight
                        let resStr = (w > 0 && h > 0) ? "\(w) × \(h)" : "Erkenne…"
                        hudRow("Auflösung", resStr)
                        hudRow("Signal", tele.videoScanSummary)
                        hudRow("Halbbild-Ordnung", tele.fieldOrder)
                        hudRow("Codec", tele.codec)
                        hudRow("TS-Bitrate", tele.tsBitrateKbps > 0 ? String(format: "%.1f Mbps", tele.tsBitrateKbps / 1000.0) : "—")
                        let hwStatus = tele.hwDecodeActive ? "VideoToolbox 🚀" : (tele.vtSessionActive ? "Init…" : "Noch nicht bestätigt")
                        hudRow("HW Decode", hwStatus, highlight: tele.hwDecodeActive)
                    }

                    // 2. FARBRAUM & SIGNAL (COLORIMETRY)
                    hudSection(title: "FARBRAUM & DYNAMIK (SIGNAL)") {
                        hudRow("Farbraum", tele.colorPrimaries, highlight: tele.colorPrimaries != "—")
                        hudRow("Dynamikumfang", tele.transferFunction, highlight: tele.isHDR)
                        hudRow("YCbCr Matrix", tele.colorMatrix)
                        hudRow("Wertebereich", tele.colorRange)
                    }

                    // 3. BILDFORMAT & GEOMETRIE (QUELLE)
                    hudSection(title: "QUELL-GEOMETRIE (BITSTREAM)") {
                        let sarStr = tele.sarSignaled ? "\(tele.sarNumerator):\(tele.sarDenominator) (signalisiert)" : "Nicht signalisiert (1:1)"
                        hudRow("SAR", sarStr, highlight: tele.sarSignaled)
                        hudRow("Quell-DAR", tele.sourceDARDescription)
                        hudRow("AFD (Active Area)", tele.afdDescription, highlight: tele.afdDescription != "—")
                    }

                    // 4. DARSTELLUNG & AUSGABE
                    hudSection(title: "DARSTELLUNG & AUSGABE") {
                        hudRow("Modus", viewPreset.rawValue, highlight: viewPreset != .standard)
                        hudRow("Ausgabe-DAR", tele.outputDARDescription)
                        hudRow("Skalierung", viewPreset.scalingMode == .fill ? "Aspect Fill (Center Crop)" : "Aspect Fit (Letterbox)")
                        hudRow("Renderpfad", presentationPath == .systemLayer ? "System Layer (AVSampleBuffer)" : "Metal Direct (CAMetalLayer)")
                        hudRow("Display Pacing", tele.fieldsSubmittedPerSec > 0 ? String(format: "%.1f fields/s", tele.fieldsSubmittedPerSec) : "—", highlight: abs(tele.fieldsSubmittedPerSec - 50.0) < 3.0)
                    }

                    // 5. AUDIO & STREAM-HEALTH
                    hudSection(title: "AUDIO & STREAM-HEALTH") {
                        let langStr = tele.audioLanguage.isEmpty || tele.audioLanguage == "und" ? "" : " [\(tele.audioLanguage.uppercased())]"
                        hudRow("Audio Format", "\(tele.audioCodec)\(langStr) \(tele.audioChannels)ch")
                        hudRow("Master Clock", tele.isAudioMasterClockActive ? "Synchronized 🟢" : "Pre-roll ⚪️", highlight: tele.isAudioMasterClockActive)
                        hudRow("Audio Lead", tele.audioLeadMs > 0 ? String(format: "%.0f ms", tele.audioLeadMs) : "—")
                        hudRow("Discontinuities", "\(tele.ptsDiscontinuities) PTS / \(tele.continuityErrors) CC", alert: tele.ptsDiscontinuities > 0 || tele.continuityErrors > 0)
                        hudRow("Drops / Errors", "\(tele.droppedFrames) drops / \(tele.decodeErrors) dec", alert: tele.droppedFrames > 0 || tele.decodeErrors > 0)
                    }

                    // 6. STARTUP & PERFORMANCE
                    hudSection(title: "PERFORMANCE & TTFP") {
                        hudRow("TTFP (Erstes Bild)", tele.ttfpTotalMs > 0 ? String(format: "%.1f ms", tele.ttfpTotalMs) : "Instant 🚀", highlight: true)
                        hudRow("Process CPU", String(format: "%.1f %%", tele.processCpuUsagePercent), highlight: tele.processCpuUsagePercent < 25.0)
                        hudRow("Footprint (Peak)", String(format: "%.1f MB (%.1f MB)", tele.memoryUsageMB, tele.peakMemoryFootprintMB))
                    }
                }
            }
            .padding(10)
        }
        .frame(maxWidth: 340, maxHeight: 420)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
        .shadow(color: Color.black.opacity(0.5), radius: 10)
    }

    // MARK: - Actions & Helpers

    private func setupPlayback() {
        AudioSessionManager.shared.configureForPlayback()
        // Watching a live stream involves no touches, so the idle timer would
        // dim and lock the screen in the middle of a programme.
        UIApplication.shared.isIdleTimerDisabled = true
        // Taken over as a whole: play and pause each do their own thing rather
        // than both landing on the toggle, and stop is claimed here instead of
        // being left pointing at the HLS player. No seek handler, so the skip
        // commands stay switched off — live has nothing to skip to until the
        // DVR path exists.
        NowPlayingManager.shared.takeOver(.init(
            play: {
                if !isPlaying {
                    if engineMode == .timeshiftHLS {
                        toggleTimeshiftPlayPause()
                    } else {
                        startCurrentPreset()
                    }
                }
            },
            pause: { if isPlaying { togglePlayPause() } },
            togglePlayPause: { togglePlayPause() },
            stop: { teardownPlayback() },
            nextChannel: { zapRelative(delta: 1) },
            previousChannel: { zapRelative(delta: -1) },
            seekRelative: { delta in
                if engineMode == .timeshiftHLS {
                    seekTimeshiftRelative(delta)
                } else if delta < 0 {
                    enterTimeshift(seekBackSeconds: abs(delta))
                }
            }
        ))
        if model?.playbackEngine == .hls {
            enterTimeshift(seekBackSeconds: 0, autoPlay: true)
        } else {
            startCurrentPreset()
        }
        scheduleControlsAutoHide()
    }

    private func teardownPlayback() {
        teardownTimeshift()
        playbackManager.stop()
        isStreaming = false
        UIApplication.shared.isIdleTimerDisabled = false
        autoHideControlsTask?.cancel()
        hideZapToastTask?.cancel()
        NowPlayingManager.shared.clear()
    }

    private func effectiveStreamURL(for rawURLString: String) -> URL? {
        guard let url = URL(string: rawURLString) else { return nil }
        let sref = url.lastPathComponent

        // No configured server means no route. Returning nil surfaces the
        // "configure a server" state; the hard-coded address that used to sit
        // here made an unconfigured build look configured.
        switch streamRouteMode {
        case .livePipeline:
            return model?.liveStreamURL(for: sref)
        case .legacySmoother:
            return model?.legacySmoothStreamURL(for: sref)
        case .direct:
            return url
        }
    }

    private func startCurrentPreset() {
        let requestedAt = CACurrentMediaTime()
        let serviceRef = URL(string: streamURLString)?.lastPathComponent ?? streamURLString

        // If the coordinator is already playing THIS EXACT channel (e.g. re-attaching from miniplayer),
        // we attach to the existing stream without re-tuning or interrupting audio.
        if coordinator.displayedServiceRef == serviceRef || coordinator.presentedServiceRef == serviceRef {
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
            return
        }

        if let ch = model?.channels.first(where: { $0.serviceRef == serviceRef || $0.name == currentChannelName }) {
            model?.recordChannelPlayback(ch)
            playbackManager.zap(to: ch)
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
            return
        }

        if streamRouteMode == .livePipeline, coordinator.canPrepare {
            Task { await coordinator.zap(to: serviceRef) }
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
            return
        }

        if let url = effectiveStreamURL(for: streamURLString) {
            Task { await coordinator.play(unprepared: url, requestedAt: requestedAt) }
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
        }
    }

    /// Publishes the entry itself, not just its rate: `updatePlaybackState` returns at
    /// its first line when nothing has been published, which is why this player's lock
    /// screen was empty and its controls inert. Resuming goes through here too, and the
    /// manager remembers the paused state from before — without that the lock screen
    /// and the watch would keep showing a pause button over running playback.
    private func announceNowPlaying() {
        NowPlayingManager.shared.updateLive(
            title: currentChannelName,
            subtitle: currentPreset?.epgNow,
            logoURL: currentPreset.flatMap { logoURL(for: $0.serviceRef) }
        )
        NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
    }

    private func switchTo(preset: ChannelPreset) {
        if model?.playbackEngine == .hls {
            teardownTimeshift()
            currentChannelName = preset.name
            streamURLString = preset.url
            viewPreset = .standard
            enterTimeshift(seekBackSeconds: 0, autoPlay: true)
            return
        }
        if engineMode == .timeshiftHLS {
            teardownTimeshift()
            engineMode = .nativeDirectLive
        }
        viewPreset = .standard
        streamURLString = preset.url
        currentChannelName = preset.name
        let requestedAt = CACurrentMediaTime()
        let serviceRef = preset.serviceRef

        if let ch = model?.channels.first(where: { $0.serviceRef == serviceRef || $0.name == preset.name }) {
            model?.recordChannelPlayback(ch)
            playbackManager.zap(to: ch)
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
            return
        }

        if streamRouteMode == .livePipeline, coordinator.canPrepare {
            Task { await coordinator.zap(to: serviceRef) }
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
            return
        }

        if let url = effectiveStreamURL(for: preset.url) {
            Task { await coordinator.play(unprepared: url, requestedAt: requestedAt) }
            isStreaming = true
            isPlaying = true
            announceNowPlaying()
        }
    }

    private func zapRelative(delta: Int) {
        guard !presets.isEmpty else { return }
        let currentIndex = presets.firstIndex(where: { isCurrentPreset($0) }) ?? 0
        var nextIndex = currentIndex + delta
        if nextIndex < 0 { nextIndex = presets.count - 1 }
        if nextIndex >= presets.count { nextIndex = 0 }
        switchTo(preset: presets[nextIndex])
    }

    // MARK: - Auto-Timeshift-Weiche (Handover Live TS ↔ HLS Timeshift)

    private var formattedTimeshiftOffset: String {
        let secs = Int(timeshiftOffsetSeconds)
        let hours = secs / 3600
        let minutes = (secs % 3600) / 60
        let seconds = secs % 60
        if hours > 0 {
            return String(format: "-%02d:%02d:%02d", hours, minutes, seconds)
        } else {
            return String(format: "-%02d:%02d", minutes, seconds)
        }
    }

    private func performInitialTimeshiftSeek(player: AVPlayer, seekBackSeconds: Double) async {
        let maxAttempts = 30
        for _ in 0..<maxAttempts {
            if let item = player.currentItem,
               item.status == .readyToPlay,
               let range = item.seekableTimeRanges.last?.timeRangeValue,
               range.duration.seconds > 0 {
                let liveEnd = range.end.seconds
                let target = max(range.start.seconds, liveEnd - seekBackSeconds)
                await withCheckedContinuation { continuation in
                    player.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero) { _ in
                        continuation.resume()
                    }
                }
                player.play()
                self.isPlaying = true
                self.isTimeshiftLoading = false
                NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
                displayZapToast("◀◀ Timeshift -\(Int(seekBackSeconds))s")
                return
            }
            try? await Task.sleep(nanoseconds: 100_000_000)
            if self.engineMode != .timeshiftHLS || self.timeshiftPlayer !== player {
                return
            }
        }
        player.play()
        self.isPlaying = true
        self.isTimeshiftLoading = false
        NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
        displayZapToast("◀◀ Timeshift -\(Int(seekBackSeconds))s")
    }

    private func enterTimeshift(seekBackSeconds: Double = 0, autoPlay: Bool = false) {
        guard let model else {
            displayZapToast("Timeshift nicht verfügbar")
            return
        }
        let ch = currentChannel
        guard !ch.serviceRef.isEmpty else {
            displayZapToast("Sender nicht verfügbar")
            return
        }

        Haptics.shared.impact(.medium)
        isTimeshiftLoading = true
        engineMode = .timeshiftHLS
        isPlaying = autoPlay || (seekBackSeconds > 0)

        // Stop the live direct pipeline presentation gracefully
        let stopping = coordinator.playing
        Task { await coordinator.stop() }
        stopping?.notePlaybackStateChanged()

        Task { @MainActor in
            do {
                if let stream = try await model.startTimeshift(for: ch) {
                    let player = PlayerAssetLoader.makeLivePlayer(for: stream, channel: ch, nowNext: model.schedule[ch.serviceRef])
                    self.timeshiftPlayer = player
                    self.attachTimeshiftObserver(player: player)

                    if seekBackSeconds > 0 {
                        await self.performInitialTimeshiftSeek(player: player, seekBackSeconds: seekBackSeconds)
                    } else if autoPlay {
                        player.play()
                        self.isPlaying = true
                        self.isTimeshiftLoading = false
                        NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
                        displayZapToast("▶ Live (HLS)")
                    } else {
                        player.pause()
                        self.isPlaying = false
                        self.isTimeshiftLoading = false
                        NowPlayingManager.shared.updatePlaybackState(isPlaying: false)
                        displayZapToast("❚❚ Timeshift Pausiert")
                    }
                } else {
                    isTimeshiftLoading = false
                    displayZapToast("HLS-Wiedergabe konnte nicht gestartet werden")
                    jumpToLiveEdge()
                }
            } catch {
                isTimeshiftLoading = false
                displayZapToast("Fehler bei HLS-Stream: \(error.localizedDescription)")
                jumpToLiveEdge()
            }
        }
    }

    private func jumpToLiveEdge() {
        Haptics.shared.notification(.success)
        if model?.playbackEngine == .hls {
            if let item = timeshiftPlayer?.currentItem,
               let range = item.seekableTimeRanges.last?.timeRangeValue {
                let liveEnd = range.end.seconds
                timeshiftPlayer?.seek(to: CMTime(seconds: liveEnd, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
                timeshiftPlayer?.play()
                self.isPlaying = true
                self.timeshiftOffsetSeconds = 0
                displayZapToast("▶ Live-Kante (HLS)")
                return
            }
        }
        teardownTimeshift()
        engineMode = .nativeDirectLive
        startCurrentPreset()
        NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
        displayZapToast("▶ Live-Kante (Native TS)")
    }

    private func teardownTimeshift() {
        if let token = timeshiftObserverToken {
            timeshiftPlayer?.removeTimeObserver(token)
            timeshiftObserverToken = nil
        }
        timeshiftPlayer?.pause()
        timeshiftPlayer = nil
        isTimeshiftLoading = false
        timeshiftOffsetSeconds = 0
        Task { await model?.stopTimeshift() }
    }

    private func attachTimeshiftObserver(player: AVPlayer) {
        if let token = timeshiftObserverToken {
            timeshiftPlayer?.removeTimeObserver(token)
            timeshiftObserverToken = nil
        }
        let interval = CMTime(seconds: 0.5, preferredTimescale: 600)
        timeshiftObserverToken = player.addPeriodicTimeObserver(forInterval: interval, queue: .main) { [weak player] _ in
            guard let player, let item = player.currentItem else { return }
            guard let lastRange = item.seekableTimeRanges.last?.timeRangeValue else { return }
            let liveEnd = lastRange.end.seconds
            let start = lastRange.start.seconds
            let current = player.currentTime().seconds

            if liveEnd.isFinite && current.isFinite && start.isFinite {
                let offset = max(0, liveEnd - current)
                let duration = max(1.0, liveEnd - start)
                let progress = max(0.0, min(1.0, (current - start) / duration))

                Task { @MainActor in
                    self.timeshiftOffsetSeconds = offset
                    self.timeshiftTotalDuration = duration
                    if !self.isTimeshiftScrubbing {
                        self.timeshiftCurrentPosition = progress
                    }

                    // Auto-return to Native Direct TS if caught up to live edge during playback
                    if self.isPlaying && !self.isTimeshiftLoading && !self.isTimeshiftScrubbing && offset < 1.5 {
                        self.jumpToLiveEdge()
                    }
                }
            }
        }
    }

    private func toggleTimeshiftPlayPause() {
        guard let player = timeshiftPlayer else {
            enterTimeshift()
            return
        }
        if isPlaying {
            player.pause()
            isPlaying = false
            NowPlayingManager.shared.updatePlaybackState(isPlaying: false)
            displayZapToast("❚❚ Pausiert")
        } else {
            player.play()
            isPlaying = true
            NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
            displayZapToast("▶ Fortsetzen")
        }
    }

    private func seekTimeshiftRelative(_ seconds: Double) {
        guard let player = timeshiftPlayer, let item = player.currentItem else { return }
        guard let range = item.seekableTimeRanges.last?.timeRangeValue else { return }
        let current = player.currentTime().seconds
        let liveEnd = range.end.seconds
        let target = max(range.start.seconds, min(liveEnd, current + seconds))

        if (liveEnd - target) <= 2.5 && seconds > 0 {
            jumpToLiveEdge()
            return
        }

        player.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        displayZapToast(seconds > 0 ? "▶▶ +\(Int(seconds))s" : "◀◀ \(Int(seconds))s")
    }

    private func commitTimeshiftSeek(progress: Double) {
        guard let player = timeshiftPlayer, let item = player.currentItem else { return }
        guard let lastRange = item.seekableTimeRanges.last?.timeRangeValue else { return }
        let start = lastRange.start.seconds
        let duration = max(1.0, lastRange.end.seconds - start)
        let target = start + (duration * progress)
        let liveEnd = lastRange.end.seconds

        if (liveEnd - target) <= 2.5 {
            jumpToLiveEdge()
            return
        }

        player.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        Haptics.shared.impact(.light)
    }

    private func togglePlayPause() {
        if engineMode == .timeshiftHLS {
            toggleTimeshiftPlayPause()
        } else {
            if isPlaying {
                enterTimeshift(seekBackSeconds: 0)
            } else {
                startCurrentPreset()
            }
        }
    }

    private func cycleViewPreset() {
        Haptics.shared.impact(.light)
        viewPreset = viewPreset.next(includeAdvanced: model?.enableAdvancedAspectRatios ?? false)
        displayZapToast("Bildformat: \(viewPreset.rawValue)")
    }

#if os(iOS)
    private func handleKeyboardToggleZapDrawer(isLandscape: Bool) {
        if isLandscape {
            withAnimation(.easeInOut(duration: 0.25)) {
                showLandscapeZapBar.toggle()
            }
        } else {
            withAnimation(.easeInOut(duration: 0.25)) {
                showPortraitDrawer.toggle()
            }
        }
        scheduleControlsAutoHide()
    }

    private func handleKeyboardEscape() {
        if showLandscapeZapBar {
            withAnimation(.easeInOut(duration: 0.2)) {
                showLandscapeZapBar = false
            }
        } else if showPortraitDrawer {
            withAnimation(.easeInOut(duration: 0.2)) {
                showPortraitDrawer = false
            }
        } else if showHUD {
            withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                showHUD = false
            }
        } else if showControls {
            withAnimation(.easeInOut(duration: 0.25)) {
                showControls = false
            }
        } else {
            closePlayer()
        }
    }
#endif

    private func displayZapToast(_ message: String) {
        hideZapToastTask?.cancel()
        withAnimation(.spring(response: 0.25, dampingFraction: 0.8)) {
            zapToast = message
        }
        hideZapToastTask = Task {
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.25)) {
                zapToast = nil
            }
        }
    }

    private func scheduleControlsAutoHide() {
        guard !showLandscapeZapBar && !showHUD && !showPortraitDrawer else { return }
#if DEBUG
        if CommandLine.arguments.contains("--demo-mode") || CommandLine.arguments.contains("--uitesting") {
            return
        }
#endif
        autoHideControlsTask?.cancel()
        autoHideControlsTask = Task {
            try? await Task.sleep(for: .seconds(4))
            guard !Task.isCancelled else { return }
            withAnimation(.easeInOut(duration: 0.3)) {
                showControls = false
            }
        }
    }

    private var hudHeader: some View {
        let isInterlaced = tele.isInterlaced
        let h = tele.videoHeight
        let srcFps = tele.sourceFrameRate
        let fps = Int(round(srcFps > 0 ? (isInterlaced ? srcFps * 2 : srcFps) : 50))
        let title = (h > 0) ? "⚡️ Native \(h)\(isInterlaced ? "i" : "p")\(fps) Video Telemetry" : "⚡️ Native Video Telemetry"
        return HStack {
            Text(title)
                .font(.subheadline.weight(.bold))
                .foregroundStyle(.white)
            Spacer()
            Circle()
                .fill(isStreaming ? Color.green : Color.gray)
                .frame(width: 8, height: 8)
        }
    }

    private func hudSection<Content: View>(title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.app(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(Color.yellow)
            content()
        }
        .padding(6)
        .background(Color.black.opacity(0.4), in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }

    private func hudRow(_ label: String, _ value: String, highlight: Bool = false, alert: Bool = false) -> some View {
        HStack {
            Text(label)
                .font(.app(size: 10, design: .monospaced))
                .foregroundStyle(Color.gray)
            Spacer()
            Text(value)
                .font(.app(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(alert ? Color.red : (highlight ? Color.green : Color.white))
        }
    }
}

struct MetalVideoStageView: UIViewRepresentable {
    /// The readouts of whichever session is on screen, or none while nothing is.
    ///
    /// Not the session itself: the stage draws what the surface is given, and which
    /// session that is belongs to the presentation context.
    let telemetry: StreamTelemetry?
    let presenter: SystemVideoPresenter
    let presentationContext: PresentationContext
    let presentationPath: MetalVideoView.PresentationPath
    let scalingMode: VideoScalingMode
    let aspectRatioOverride: VideoAspectRatio

    func makeUIView(context: Context) -> MetalVideoView {
        let view = MetalVideoView(frame: .zero)
        view.telemetry = telemetry
        view.scalingMode = scalingMode
        view.aspectRatioOverride = aspectRatioOverride

        // The context is given the view, and hands it to whichever session owns the
        // surface. Sessions are never wired to it here: with a channel prepared beside
        // one playing, the stage cannot know which of them is the visible one.
        presentationContext.setRenderView(view)

        // Presenting through AVFoundation instead of our own drawable. The Metal
        // view keeps doing the decode-side work — reorder, field scheduling and
        // the deinterlace pass — and its output goes into the display layer,
        // which is hosted on top of it.
        view.systemPresenter = presenter
        view.presentationPath = presentationPath
        presenter.scalingMode = scalingMode
        presenter.displayLayer.frame = view.bounds
        view.layer.addSublayer(presenter.displayLayer)
        presenter.enablePictureInPicture()

        return view
    }

    func updateUIView(_ uiView: MetalVideoView, context: Context) {
        uiView.telemetry = telemetry
        uiView.presentationPath = presentationPath
        uiView.scalingMode = scalingMode
        uiView.aspectRatioOverride = aspectRatioOverride
        presenter.scalingMode = scalingMode
        // The layer is not managed by Auto Layout, so it has to follow the view
        // itself. Without this it keeps its size across a rotation and the
        // picture stays letterboxed at the old aspect.
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        presenter.displayLayer.frame = uiView.bounds
        CATransaction.commit()
    }
}

/// Shown in place of the picture when the channel's video format cannot be
/// assembled on this path.
///
/// Direct playback carries the broadcast untouched, which is its whole point
/// and also its limit: an MPEG-2 or HEVC service arrives intact and unusable,
/// because the only assembler here reads H.264. The viewer sees a black screen
/// and has no way to know the channel is fine and the route is wrong — so the
/// notice names the format, and names the setting that fixes it.
struct UnplayableFormatNotice: View {

    let formatDescription: String
    let channelName: String

    var body: some View {
        ZStack {
            Color.black.opacity(0.92)

            VStack(spacing: 14) {
                Image(systemName: "tv.slash")
                    .font(.app(size: 40, weight: .light))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Text("\(channelName) sendet in einem Format, das die Direktwiedergabe nicht darstellen kann")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.white)
                    .multilineTextAlignment(.center)

                Text("Der Sender überträgt \(formatDescription). Dieses Gerät kann das bei Direktwiedergabe nicht dekodieren.")
                    .font(.footnote)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .multilineTextAlignment(.center)

                Text("Stelle unter Einstellungen → Wiedergabe-Art auf „Über den Server“ um, dann läuft dieser Sender.")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(Theme.Colors.accentAction)
                    .multilineTextAlignment(.center)
            }
            .padding(28)
        }
        .allowsHitTesting(false)
    }
}

// MARK: - Timeshift Timeline Bar

struct TimeshiftTimelineBar: View {
    let currentOffset: String
    let progress: Double // 0.0 to 1.0
    let durationSeconds: Double
    let onSeekProgress: (Double) -> Void
    let onCommitSeek: (Double) -> Void
    let onJumpLive: () -> Void

    @State private var dragProgress: Double?

    private var displayProgress: Double {
        dragProgress ?? progress
    }

    var body: some View {
        VStack(spacing: 6) {
            HStack {
                HStack(spacing: 5) {
                    Circle()
                        .fill(Theme.Colors.statusWarning)
                        .frame(width: 6, height: 6)
                    Text("TIMESHIFT")
                        .font(.app(size: 10, weight: .black, design: .monospaced))
                        .foregroundStyle(Theme.Colors.statusWarning)
                    Text(currentOffset)
                        .font(.app(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(.white)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .background(.ultraThinMaterial, in: Capsule())

                Spacer()

                Button(action: onJumpLive) {
                    HStack(spacing: 4) {
                        PulsingLiveDot(size: 5)
                        Text("Zur Live-Kante")
                            .font(.app(size: 10, weight: .bold))
                    }
                    .padding(.horizontal, 9)
                    .padding(.vertical, 4)
                    .background(Theme.Colors.accentLive, in: Capsule())
                    .foregroundStyle(Color.black)
                }
                .buttonStyle(.plain)
            }

            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(Color.white.opacity(0.2))
                        .frame(height: 4)

                    Capsule()
                        .fill(
                            LinearGradient(
                                colors: [Theme.Colors.statusWarning, Theme.Colors.accentLive],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(displayProgress))), height: 4)

                    Circle()
                        .fill(Color.white)
                        .frame(width: 14, height: 14)
                        .shadow(color: Theme.Colors.accentLive.opacity(0.8), radius: 4)
                        .offset(x: max(0, min(geo.size.width - 14, geo.size.width * CGFloat(displayProgress) - 7)))
                }
                .contentShape(Rectangle())
#if os(iOS)
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            let fraction = max(0.0, min(1.0, value.location.x / geo.size.width))
                            dragProgress = fraction
                            onSeekProgress(fraction)
                        }
                        .onEnded { value in
                            let fraction = max(0.0, min(1.0, value.location.x / geo.size.width))
                            dragProgress = nil
                            onCommitSeek(fraction)
                        }
                )
#endif
            }
            .frame(height: 14)
        }
        .padding(12)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
        .shadow(color: Color.black.opacity(0.3), radius: 8)
    }
}

#if os(tvOS)
struct TVPlayerControlButtonStyle: ButtonStyle {
    var size: CGFloat = 48
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: size, height: size)
            .background(
                Circle()
                    .fill(isFocused ? Theme.Colors.accentAction : Color.black.opacity(0.6))
            )
            .overlay(
                Circle()
                    .strokeBorder(isFocused ? Color.white : Theme.Colors.borderSubtle, lineWidth: isFocused ? 3 : 1)
            )
            .scaleEffect(isFocused ? 1.18 : 1.0)
            .shadow(color: isFocused ? Theme.Colors.accentAction.opacity(0.6) : Color.black.opacity(0.4), radius: isFocused ? 14 : 6)
            .animation(.easeOut(duration: 0.16), value: isFocused)
    }
}

struct TVPlayerPillButtonStyle: ButtonStyle {
    var isActive: Bool = false
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                Capsule()
                    .fill(isFocused ? Theme.Colors.accentAction : (isActive ? Theme.Colors.surfaceElevated : Color.black.opacity(0.6)))
            )
            .overlay(
                Capsule()
                    .strokeBorder(isFocused ? Color.white : Theme.Colors.borderSubtle, lineWidth: isFocused ? 2.5 : 1)
            )
            .scaleEffect(isFocused ? 1.10 : 1.0)
            .shadow(color: isFocused ? Theme.Colors.accentAction.opacity(0.6) : Color.black.opacity(0.4), radius: isFocused ? 12 : 4)
            .animation(.easeOut(duration: 0.16), value: isFocused)
    }
}

struct TVPlayerTransportButtonStyle: ButtonStyle {
    var size: CGFloat = 56
    var isPrimary: Bool = false
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: size, height: size)
            .background(
                Circle()
                    .fill(isFocused ? Color.white : (isPrimary ? Theme.Colors.accentAction.opacity(0.9) : Color.black.opacity(0.55)))
            )
            .overlay(
                Circle()
                    .strokeBorder(isFocused ? Color.white : Theme.Colors.borderSubtle, lineWidth: isFocused ? 3 : 1)
            )
            .foregroundStyle(isFocused ? Color.black : Color.white)
            .scaleEffect(isFocused ? 1.2 : 1.0)
            .shadow(color: isFocused ? Color.white.opacity(0.4) : Color.black.opacity(0.4), radius: isFocused ? 16 : 6)
            .animation(.easeOut(duration: 0.16), value: isFocused)
    }
}
#endif

#if os(iOS)
private struct LivePlayerKeyboardShortcuts: View {
    let isLandscape: Bool
    let engineMode: LivePlayerScreen.PlaybackEngineMode
    let presentationPath: MetalVideoView.PresentationPath
    let togglePlayPause: () -> Void
    let zapRelative: (Int) -> Void
    let seekTimeshiftRelative: (Double) -> Void
    let enterTimeshift: (Double) -> Void
    let displayZapToast: (String) -> Void
    let toggleZapDrawer: (Bool) -> Void
    let cycleViewPreset: () -> Void
    let toggleHUD: () -> Void
    let startPiP: () -> Void
    let handleEscape: () -> Void

    var body: some View {
        Group {
            Button("") { togglePlayPause() }
                .keyboardShortcut(.space, modifiers: [])

            Button("") { zapRelative(1) }
                .keyboardShortcut(.upArrow, modifiers: [])

            Button("") { zapRelative(-1) }
                .keyboardShortcut(.downArrow, modifiers: [])

            Button("") {
                if engineMode == .timeshiftHLS {
                    seekTimeshiftRelative(-30)
                } else {
                    enterTimeshift(30)
                }
            }
            .keyboardShortcut(.leftArrow, modifiers: [])

            Button("") {
                if engineMode == .timeshiftHLS {
                    seekTimeshiftRelative(30)
                } else {
                    displayZapToast("Bereits an der Live-Kante")
                }
            }
            .keyboardShortcut(.rightArrow, modifiers: [])

            Button("") { toggleZapDrawer(isLandscape) }
                .keyboardShortcut("z", modifiers: [])

            Button("") { cycleViewPreset() }
                .keyboardShortcut("f", modifiers: [])

            Button("") { toggleHUD() }
                .keyboardShortcut("i", modifiers: [])

            Button("") {
                if presentationPath == .systemLayer {
                    startPiP()
                }
            }
            .keyboardShortcut("p", modifiers: [])

            Button("") { handleEscape() }
                .keyboardShortcut(.escape, modifiers: [])

            Button("") { handleEscape() }
                .keyboardShortcut(".", modifiers: .command)
        }
        .frame(width: 0, height: 0)
        .opacity(0)
        .onReceive(NotificationCenter.default.publisher(for: .playerTogglePlayPause)) { _ in
            togglePlayPause()
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerZapNext)) { _ in
            zapRelative(1)
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerZapPrevious)) { _ in
            zapRelative(-1)
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerSeekBackward)) { _ in
            if engineMode == .timeshiftHLS {
                seekTimeshiftRelative(-30)
            } else {
                enterTimeshift(30)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerSeekForward)) { _ in
            if engineMode == .timeshiftHLS {
                seekTimeshiftRelative(30)
            } else {
                displayZapToast("Bereits an der Live-Kante")
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerToggleZapDrawer)) { _ in
            toggleZapDrawer(isLandscape)
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerCycleAspect)) { _ in
            cycleViewPreset()
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerToggleHUD)) { _ in
            toggleHUD()
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerStartPiP)) { _ in
            if presentationPath == .systemLayer {
                startPiP()
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .playerClose)) { _ in
            handleEscape()
        }
    }
}
#endif
