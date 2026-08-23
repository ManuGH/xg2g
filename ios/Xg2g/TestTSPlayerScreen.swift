// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import UIKit

/// SwiftUI screen to test and benchmark the Phase 1 1080i50 $\rightarrow$ 1080p50 VideoToolbox + Metal Vertical Slice.
public struct TestTSPlayerScreen: View {

    @Environment(\.dismiss) private var dismiss
    /// The catalogue, when the caller has one.
    ///
    /// The presets carry a `serviceRef` but no logo — the logos live on the
    /// catalogue's `Channel`. Without this the lock screen had nothing but the
    /// channel name to draw. Optional so the screen stays usable standalone.
    private let model: AppModel?
    /// Owns the visible surface and whichever session is on it.
    ///
    /// Held here rather than inside the UIViewRepresentable so it survives SwiftUI
    /// rebuilding that view, which would otherwise tear down PiP mid-stream — and
    /// held as one object rather than as a pipeline plus a presenter, because with
    /// channels prepared beside one another there is no longer a single pipeline for
    /// the screen to own.
    @StateObject private var coordinator: ZapCoordinator

    /// The readouts of the channel actually on screen.
    ///
    /// Empty values while nothing is playing, so the HUD reads as "nothing yet"
    /// rather than as the last channel's figures.
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
    /// Which presentation model the render view uses. Selectable rather than
    /// implied, so the drawable path is reachable and the two can be compared
    /// on the same stream instead of only one of them ever running.
    @State private var presentationPath: MetalVideoView.PresentationPath = .systemLayer
    @State private var viewPreset: VideoViewPreset = .standard
    @State private var showControls: Bool = true
    @State private var showLandscapeZapBar: Bool = false
    @State private var autoHideControlsTask: Task<Void, Never>?
    @State private var zapToast: String?
    @State private var hideZapToastTask: Task<Void, Never>?

    private struct ChannelPreset: Identifiable, Hashable {
        /// Keyed on the service, not on a fresh UUID: the list is computed from
        /// the catalogue on demand, and an identity that changed on every read
        /// would make SwiftUI rebuild the whole row set each time.
        var id: String { serviceRef }
        let name: String
        let serviceRef: String
        let url: String
        let epgNow: String
        let category: String
    }

    /// Hard-coded services for bench work, used only when no catalogue is
    /// available — i.e. when the screen is opened from the developer section
    /// rather than to watch an actual channel.
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

