// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct SettingsView: View {

    @Bindable var model: AppModel
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var showingRevokeConfirmation = false
    @State private var showingClearCacheConfirmation = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                List {
                    // MARK: - 1. Ebene: Wiedergabe
                    Section {
                        if dynamicTypeSize.isAccessibilitySize {
                            VStack(alignment: .leading, spacing: 8) {
                                HStack(spacing: 8) {
                                    SettingsIconBadge(systemName: "play.rectangle.on.rectangle", backgroundColor: Color.indigo)
                                    Text("Playback Mode")
                                        .font(.headline)
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                }
                                Picker("Playback Mode", selection: $model.playbackEngine) {
                                    ForEach(AppModel.PlaybackEngine.allCases) { engine in
                                        Text(engine.localizedTitle).tag(engine)
                                    }
                                }
                                .labelsHidden()
                                .pickerStyle(.menu)
                            }
                        } else {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "play.rectangle.on.rectangle", backgroundColor: Color.indigo)
                                Picker("Playback Mode", selection: $model.playbackEngine) {
                                    ForEach(AppModel.PlaybackEngine.allCases) { engine in
                                        Text(engine.localizedTitle).tag(engine)
                                    }
                                }
                                .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }

                        PlaybackEngineComparison(selected: model.playbackEngine)

                        // Kontextsensitive Qualitätsoptionen
                        if model.playbackEngine == .native {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "sparkles", backgroundColor: Theme.Colors.accentLive)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Quality")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Original · No video transcoding · Minimal server load")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                                Spacer()
                                Text("Original")
                                    .font(.subheadline.bold())
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }
                        } else {
                            if dynamicTypeSize.isAccessibilitySize {
                                VStack(alignment: .leading, spacing: 8) {
                                    HStack(spacing: 8) {
                                        SettingsIconBadge(systemName: "bolt.badge.automatic", backgroundColor: Theme.Colors.accentLive)
                                        Text("Streaming Quality")
                                            .font(.headline)
                                            .foregroundStyle(Theme.Colors.textPrimary)
                                    }
                                    Picker("Streaming Quality", selection: $model.qualityPreference) {
                                        ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                                            Text(pref.localizedTitle).tag(pref)
                                        }
                                    }
                                    .labelsHidden()
                                    .pickerStyle(.menu)
                                }
                            } else {
                                HStack(spacing: 12) {
                                    SettingsIconBadge(systemName: "bolt.badge.automatic", backgroundColor: Theme.Colors.accentLive)
                                    Picker("Streaming Quality", selection: $model.qualityPreference) {
                                        ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                                            Text(pref.localizedTitle).tag(pref)
                                        }
                                    }
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                }
                            }
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "wifi", backgroundColor: Color.teal)
                            Text("Network Status")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            HStack(spacing: 6) {
                                Image(systemName: NetworkMonitor.shared.currentType == .wifi ? "wifi" : "antenna.radiowaves.left.and.right")
                                    .foregroundStyle(Theme.Colors.accentAction)
                                Text(NetworkMonitor.shared.currentType.rawValue.uppercased())
                                    .font(.subheadline.monospaced())
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }
                    } header: {
                        Text("Playback")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        if model.playbackEngine == .auto {
                            Text("In 'Auto' mode, xg2g Planner dynamically decides the optimal pipeline based on network, device capabilities, and server resources.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        } else if model.playbackEngine == .native {
                            Text("Native Live TV delivers the signal unmodified to the VideoToolbox hardware pipeline with minimal latency.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        } else {
                            Text("Server Streaming (HLS) enables pause/timeshift, remote access, and adaptive bitrate; xg2g decides whether copy, remux, or transcode is required.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - 2. Ebene: Offline & Downloads
                    Section {
                        if dynamicTypeSize.isAccessibilitySize {
                            VStack(alignment: .leading, spacing: 8) {
                                HStack(spacing: 8) {
                                    SettingsIconBadge(systemName: "arrow.down.circle.fill", backgroundColor: Theme.Colors.statusSuccess)
                                    Text("Default Download Quality")
                                        .font(.headline)
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                }
                                Picker("Default Download Quality", selection: Binding(
                                    get: { DownloadManager.shared.defaultQuality },
                                    set: { DownloadManager.shared.defaultQuality = $0 }
                                )) {
                                    ForEach(DownloadQuality.supportedQualities) { q in
                                        Text(q.localizedTitle).tag(q)
                                    }
                                }
                                .labelsHidden()
                                .pickerStyle(.menu)
                            }
                        } else {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "arrow.down.circle.fill", backgroundColor: Theme.Colors.statusSuccess)
                                Picker("Default Download Quality", selection: Binding(
                                    get: { DownloadManager.shared.defaultQuality },
                                    set: { DownloadManager.shared.defaultQuality = $0 }
                                )) {
                                    ForEach(DownloadQuality.supportedQualities) { q in
                                        Text(q.localizedTitle).tag(q)
                                    }
                                }
                                .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }

                        Toggle(isOn: Binding(
                            get: { DownloadManager.shared.wifiOnly },
                            set: { DownloadManager.shared.wifiOnly = $0 }
                        )) {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "wifi", backgroundColor: Color.blue)
                                Text("Download Over Wi-Fi Only")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }
                        .tint(Theme.Colors.accentAction)
                    } header: {
                        Text("Offline & Downloads")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Compact (HEVC) saves up to 75% storage space on your device and is ideal for flights and long series seasons.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - 3. Ebene: Erweitert & Diagnose
                    Section {
                        Toggle(isOn: $model.enableAdvancedAspectRatios) {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "aspectratio", backgroundColor: Color.purple)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Advanced Aspect Ratios")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Enables manual aspect ratio overrides (16:9, 4:3, Cinemascope) in the player.")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }
                        .tint(Theme.Colors.accentAction)

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "gauge.with.dots.needle.bottom.50percent", backgroundColor: Color.cyan)
                            VStack(alignment: .leading, spacing: 2) {
                                    Text("Active Playback Plan")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                Text(model.activePlaybackPlanDescription)
                                    .font(.caption.monospaced())
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }

                        NavigationLink {
                            DiagnosticPipelineOverrideView(model: model)
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "wrench.and.screwdriver", backgroundColor: Color.gray)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Streaming Technology & Diagnostics")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Pipeline profiles, test overrides & diagnostic status")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }
                    } header: {
                        Text("Advanced & Diagnostics")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Recordings and scheduled timers are unaffected by these settings — they always run on the server and work across all configurations.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Verbindung & Infrastruktur
                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "server.rack", backgroundColor: Theme.Colors.accentAction)
                            Text("Server Address")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.serverURLString)
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "checkmark.circle.fill", backgroundColor: Theme.Colors.statusSuccess)
                            Text("Server Status")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            HStack(spacing: 6) {
                                Circle()
                                    .fill(Theme.Colors.statusSuccess)
                                    .frame(width: 8, height: 8)
                                Text("Connected")
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.statusSuccess)
                            }
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "antenna.radiowaves.left.and.right", backgroundColor: Color.orange)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Receiver Address (Expert Fallback)")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                TextField(ServerAddress.receiverPlaceholder, text: $model.receiverStreamBaseURL)
                                    .textInputAutocapitalization(.never)
                                    .autocorrectionDisabled()
                                    .keyboardType(.URL)
                                    .font(.subheadline.monospaced())
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }
                    } header: {
                        Text("Connection & Infrastructure")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("The receiver address serves solely as an optional local fallback for unmanaged direct ingest in the home network. By default, xg2g coordinates all streams.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Geräteidentität
                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "iphone.gen3", backgroundColor: Color.blue.opacity(0.85))
                            Text("Device Name")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceName)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "cpu", backgroundColor: Color(white: 0.35))
                            Text("Device Type")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceType)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "lock.shield.fill", backgroundColor: Color.purple)
                            Text("Key Storage")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            HStack(spacing: 4) {
                                Image(systemName: "lock.shield.fill")
                                    .foregroundStyle(Theme.Colors.accentAction)
                                Text("Secure Enclave (DPoP)")
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }
                    } header: {
                        Text("Device Identity")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)


                    // MARK: - Speicher & Cache
                    Section {
                        Button {
                            showingClearCacheConfirmation = true
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "trash.circle", backgroundColor: Color.orange)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Clear EPG & Media Cache")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Clears temporary channel and EPG caches and reloads data.")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                                Spacer()
                            }
                        }
                    } header: {
                        Text("Storage & Cache")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Über xg2g
                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "info.circle.fill", backgroundColor: Theme.Colors.accentAction)
                            Text("Version")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text("\(model.appVersion) (\(model.buildNumber))")
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "network.badge.shield.half.filled", backgroundColor: Color.teal)
                            Text("Protocol")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text("v3 Normative")
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "doc.text.fill", backgroundColor: Color.gray)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("License")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                Text("PolyForm Noncommercial License 1.0.0")
                                    .font(.caption)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }

                        NavigationLink {
                            PrivacyPolicyView()
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "hand.raised.fill", backgroundColor: Color.blue)
                                Text("Privacy Policy")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }

                        #if !os(tvOS)
                        if let supportURL = URL(string: "https://github.com/ManuGH/xg2g/issues") {
                            Link(destination: supportURL) {
                                HStack(spacing: 12) {
                                    SettingsIconBadge(systemName: "questionmark.circle.fill", backgroundColor: Color.indigo)
                                    Text("Support & Documentation")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Spacer()
                                    Image(systemName: "arrow.up.right.square")
                                        .font(.footnote)
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                }
                            }
                        }
                        #else
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "questionmark.circle.fill", backgroundColor: Color.indigo)
                            Text("Support & Documentation")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text("github.com/ManuGH/xg2g")
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }
                        #endif
                    } header: {
                        Text("About xg2g")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Sitzung
                    Section {
                        Button(role: .destructive) {
                            showingRevokeConfirmation = true
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "rectangle.portrait.and.arrow.right", backgroundColor: Theme.Colors.statusError)
                                Text(model.isDemoMode ? "Exit Demo Mode" : "Disconnect & Sign Out Device")
                                    .foregroundStyle(Theme.Colors.statusError)
                                    .fontWeight(.medium)
                                Spacer()
                            }
                        }
                    } header: {
                        Text("Session")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Disconnects the DPoP-bound session from the server and securely removes the cryptographic device key from the Secure Enclave.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)
                }
                .safeAreaPadding(.bottom, 80)
                #if !os(tvOS)
                .scrollContentBackground(.hidden)
                #endif
                .confirmationDialog(
                    "Are you sure you want to disconnect this device?",
                    isPresented: $showingRevokeConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Disconnect Device", role: .destructive) {
                        Task { await model.disconnectServer() }
                    }
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text("The device will be signed out on the server. To regain access, the pairing code must be approved again.")
                }
                .confirmationDialog(
                    "Are you sure you want to clear the cache?",
                    isPresented: $showingClearCacheConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Clear Cache & Reload Data", role: .destructive) {
                        Task { await model.clearCaches() }
                    }
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text("Deletes saved EPG data and channel lists and reloads them fresh from the server.")
                }
            }
            .navigationTitle("Settings")
        }
    }
}

