// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import UIKit

/// SwiftUI screen to test and benchmark the Phase 1 1080i50 $\rightarrow$ 1080p50 VideoToolbox + Metal Vertical Slice.
public struct TestTSPlayerScreen: View {

    @Environment(\.dismiss) private var dismiss
    /// The catalogue, when the caller has one.
    private let model: AppModel?
    @ObservedObject private var playbackManager: PlaybackManager
    private var coordinator: ZapCoordinator { playbackManager.coordinator }

    /// The readouts of the channel actually on screen.
    private var tele: TelemetryValues { coordinator.playing?.telemetry.display ?? TelemetryValues() }
    @State private var streamURLString: String = "http://10.10.55.64:8001/1:0:19:11:6:85:C00000:0:0:0:"
    @State private var currentChannelName: String = "Sky Sport F1 HD"
    private enum StreamRouteMode: String, CaseIterable {
        case livePipeline = "LIVE (v3 Ingest)"
        case direct = "DIRECT (Vu+:8001)"
        case legacySmoother = "SMOOTHER (legacy)"
    }
    @State private var streamRouteMode: StreamRouteMode = .livePipeline
    @State private var isStreaming: Bool = false
    @State private var isPlaying: Bool = true
    @State private var showHUD: Bool = false
    @State private var presentationPath: MetalVideoView.PresentationPath = .systemLayer
    @State private var viewPreset: VideoViewPreset = .standard
    @State private var showControls: Bool = true
    @State private var showLandscapeZapBar: Bool = false
    @State private var autoHideControlsTask: Task<Void, Never>?
    @State private var zapToast: String?
    @State private var hideZapToastTask: Task<Void, Never>?
    @State private var currentSubtitleImage: CGImage?

    private struct ChannelPreset: Identifiable, Hashable {
        var id: String { serviceRef }
        let name: String
        let serviceRef: String
        let url: String
        let epgNow: String
        let category: String
    }