    // Internal rather than public: `AppModel` is internal, and this screen is
    // only ever constructed from inside the app.
    //
    // With a `channel` it opens on that service and behaves as the live player;
    // without one it opens on a bench preset, which is how the developer entry
    // in Settings uses it.
    init(model: AppModel? = nil, channel: Channel? = nil) {
        self.model = model
        if let model, let channel, let url = model.directStreamURL(for: channel) {
            _streamURLString = State(initialValue: url.absoluteString)
            _currentChannelName = State(initialValue: channel.name)
        }
        // Built once with the screen and outliving every session on it. Without a
        // configured backend it has nothing to prepare against and says so, rather
        // than pretending a channel change is a transaction when it is not.
        _coordinator = StateObject(wrappedValue: ZapCoordinator(
            preparations: model?.makeZapPreparationClient(),
            streamURL: { [weak model] serviceRef in
                model?.liveStreamURL(for: serviceRef)
                    ?? URL(string: "http://10.10.55.14:8089/api/v3/stream/live/\(serviceRef)")
            }
        ))
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

    private var currentLogoURL: URL? {
        if let preset = currentPreset, let url = logoURL(forPreset: preset) {
            return url
        }
        if let match = model?.channels.first(where: { $0.name == currentChannelName }) {
            return match.logoURL
        }
        return nil
    }

    private var currentChannel: Channel {
        if let model, let match = model.channels.first(where: { $0.name == currentChannelName || $0.serviceRef == currentPreset?.serviceRef }) {
            return match
        }
        return Channel(
            id: currentPreset?.serviceRef ?? "native_lab",
            name: currentChannelName,
            number: nil,
            serviceRef: currentPreset?.serviceRef ?? "",
            logoURL: currentLogoURL
        )
    }

    private func switchToChannel(_ channel: Channel) {
        guard let model, let url = model.directStreamURL(for: channel) else { return }
        streamURLString = url.absoluteString
        currentChannelName = channel.name
        displayZapToast("Kanal: \(channel.name)")
        startCurrentPreset()
    }

    private func closePlayer() {
        teardownPlayback()
        if let model {
            model.playingChannel = nil
        }
        dismiss()
    }

    /// The channels this screen can tune, newest EPG title included.
    ///
    /// Real catalogue when there is one — this screen is a normal player now,
    /// not only a bench — and the hard-coded services only when there is not.
    private var presets: [ChannelPreset] {
        guard let model else { return Self.labPresets }
        let catalogue = model.filteredChannels.isEmpty ? model.channels : model.filteredChannels
        let live = catalogue.compactMap { channel -> ChannelPreset? in
            guard let url = model.directStreamURL(for: channel) else { return nil }
            return ChannelPreset(
                name: channel.name,
                serviceRef: channel.serviceRef,
                url: url.absoluteString,
                epgNow: model.schedule[channel.serviceRef]?.now?.title ?? "",
                category: ""
            )
        }
        return live.isEmpty ? Self.labPresets : live
    }

    /// The preset currently streaming, for anything that needs more than its URL.
    private var currentPreset: ChannelPreset? {
        presets.first { $0.url == streamURLString }
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
            teardownPlayback()
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
                HStack(spacing: 12) {
                    Button {
                        closePlayer()
                    } label: {
                        Image(systemName: isLandscape ? "xmark.circle.fill" : "chevron.down")
                            .font(.system(size: isLandscape ? 22 : 13, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(isLandscape ? 6 : 9)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    ChannelLogo(url: currentLogoURL, name: currentChannelName, size: isLandscape ? 34 : 30)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            if let num = currentChannel.number {
                                Text(num)
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1.5)
                                    .background(Theme.Colors.accentAction.opacity(0.2), in: RoundedRectangle(cornerRadius: 4))
                            }

                            Text(currentChannelName)
                                .font(.system(size: 14, weight: .bold))
                                .foregroundStyle(.white)

                            Text("LIVE 1080i50")
                                .font(.system(size: 9, weight: .black, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.accentLive.opacity(0.2), in: RoundedRectangle(cornerRadius: 4))
                        }

                        if let preset = presets.first(where: { $0.url == streamURLString }), !preset.epgNow.isEmpty {
                            Text(preset.epgNow)
                                .font(.system(size: 11, weight: .medium))
                                .foregroundStyle(.white.opacity(0.85))
                                .lineLimit(1)
                        }
                    }

                    Spacer()

                    // Telemetry HUD Toggle Badge
                    Button {
                        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                            showHUD.toggle()
                        }
                    } label: {
                        HStack(spacing: 5) {
                            Circle()
                                .fill(tele.isAudioMasterClockActive ? Color.green : Color.yellow)
                                .frame(width: 7, height: 7)
                            Image(systemName: showHUD ? "chart.bar.fill" : "chart.bar")
                                .font(.system(size: 12, weight: .bold))
                            Text(String(format: "%.0f fps", tele.fieldsSubmittedPerSec))
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(.white)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    // Presentation path selector — within the native pipeline only.
                    Button {
                        Haptics.shared.impact(.light)
                        presentationPath = (presentationPath == .systemLayer) ? .metalDrawable : .systemLayer
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: presentationPath == .systemLayer ? "rectangle.on.rectangle" : "cpu")
                                .font(.system(size: 11, weight: .bold))
                            Text(presentationPath == .systemLayer ? "Layer" : "Drawable")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(.white)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    // Video aspect ratio & scaling preset selector (VLC style)
                    Button {
                        cycleViewPreset()
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: viewPreset.scalingMode == .fill ? "arrow.up.left.and.arrow.down.right" : "aspectratio")
                                .font(.system(size: 11, weight: .bold))
                            Text(viewPreset.rawValue)
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(.white)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    // Picture in Picture.
                    Button {
                        Haptics.shared.impact(.light)
                        coordinator.surface.startPictureInPicture()
                    } label: {
                        Image(systemName: "pip.enter")
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: 34, height: 34)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                    .disabled(presentationPath != .systemLayer)
                    .opacity(presentationPath == .systemLayer ? 1.0 : 0.4)

                    // AirPlay Route Picker Button
                    AirPlayButton()
                        .frame(width: 34, height: 34)
                        .padding(2)
                        .background(.ultraThinMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .padding(.horizontal, sideInset)
                .padding(.top, isLandscape ? 14 : max(safeInsets.top, 8))

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
                            Text("1080i50 Hardware Direct")
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
                        ChannelLogo(url: currentLogoURL, name: currentChannelName, size: 44)

                        VStack(alignment: .leading, spacing: 2) {
                            Text(currentChannelName)
                                .font(.title3.weight(.bold))
                                .foregroundStyle(.white)

                            if let preset = presets.first(where: { $0.url == streamURLString }) {
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

                    // Format Status Badges
                    HStack(spacing: 8) {
                        badgeItem(icon: "sparkles.tv", label: "1080i50 HW Bob", color: .green)
                        badgeItem(icon: "speaker.wave.3.fill", label: "\(tele.audioCodec) 5.1", color: .blue)
                        badgeItem(icon: "thermometer.medium", label: tele.thermalState, color: .orange)
                    }
                }
                // Stream Routing Mode (A/B Test)
                VStack(alignment: .leading, spacing: 8) {
                    Text("STREAM-ROUTING (A/B TEST)")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)

                    Picker("Route Mode", selection: $streamRouteMode) {
                        ForEach(StreamRouteMode.allCases, id: \.self) { mode in
                            Text(mode.rawValue).tag(mode)
                        }
                    }
                    .pickerStyle(.segmented)
                    .onChange(of: streamRouteMode) { _, _ in
                        if isStreaming {
                            startCurrentPreset()
                        }
                    }
                }
                .padding(14)
                .background(.ultraThinMaterial)
                .cornerRadius(14)
                .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Quick Zap Channel Presets
                VStack(alignment: .leading, spacing: 8) {
                    Text("SCHNELL-UMSCHALTEN (TEST-KANÄLE)")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)

                    ForEach(presets) { preset in
                        Button {
                            switchTo(preset: preset)
                        } label: {
                            HStack(spacing: 12) {
                                ChannelLogo(url: logoURL(forPreset: preset), name: preset.name, size: 36)

                                VStack(alignment: .leading, spacing: 2) {
                                    Text(preset.name)
                                        .font(.subheadline.weight(streamURLString == preset.url ? .bold : .medium))
                                        .foregroundStyle(streamURLString == preset.url ? .white : Theme.Colors.textPrimary)

                                    Text(preset.epgNow)
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                        .lineLimit(1)
                                }

                                Spacer()

                                if streamURLString == preset.url {
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
                            .background(streamURLString == preset.url ? Color.white.opacity(0.08) : Color.clear)
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
                    hudSection(title: "STARTUP GATES (TTFP)") {
                        hudRow("TTFP (GPU Done)", tele.ttfpGpuCompletedMs > 0 ? String(format: "%.1f ms", tele.ttfpGpuCompletedMs) : (tele.ttfpTotalMs > 0 ? String(format: "%.1f ms", tele.ttfpTotalMs) : "Instant 🚀"), highlight: true)
                        hudRow("Performance", tele.ttfpRating, highlight: true)
                        hudRow("t0→t1: Network", String(format: "%.1f ms", tele.ttfpNetworkMs))
                        hudRow("t1→t2: PSI Demux", String(format: "%.1f ms", tele.ttfpPsiMs))
                        hudRow("t4→t5: HW Decode", String(format: "%.1f ms", tele.ttfpDecodeMs))
                    }

                    hudSection(title: "VIDEO & RENDER") {
                        let isInterlaced = tele.isInterlaced
                        let h = tele.videoHeight
                        let w = tele.videoWidth
                        let srcFps = tele.sourceFrameRate
                        let fps = Int(round(srcFps > 0 ? (isInterlaced ? srcFps * 2 : srcFps) : 50))
                        let formatStr = (w > 0 && h > 0) ? "\(w)x\(h) \(h)\(isInterlaced ? "i" : "p")\(fps)" : "Detecting…"
                        hudRow("Format", formatStr)
                        hudRow("HW Decode", tele.hwDecodeActive ? "Active 🚀" : "Pending…", highlight: tele.hwDecodeActive)
                        hudRow("Fields Displayed", String(format: "%.1f fields/s", tele.fieldsSubmittedPerSec), highlight: abs(tele.fieldsSubmittedPerSec - 50.0) < 3.0)
                        hudRow("Bitrate", String(format: "%.1f kbps", tele.tsBitrateKbps))
                    }

                    hudSection(title: "AUDIO (AVFoundation)") {
                        hudRow("Codec", tele.audioCodec)
                        hudRow("Channels", "\(tele.audioChannels) ch")
                        hudRow("Master Clock", tele.isAudioMasterClockActive ? "Synchronized 🟢" : "Pre-roll ⚪️", highlight: tele.isAudioMasterClockActive)
                    }

                    hudSection(title: "SYSTEM & PERFORMANCE") {
                        hudRow("Thermal State", tele.thermalState, highlight: tele.thermalState.contains("Nominal"))
                        hudRow("Footprint (Peak)", String(format: "%.1f MB (%.1f MB)", tele.memoryUsageMB, tele.peakMemoryFootprintMB))
                        hudRow("Process CPU", String(format: "%.1f %%", tele.processCpuUsagePercent), highlight: tele.processCpuUsagePercent < 25.0)
                        hudRow("VT In-Flight", "\(tele.vtInFlightFrames)", highlight: tele.vtInFlightFrames <= 3)
                    }
                }
            }
            .padding(10)
        }
        .frame(maxWidth: 320, maxHeight: 350)
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
        Task { await coordinator.stop() }
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
        // Stamped here so the figure covers the wait as the viewer experiences
        // it, including whatever this function does before handing over.
        let requestedAt = CACurrentMediaTime()
        let serviceRef = URL(string: streamURLString)?.lastPathComponent

        // The live route is the only one with a backend to warm a channel on, so it is
        // the only one that can make before it breaks: the channel playing keeps
        // playing while the next is prepared, and the surface changes hands once. The
        // direct and legacy routes go straight at the receiver, have nothing to prove
        // against, and start outright — which is what every route used to do.
        if streamRouteMode == .livePipeline, coordinator.canPrepare, let serviceRef {
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
            // Publishes the entry itself, not just its rate: `updatePlaybackState`
            // returns at its first line when nothing has been published, which is
            // why this player's lock screen was empty and its controls inert.
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
        displayZapToast("Kanal: \(preset.name)")
        startCurrentPreset()
    }

    private func zapRelative(delta: Int) {
        guard let currentIndex = presets.firstIndex(where: { $0.url == streamURLString }) else { return }
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

private struct MetalVideoStageView: UIViewRepresentable {
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