/// Detailed diagnostics and test overrides view for developers & power users.
struct DiagnosticPipelineOverrideView: View {

    @Bindable var model: AppModel

    var body: some View {
        List {
            Section {
                Label(
                    "Manual overrides bypass the automatic pipeline selection of the xg2g Planner and are intended for development and diagnostic purposes.",
                    systemImage: "exclamationmark.triangle.fill"
                )
                .font(.footnote)
                .foregroundStyle(Theme.Colors.statusWarning)
            } header: {
                Text("Notice")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            Section {
                Picker("Pipeline Override", selection: $model.qualityPreference) {
                    ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                        VStack(alignment: .leading) {
                            Text(pref.displayName)
                            Text(pref.technicalDetails)
                                .font(.caption2.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }
                        .tag(pref)
                    }
                }
                .pickerStyle(.inline)
            } header: {
                Text("Test Override")
                    .foregroundStyle(Theme.Colors.textTertiary)
            } footer: {
                Text("Specifies the backend technical pipeline: Copy fMP4, Copy MPEG-TS, QSV Closed-GOP normalization, or transcoding.")
                    .font(.footnote)
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            Section {
                HStack {
                    Text("Active Intent")
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Spacer()
                    Text(model.qualityPreference.rawValue)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Theme.Colors.accentAction)
                }

                HStack {
                    Text("Technical Pipeline")
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Spacer()
                    Text(model.qualityPreference.technicalDetails)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
            } header: {
                Text("Telemetry Details")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)
        }
        #if !os(tvOS)
        .scrollContentBackground(.hidden)
        #endif
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .navigationTitle("Streaming Technology")
        #if !os(tvOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
}

/// Shows what the chosen playback route gives the viewer and what it takes away.
///
/// The two routes are not better and worse, they are different trades, and the
/// difference is one a viewer notices immediately — pausing live television
/// works on one and not on the other. Stating both sides is what makes the
/// choice possible; a settings row naming only the upside would leave someone
/// wondering why the pause button stopped working.
struct PlaybackEngineComparison: View {

    let selected: AppModel.PlaybackEngine

    var body: some View {
        let tradeoff = selected.tradeoff

        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(tradeoff.gains.enumerated()), id: \.offset) { _, gain in
                row(icon: "checkmark.circle.fill", tint: Theme.Colors.statusSuccess, text: gain)
            }
            ForEach(Array(tradeoff.costs.enumerated()), id: \.offset) { _, cost in
                row(icon: "minus.circle.fill", tint: Theme.Colors.statusWarning, text: cost)
            }
        }
        .padding(.vertical, 4)
    }

    private func row(icon: String, tint: Color, text: LocalizedStringResource) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: icon)
                .font(.caption)
                .foregroundStyle(tint)
            Text(text)
                .font(.footnote)
                .foregroundStyle(Theme.Colors.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
    }
}

