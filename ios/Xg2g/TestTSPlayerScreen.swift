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
    @StateObject private var pipeline = NativeTSVideoPipeline()
    /// Owns the AVFoundation display layer and the PiP controller. Held here
    /// rather than inside the UIViewRepresentable so it survives SwiftUI
    /// rebuilding that view, which would otherwise tear down PiP mid-stream.
    @StateObject private var systemPresenter = SystemVideoPresenterBox()
    @State private var streamURLString: String = "http://10.10.55.64:8001/1:0:19:11:6:85:C00000:0:0:0:"
    @State private var currentChannelName: String = "Sky Sport F1 HD"
    @State private var isStreaming: Bool = false
    @State private var isPlaying: Bool = true
    @State private var showHUD: Bool = false
    /// Which presentation model the render view uses. Selectable rather than
    /// implied, so the drawable path is reachable and the two can be compared
    /// on the same stream instead of only one of them ever running.
    @State private var presentationPath: MetalVideoView.PresentationPath = .systemLayer
    @State private var showControls: Bool = true
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
    }

    /// The catalogue logo for a preset, matched on `serviceRef`.
    private func logoURL(for serviceRef: String) -> URL? {
        model?.channels.first { $0.serviceRef == serviceRef }?.logoURL
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
                            pipeline: pipeline,
                            presenter: systemPresenter.presenter,
                            presentationPath: presentationPath
                        )
                            .ignoresSafeArea(edges: isLandscape ? .all : [])

                        // 2. A format this pipeline cannot assemble produces no
                        //    picture at all, and a black rectangle explains
                        //    nothing. Say what happened and name the way out.
                        if let unplayable = pipeline.telemetry.display.unplayableVideoCodec {
                            UnplayableFormatNotice(
                                formatDescription: unplayable,
                                channelName: currentChannelName
                            )
                        }

                        // 3. On-Screen Display Controls & Buttons
                        if showControls {
                            videoOverlayControls(isLandscape: isLandscape, safeTop: geometry.safeAreaInsets.top)
                                .transition(.opacity)
                        }

                        // 3. Floating Telemetry Inspector Modal
                        if showHUD {
                            telemetryHUDView
                                .padding(.top, isLandscape ? 48 : max(geometry.safeAreaInsets.top, 8) + 40)
                                .padding(.leading, 12)
                                .transition(.scale(scale: 0.9).combined(with: .opacity))
                        }
                    }
                    .frame(
                        maxWidth: .infinity,
                        maxHeight: isLandscape ? .infinity : (geometry.size.width * 9.0 / 16.0)
                    )
                    .background(Color.black)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        withAnimation(.easeInOut(duration: 0.2)) {
                            showControls.toggle()
                        }
                        if showControls {
                            scheduleControlsAutoHide()
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
            // Real fullscreen, not merely an edge-to-edge video frame. Ignoring
            // the safe area stretches the picture under the status bar and the
            // home indicator, but both keep drawing on top of it — a bright
            // clock over the image and a bar across the bottom. Landscape here
            // means "watching", so the system chrome leaves with the controls.
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
    private func videoOverlayControls(isLandscape: Bool, safeTop: CGFloat) -> some View {
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
                        dismiss()
                    } label: {
                        Image(systemName: isLandscape ? "xmark.circle.fill" : "chevron.down")
                            .font(.system(size: isLandscape ? 22 : 13, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(isLandscape ? 6 : 9)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
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

                        if let preset = presets.first(where: { $0.url == streamURLString }) {
                            Text(preset.epgNow)
                                .font(.system(size: 11, weight: .medium))
                                .foregroundStyle(.white.opacity(0.8))
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
                                .fill(pipeline.telemetry.display.isAudioMasterClockActive ? Color.green : Color.yellow)
                                .frame(width: 7, height: 7)
                            Image(systemName: showHUD ? "chart.bar.fill" : "chart.bar")
                                .font(.system(size: 12, weight: .bold))
                            Text(String(format: "%.0f fps", pipeline.telemetry.display.fieldsSubmittedPerSec))
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
                    //
                    // Labelled "Layer"/"Drawable" rather than anything with AV in
                    // it: the app also ships an entirely separate AVPlayer/HLS
                    // player, and a badge reading "AVF" here invites reading this
                    // as a switch between the two players. It is not. Both
                    // settings decode the transport stream natively and differ
                    // only in who presents the finished fields —
                    // `AVSampleBufferDisplayLayer`, which schedules them from
                    // their timestamps and is what Picture in Picture needs, or
                    // our own drawable, scheduled here against the stream clock.
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

                    // Picture in Picture. Enabled only once the display layer has
                    // content and the system reports it as possible, so the button
                    // never offers something that would silently do nothing.
                    // The drawable path has no display layer to hand PiP, so the
                    // button goes with it.
                    Button {
                        Haptics.shared.impact(.light)
                        systemPresenter.presenter.startPictureInPicture()
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
                .padding(.horizontal, 12)
                .padding(.top, isLandscape ? 12 : max(safeTop, 8))

                Spacer()

                // Center Transport Controls
                HStack(spacing: 32) {
                    // Previous Channel
                    Button {
                        zapRelative(delta: -1)
                    } label: {
                        Image(systemName: "backward.end.fill")
                            .font(.system(size: 20, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
                    }
                    .buttonStyle(.plain)

                    // Play / Pause Toggle
                    Button {
                        togglePlayPause()
                    } label: {
                        Image(systemName: isPlaying ? "pause.fill" : "play.fill")
                            .font(.system(size: 28, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(16)
                            .background(Theme.Colors.accentAction.opacity(0.9), in: Circle())
                            .shadow(color: Theme.Colors.accentAction.opacity(0.5), radius: 10)
                    }
                    .buttonStyle(.plain)

                    // Next Channel
                    Button {
                        zapRelative(delta: 1)
                    } label: {
                        Image(systemName: "forward.end.fill")
                            .font(.system(size: 20, weight: .bold))
                            .foregroundStyle(.white)
                            .padding(12)
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
                            Text("\(pipeline.telemetry.display.audioCodec) (\(pipeline.telemetry.display.audioChannels == 6 ? "5.1 Surround" : "\(pipeline.telemetry.display.audioChannels) ch"))")
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
                            Text("1920x1080i50 Metal Bob")
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(.white.opacity(0.9))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(.ultraThinMaterial, in: Capsule())

                        Spacer()

                        Text(String(format: "Bitrate: %.1f kbps", pipeline.telemetry.display.tsBitrateKbps))
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(.white.opacity(0.7))
                    }
                    .padding(.horizontal, 16)
                    .padding(.bottom, 12)
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
                    HStack {
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
                                pipeline.stopStreaming()
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
                        badgeItem(icon: "speaker.wave.3.fill", label: "\(pipeline.telemetry.display.audioCodec) 5.1", color: .blue)
                        badgeItem(icon: "thermometer.medium", label: pipeline.telemetry.display.thermalState, color: .orange)
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
                                Image(systemName: streamURLString == preset.url ? "play.circle.fill" : "tv")
                                    .font(.system(size: 18))
                                    .foregroundStyle(streamURLString == preset.url ? Theme.Colors.accentLive : Theme.Colors.textSecondary)

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
                            .padding(.vertical, 10)
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

                if let warning = pipeline.telemetry.display.validationWarning {
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
                        hudRow("TTFP (GPU Done)", pipeline.telemetry.display.ttfpGpuCompletedMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpGpuCompletedMs) : (pipeline.telemetry.display.ttfpTotalMs > 0 ? String(format: "%.1f ms", pipeline.telemetry.display.ttfpTotalMs) : "Instant 🚀"), highlight: true)
                        hudRow("Performance", pipeline.telemetry.display.ttfpRating, highlight: true)
                        hudRow("t0→t1: Network", String(format: "%.1f ms", pipeline.telemetry.display.ttfpNetworkMs))
                        hudRow("t1→t2: PSI Demux", String(format: "%.1f ms", pipeline.telemetry.display.ttfpPsiMs))
                        hudRow("t4→t5: HW Decode", String(format: "%.1f ms", pipeline.telemetry.display.ttfpDecodeMs))
                    }

                    hudSection(title: "VIDEO & RENDER") {
                        hudRow("Format", "\(pipeline.telemetry.display.videoWidth)x\(pipeline.telemetry.display.videoHeight) 1080i50")
                        hudRow("HW Decode", pipeline.telemetry.display.hwDecodeActive ? "Active 🚀" : "Pending…", highlight: pipeline.telemetry.display.hwDecodeActive)
                        hudRow("Fields Displayed", String(format: "%.1f fields/s", pipeline.telemetry.display.fieldsSubmittedPerSec), highlight: abs(pipeline.telemetry.display.fieldsSubmittedPerSec - 50.0) < 3.0)
                        hudRow("Bitrate", String(format: "%.1f kbps", pipeline.telemetry.display.tsBitrateKbps))
                    }

                    hudSection(title: "AUDIO (AVFoundation)") {
                        hudRow("Codec", pipeline.telemetry.display.audioCodec)
                        hudRow("Channels", "\(pipeline.telemetry.display.audioChannels) ch")
                        hudRow("Master Clock", pipeline.telemetry.display.isAudioMasterClockActive ? "Synchronized 🟢" : "Pre-roll ⚪️", highlight: pipeline.telemetry.display.isAudioMasterClockActive)
                    }

                    hudSection(title: "SYSTEM THERMAL") {
                        hudRow("Thermal State", pipeline.telemetry.display.thermalState, highlight: pipeline.telemetry.display.thermalState.contains("Nominal"))
                        hudRow("RAM Usage", String(format: "%.1f MB", pipeline.telemetry.display.memoryUsageMB))
                    }
                }
            }
            .padding(10)
        }
        .frame(maxWidth: 300, maxHeight: 320)
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
        pipeline.stopStreaming()
        isStreaming = false
        UIApplication.shared.isIdleTimerDisabled = false
        autoHideControlsTask?.cancel()
        hideZapToastTask?.cancel()
        NowPlayingManager.shared.clear()
    }

    private func startCurrentPreset() {
        // Stamped here so the figure covers the wait as the viewer experiences
        // it, including whatever this function does before handing over.
        let requestedAt = CACurrentMediaTime()
        if let url = URL(string: streamURLString) {
            pipeline.startStreaming(url: url, requestedAt: requestedAt)
            isStreaming = true
            isPlaying = true
            // Publishes the entry itself, not just its rate: `updatePlaybackState`
            // returns at its first line when nothing has been published, which is
            // why this player's lock screen was empty and its controls inert.
            NowPlayingManager.shared.updateLive(
                title: currentChannelName,
                subtitle: currentPreset?.epgNow,
                logoURL: currentPreset.flatMap { logoURL(for: $0.serviceRef) }
            )
            // Resuming goes through here, and the manager remembers the paused
            // state from before — without this the lock screen and the watch
            // would keep showing a pause button over running playback.
            NowPlayingManager.shared.updatePlaybackState(isPlaying: true)
        }
    }

    private func switchTo(preset: ChannelPreset) {
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
            pipeline.stopStreaming()
            isPlaying = false
            isStreaming = false
            NowPlayingManager.shared.updatePlaybackState(isPlaying: false)
            pipeline.notePlaybackStateChanged()
        } else {
            startCurrentPreset()
        }
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
        HStack {
            Text("⚡️ Native 1080p50 Video Telemetry")
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
    let pipeline: NativeTSVideoPipeline
    let presenter: SystemVideoPresenter
    let presentationPath: MetalVideoView.PresentationPath

    func makeUIView(context: Context) -> MetalVideoView {
        let view = MetalVideoView(frame: .zero)
        view.telemetry = pipeline.telemetry
        pipeline.renderView = view

        // Presenting through AVFoundation instead of our own drawable. The Metal
        // view keeps doing the decode-side work — reorder, field scheduling and
        // the deinterlace pass — and its output goes into the display layer,
        // which is hosted on top of it.
        view.systemPresenter = presenter
        pipeline.systemPresenter = presenter
        view.presentationPath = presentationPath
        presenter.displayLayer.frame = view.bounds
        view.layer.addSublayer(presenter.displayLayer)
        presenter.enablePictureInPicture()

        return view
    }

    func updateUIView(_ uiView: MetalVideoView, context: Context) {
        uiView.telemetry = pipeline.telemetry
        uiView.presentationPath = presentationPath
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

                Text("Der Sender überträgt \(formatDescription). Direkt vom Receiver lässt sich nur H.264 wiedergeben.")
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