    private static let labPresets: [ChannelPreset] = [
        ChannelPreset(name: "ORF 1 HD", serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:132F:3EF:1:C00000:0:0:0:", epgNow: "ORF 1 HD Live Feed", category: "Vollprogramm"),
        ChannelPreset(name: "Sky Sport F1 HD", serviceRef: "1:0:19:11:6:85:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:11:6:85:C00000:0:0:0:", epgNow: "Formel 1: GP Vorberichte & Live-Session", category: "Sport"),
        ChannelPreset(name: "Sky Sport Top Event HD", serviceRef: "1:0:19:81:6:85:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:81:6:85:C00000:0:0:0:", epgNow: "Top-Event Highlights & Analysen", category: "Sport"),
        ChannelPreset(name: "Sky Sport Bundesliga HD", serviceRef: "1:0:19:69:C:85:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:69:C:85:C00000:0:0:0:", epgNow: "Bundesliga Konferenz / Live", category: "Sport"),
        ChannelPreset(name: "Sky Sport Premier League HD", serviceRef: "1:0:19:91:4:85:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:91:4:85:C00000:0:0:0:", epgNow: "Premier League Live Match", category: "Sport"),
        ChannelPreset(name: "PULS 24 HD", serviceRef: "1:0:19:14B8:407:1:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:14B8:407:1:C00000:0:0:0:", epgNow: "PULS 24 News Live", category: "News"),
        ChannelPreset(name: "ZDF HD", serviceRef: "1:0:19:2B66:3F3:1:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:2B66:3F3:1:C00000:0:0:0:", epgNow: "heute journal / Magazin", category: "Vollprogramm"),
        ChannelPreset(name: "Das Erste HD", serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", url: "http://10.10.55.64:8001/1:0:19:283D:3FB:1:C00000:0:0:0:", epgNow: "Tagesschau / Reportage", category: "Vollprogramm")
    ]

    init(model: AppModel? = nil, playbackManager: PlaybackManager? = nil, channel: Channel? = nil) {
        self.model = model
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
        if let initialURL, let channel {
            _streamURLString = State(initialValue: initialURL)
            _currentChannelName = State(initialValue: channel.name)
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
        return nil
    }

    private var currentChannel: Channel {
        let name = presentedPreset?.name ?? currentChannelName
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
            let url = model?.directStreamURL(for: channel)?.absoluteString
                ?? model?.liveStreamURL(for: channel.serviceRef)?.absoluteString
                ?? "http://10.10.55.14:8089/api/v3/stream/live/\(channel.serviceRef)"
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
        playbackManager.minimize()
        dismiss()
    }

    /// The channels this screen can tune, newest EPG title included.
    ///
    /// Real catalogue when there is one — this screen is a normal player now,
    /// not only a bench — and the hard-coded services only when there is not.
    private var presets: [ChannelPreset] {
        guard let model, !model.channels.isEmpty else { return Self.labPresets }
        let catalogue = model.filteredChannels.isEmpty ? model.channels : model.filteredChannels
        let live = catalogue.map { channel -> ChannelPreset in
            let url = model.directStreamURL(for: channel)?.absoluteString
                ?? model.liveStreamURL(for: channel.serviceRef)?.absoluteString
                ?? "http://10.10.55.14:8089/api/v3/stream/live/\(channel.serviceRef)"
            return ChannelPreset(
                name: channel.name,
                serviceRef: channel.serviceRef,
                url: url,
                epgNow: model.schedule[channel.serviceRef]?.now?.title ?? "",
                category: ""
            )
        }
        return live.isEmpty ? Self.labPresets : live
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

                        // 1b. Synchronized Native DVB Subtitle Overlay
                        if let subImage = currentSubtitleImage {
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

                        // 3. On-Screen Display Controls & Buttons
                        if showControls && !showLandscapeZapBar {
                            videoOverlayControls(isLandscape: isLandscape, safeInsets: geometry.safeAreaInsets)
                                .transition(.opacity)
                        }

                        // 4. Floating Telemetry Inspector Modal
                        if showHUD {
                            telemetryHUDView
                                .padding(.top, isLandscape ? 48 : max(geometry.safeAreaInsets.top, 8) + 40)
                                .padding(.leading, max(geometry.safeAreaInsets.leading, 12))
                                .transition(.scale(scale: 0.9).combined(with: .opacity))
                        }

                        // 5. Landscape Quick-Zap Channel Carousel
                        if isLandscape && showLandscapeZapBar {
                            VStack {
                                Spacer()
                                LandscapeQuickZapBar(
                                    channels: model?.filteredChannels.isEmpty == false ? (model?.filteredChannels ?? []) : (model?.channels ?? []),
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
                        } else {
                            withAnimation(.easeInOut(duration: 0.2)) {
                                showControls.toggle()
                            }
                            if showControls {
                                scheduleControlsAutoHide()
                            }
                        }
                    }
                    .gesture(
                        DragGesture(minimumDistance: 40)
                            .onEnded { value in
                                if value.translation.width < -50 {
                                    Haptics.shared.impact(.medium)
                                    zapRelative(delta: 1)
                                } else if value.translation.width > 50 {
                                    Haptics.shared.impact(.medium)
                                    zapRelative(delta: -1)
                                }
                            }
                    )
                    .overlay(alignment: .topTrailing) {
                        if let requested = requestedPreset {
                            HStack(spacing: 8) {
                                ProgressView()
                                    .progressViewStyle(CircularProgressViewStyle(tint: .white))
                                    .scaleEffect(0.75)
                                    .frame(width: 14, height: 14)

                                VStack(alignment: .leading, spacing: 1) {
                                    Text(requested.name)
                                        .font(.system(size: 13, weight: .semibold))
                                        .foregroundStyle(.white)
                                    Text("Wird vorbereitet…")
                                        .font(.system(size: 10, weight: .regular))
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
                        portraitControlsDrawer
                    }
                }
            }
            // Real fullscreen, not merely an edge-to-edge video frame.
            .statusBarHidden(isLandscape)
            .persistentSystemOverlays(isLandscape ? .hidden : .automatic)
        }
        .onAppear {
            setupPlayback()
        }
        .onDisappear {
            if playbackManager.presentationMode == .hidden {
                teardownPlayback()
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
            announceNowPlaying()
        }
        .onChange(of: coordinator.phase) { _, newPhase in
            switch newPhase {
            case .failed(let serviceRef, let reason):
                let name = presets.first(where: { $0.serviceRef == serviceRef })?.name ?? serviceRef
                displayZapToast("\(name) konnte nicht geladen werden (\(reason))")
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
                    // 1. Dismiss Button (44x44 pt hit target)
                    Button {
                        closePlayer()
                    } label: {
                        Image(systemName: isLandscape ? "xmark.circle.fill" : "chevron.down")
                            .font(.system(size: isLandscape ? 20 : 14, weight: .bold))
                            .foregroundStyle(.white)
                            .frame(width: 36, height: 36)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())

                    // 2. Channel Info (Compact & Non-overflowing)
                    ChannelLogo(url: currentLogoURL, name: currentChannelName, size: isLandscape ? 32 : 28)

                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: 5) {
                            if let num = currentChannel.number {
                                Text(num)
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(Theme.Colors.accentAction.opacity(0.2), in: RoundedRectangle(cornerRadius: 3, style: .continuous))
                            }

                            Text(currentChannelName)
                                .font(.system(size: 13, weight: .bold))
                                .foregroundStyle(.white)
                                .lineLimit(1)

                            let liveTag = tele.videoScanSummary != "—" ? "LIVE \(tele.videoScanSummary)" : "LIVE"
                            Text(liveTag)
                                .font(.system(size: 9, weight: .black, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 4)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.accentLive.opacity(0.2), in: RoundedRectangle(cornerRadius: 3, style: .continuous))
                        }

                        if let preset = presets.first(where: { $0.url == streamURLString }), !preset.epgNow.isEmpty {
                            Text(preset.epgNow)
                                .font(.system(size: 10, weight: .medium))
                                .foregroundStyle(.white.opacity(0.8))
                                .lineLimit(1)
                        }
                    }

                    Spacer(minLength: 4)

                    // 3. Aspect Ratio Preset Button (Primary Control, 44pt hit target)
                    Button {
                        cycleViewPreset()
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: viewPreset.scalingMode == .fill ? "arrow.up.left.and.arrow.down.right" : "aspectratio")
                                .font(.system(size: 11, weight: .bold))
                            Text(viewPreset.shortLabel)
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(.white)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 6)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())

                    if isLandscape {
                        // 4. Picture in Picture Button (Landscape direct access, 44x44 hitbox)
                        Button {
                            Haptics.shared.impact(.light)
                            coordinator.surface.startPictureInPicture()
                        } label: {
                            Image(systemName: "pip.enter")
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(.white)
                                .frame(width: 36, height: 36)
                                .background(.ultraThinMaterial, in: Circle())
                                .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                        }
                        .buttonStyle(.plain)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
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
                                Label("Tonspuren (\(playing.availableAudioTracks.count))", systemImage: "waveform")
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
                                showHUD ? "Stream-Info ausblenden" : "Stream-Info (Inspector)",
                                systemImage: showHUD ? "chart.bar.fill" : "chart.bar"
                            )
                        }

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
                        Menu("Stream-Routing (Labor)") {
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
                        Image(systemName: showHUD ? "ellipsis.circle.fill" : "ellipsis.circle")
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(showHUD ? Theme.Colors.accentLive : Color.white)
                            .frame(width: 36, height: 36)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(showHUD ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())
                }
                .padding(.horizontal, sideInset)
                .padding(.top, isLandscape ? 12 : max(safeInsets.top, 8))

                Spacer()

                // Center Transport Controls
                HStack(spacing: 36) {
                    // Previous Channel
                    Button {
                        zapRelative(delta: -1)
                    } label: {
                        Image(systemName: "backward.end.fill")
                            .font(.system(size: 22, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(13)
                            .background(.ultraThinMaterial, in: Circle())
                    }
                    .buttonStyle(.plain)

                    // Play / Pause Toggle
                    Button {
                        togglePlayPause()
                    } label: {
                        Image(systemName: isPlaying ? "pause.fill" : "play.fill")
                            .font(.system(size: 30, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(18)
                            .background(Theme.Colors.accentAction.opacity(0.9), in: Circle())
                            .shadow(color: Theme.Colors.accentAction.opacity(0.5), radius: 10)
                    }
                    .buttonStyle(.plain)

                    // Next Channel
                    Button {
                        zapRelative(delta: 1)
                    } label: {
                        Image(systemName: "forward.end.fill")
                            .font(.system(size: 22, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(13)
                            .background(.ultraThinMaterial, in: Circle())
                    }
                    .buttonStyle(.plain)
                }

                Spacer()

                // Bottom Stream Info Bar in Landscape
                if isLandscape {
                    HStack(spacing: 12) {
                        HStack(spacing: 6) {
                            Image(systemName: "waveform")
                                .font(.system(size: 11, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentAction)
                            Text("\(tele.audioCodec) (\(tele.audioChannels == 6 ? "5.1 Surround" : "\(tele.audioChannels) ch"))")
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.9))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.ultraThinMaterial, in: Capsule())

                        HStack(spacing: 6) {
                            Image(systemName: "tv")
                                .font(.system(size: 11, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentLive)
                            let renderMode = tele.isInterlaced ? "HW Bob" : "HW Direct"
                            let scanText = tele.videoScanSummary != "—" ? "\(tele.videoScanSummary) \(renderMode)" : "Video \(renderMode)"
                            Text(scanText)
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.9))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.ultraThinMaterial, in: Capsule())

                        Text(String(format: "%.1f Mbps", tele.tsBitrateKbps / 1000.0))
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
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
                                    .font(.system(size: 12, weight: .bold))
                                Text("Sender")
                                    .font(.system(size: 12, weight: .bold))
                            }
                            .foregroundStyle(.white)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 6)
                            .background(Theme.Colors.accentAction.opacity(0.85), in: Capsule())
                            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                        }
                        .buttonStyle(.plain)
                    }
                    .padding(.horizontal, sideInset)
                    .padding(.bottom, max(safeInsets.bottom, 12))
                }
            }
        }
    }

    // MARK: - Portrait Bottom Drawer (Quick Zap & Stats)

    private var portraitControlsDrawer: some View {
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
                .background(.ultraThinMaterial)
                .cornerRadius(14)
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Quick Zap Channel Presets
                VStack(alignment: .leading, spacing: 8) {
                    Text("SENDER")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
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
                                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                                            .foregroundStyle(Theme.Colors.accentLive)
                                    }
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                                } else if isPresented {
                                    Text("AKTIV")
                                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.accentLive)
                                        .padding(.horizontal, 6)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                                }
                            }
                            .padding(.vertical, 8)
                            .padding(.horizontal, 12)
                            .background(isPresented ? Color.white.opacity(0.08) : Color.clear)
                            .cornerRadius(10)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial)
                .cornerRadius(14)
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Custom Stream URL Bar
                VStack(alignment: .leading, spacing: 8) {
                    Text("BENUTZERDEFINIERTE STREAM-URL")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)

                    HStack(spacing: 8) {
                        TextField("http://...", text: $streamURLString)
                            .textFieldStyle(.roundedBorder)
                            .font(.caption.monospaced())
                            .autocapitalization(.none)
                            .disableAutocorrection(true)

                        Button("Laden") {
                            startCurrentPreset()
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(Theme.Colors.accentAction)
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial)
                .cornerRadius(14)
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
            }
            .padding(12)
        }
    }

    private func badgeItem(icon: String, label: String, color: Color) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon)
                .font(.system(size: 10, weight: .bold))
                .foregroundStyle(color)
            Text(label)
                .font(.system(size: 10, weight: .bold, design: .monospaced))
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
                    .background(Color.red.opacity(0.85))
                    .cornerRadius(8)
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
        .background(.ultraThinMaterial)
        .cornerRadius(12)
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
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
            play: { if !isPlaying { startCurrentPreset() } },
            pause: { if isPlaying { togglePlayPause() } },
            togglePlayPause: { togglePlayPause() },
            stop: { teardownPlayback() },
            nextChannel: { zapRelative(delta: 1) },
            previousChannel: { zapRelative(delta: -1) }
        ))
        startCurrentPreset()
        scheduleControlsAutoHide()
    }

    private func teardownPlayback() {
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

        switch streamRouteMode {
        case .livePipeline:
            if let model, let liveURL = model.liveStreamURL(for: sref) {
                return liveURL
            }
            return URL(string: "http://10.10.55.14:8089/api/v3/stream/live/\(sref)")
        case .legacySmoother:
            if let model, let smoothURL = model.legacySmoothStreamURL(for: sref) {
                return smoothURL
            }
            return URL(string: "http://10.10.55.14:8089/api/v3/stream/smooth/\(sref)")
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
        viewPreset = .standard
        streamURLString = preset.url
        currentChannelName = preset.name
        let requestedAt = CACurrentMediaTime()
        let serviceRef = preset.serviceRef

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

    private func togglePlayPause() {
        if isPlaying {
            let stopping = coordinator.playing
            Task { await coordinator.stop() }
            isPlaying = false
            isStreaming = false
            NowPlayingManager.shared.updatePlaybackState(isPlaying: false)
            stopping?.notePlaybackStateChanged()
        } else {
            startCurrentPreset()
        }
    }

    private func cycleViewPreset() {
        Haptics.shared.impact(.light)
        viewPreset = viewPreset.next(includeAdvanced: model?.enableAdvancedAspectRatios ?? false)
        displayZapToast("Bildformat: \(viewPreset.rawValue)")
    }

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
        guard !showLandscapeZapBar && !showHUD else { return }
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
                .font(.system(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(Color.yellow)
            content()
        }
        .padding(6)
        .background(Color.black.opacity(0.4))
        .cornerRadius(6)
    }

    private func hudRow(_ label: String, _ value: String, highlight: Bool = false, alert: Bool = false) -> some View {
        HStack {
            Text(label)
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(Color.gray)
            Spacer()
            Text(value)
                .font(.system(size: 10, weight: .bold, design: .monospaced))
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
                    .font(.system(size: 40, weight: .light))
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