/// Built-in privacy policy summary view accessible on iOS, iPadOS, and tvOS.
struct PrivacyPolicyView: View {

    var body: some View {
        List {
            Section {
                Text("xg2g is a self-hosted personal TV client for your own Enigma2 receiver and xg2g gateway. It does not collect, track, or sell any personal data.")
                    .font(.subheadline)
                    .foregroundStyle(Theme.Colors.textPrimary)
            } header: {
                Text("Overview")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            Section {
                VStack(alignment: .leading, spacing: 6) {
                    Text("No Analytics or Tracking")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Text("The app contains zero third-party analytics SDKs, advertising frameworks, or crash-reporting trackers.")
                        .font(.footnote)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }

                VStack(alignment: .leading, spacing: 6) {
                    Text("Direct Local & Self-Hosted Communication")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Text("All network connections occur strictly between your Apple device and the xg2g server address you configure (or Apple's public sample HLS streams when using Demo Mode).")
                        .font(.footnote)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }

                VStack(alignment: .leading, spacing: 6) {
                    Text("On-Device Credentials (Secure Enclave)")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Text("Device pairing uses RFC 9449 DPoP hardware-bound cryptographic keys stored in the Apple Keychain / Secure Enclave and local preferences in UserDefaults. You can revoke and erase them at any time via 'Disconnect & Sign Out Device'.")
                        .font(.footnote)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
            } header: {
                Text("Data Handling")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            #if !os(tvOS)
            if let policyURL = URL(string: "https://github.com/ManuGH/xg2g/blob/main/docs/PRIVACY_POLICY_IOS.md") {
                Section {
                    Link(destination: policyURL) {
                        HStack {
                            Text("View Full Privacy Policy Online")
                                .foregroundStyle(Theme.Colors.accentAction)
                            Spacer()
                            Image(systemName: "arrow.up.right.square")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                }
                .listRowBackground(Theme.Colors.surfaceElevated)
            }
            #endif
        }
        #if !os(tvOS)
        .scrollContentBackground(.hidden)
        #endif
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .navigationTitle("Privacy Policy")
        #if !os(tvOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
}

