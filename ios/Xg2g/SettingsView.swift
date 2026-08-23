// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct SettingsView: View {

    @Bindable var model: AppModel
    @State private var showingRevokeConfirmation = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                List {
                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "server.rack", backgroundColor: Theme.Colors.accentAction)
                            Text("Server-Adresse")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.serverURLString)
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "checkmark.circle.fill", backgroundColor: Theme.Colors.statusSuccess)
                            Text("Status")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            HStack(spacing: 6) {
                                Circle()
                                    .fill(Theme.Colors.statusSuccess)
                                    .frame(width: 8, height: 8)
                                Text("Verbunden")
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.statusSuccess)
                            }
                        }
                    } header: {
                        Text("Server")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "play.rectangle.on.rectangle", backgroundColor: Color.indigo)
                            Picker("Wiedergabe-Art", selection: $model.playbackEngine) {
                                ForEach(AppModel.PlaybackEngine.allCases) { engine in
                                    Text(engine.displayName).tag(engine)
                                }
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        PlaybackEngineComparison(selected: model.playbackEngine)

                        if model.playbackEngine == .native {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "antenna.radiowaves.left.and.right", backgroundColor: Color.orange)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Receiver-Adresse")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    TextField("http://192.168.1.50:8001", text: $model.receiverStreamBaseURL)
                                        .textInputAutocapitalization(.never)
                                        .autocorrectionDisabled()
                                        .keyboardType(.URL)
                                        .font(.subheadline.monospaced())
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }

                            if !model.isDirectPlaybackAvailable {
                                Label(
                                    "Ohne Receiver-Adresse läuft die Wiedergabe weiter über den Server.",
                                    systemImage: "exclamationmark.triangle.fill"
                                )
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.statusWarning)
                            }
                        }

                        Toggle(isOn: $model.enableAdvancedAspectRatios) {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "aspectratio", backgroundColor: Color.purple)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Erweiterte Seitenverhältnisse")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Aktiviert manuelle Format-Overrides (16:9, 4:3, Cinemascope) im Player.")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }
                        .tint(Theme.Colors.accentAction)
                    } header: {
                        Text("Wiedergabe-Art")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Aufnahmen und programmierte Timer sind von dieser Einstellung nicht betroffen — sie laufen immer über den Server und funktionieren in beiden Fällen.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "bolt.badge.automatic", backgroundColor: Theme.Colors.accentLive)
                            Picker("Streaming-Modus", selection: $model.qualityPreference) {
                                ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                                    Text(pref.displayName).tag(pref)
                                }
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "wifi", backgroundColor: Color.teal)
                            Text("Verbindung")
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
                        Text("Wiedergabe")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Im Modus 'Automatisch' schaltet xg2g im WLAN automatisch auf verlustfreies 1:1 Direct-Streaming (0% Server-CPU) und mobil auf adaptives Streaming.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "arrow.down.circle.fill", backgroundColor: Theme.Colors.statusSuccess)
                            Picker("Standard-Download", selection: Binding(
                                get: { DownloadManager.shared.defaultQuality },
                                set: { DownloadManager.shared.defaultQuality = $0 }
                            )) {
                                ForEach(DownloadQuality.supportedQualities) { q in
                                    Text(q.title).tag(q)
                                }
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        Toggle(isOn: Binding(
                            get: { DownloadManager.shared.wifiOnly },
                            set: { DownloadManager.shared.wifiOnly = $0 }
                        )) {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "wifi", backgroundColor: Color.blue)
                                Text("Nur über WLAN laden")
                                    .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }
                        .tint(Theme.Colors.accentAction)
                    } header: {
                        Text("Offline & Downloads")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Kompakt (HEVC) spart bis zu 75% Speicherplatz auf deinem Gerät und ist ideal für Flugreisen und lange Serien-Staffeln.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "iphone.gen3", backgroundColor: Color.blue.opacity(0.85))
                            Text("Gerätename")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceName)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "cpu", backgroundColor: Color(white: 0.35))
                            Text("Gerätetyp")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceType)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "lock.shield.fill", backgroundColor: Color.purple)
                            Text("Schlüsselspeicher")
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
                        Text("Geräteidentität")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

#if DEBUG
                    Section {
                        NavigationLink {
                            TestTSPlayerScreen(model: model)
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "sparkles.tv", backgroundColor: Color.orange)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("1080i50 Metal Player (Labor)")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Native VideoToolbox & Metal 50p Benchmark")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }
                    } header: {
                        Text("Experimentell & Labor")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Direkter 1080i50 TS-Stream Hardware-Decode mit Echtzeit-GPU-Deinterlacing und Telemetrie-HUD.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)
#endif

                    Section {
                        Button(role: .destructive) {
                            showingRevokeConfirmation = true
                        } label: {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "rectangle.portrait.and.arrow.right", backgroundColor: Theme.Colors.statusError)
                                Text("Gerät trennen & abmelden")
                                    .foregroundStyle(Theme.Colors.statusError)
                                    .fontWeight(.medium)
                                Spacer()
                            }
                        }
                    } header: {
                        Text("Sitzung")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Trennt die DPoP-Verbindung zum Server und entfernt den kryptographischen Geräteschlüssel sicher aus der Secure Enclave.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)
                }
                .safeAreaPadding(.bottom, 80)
                .scrollContentBackground(.hidden)
                .confirmationDialog(
                    "Möchtest du dieses Gerät wirklich trennen?",
                    isPresented: $showingRevokeConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Gerät trennen", role: .destructive) {
                        Task { await model.disconnectServer() }
                    }
                    Button("Abbrechen", role: .cancel) {}
                } message: {
                    Text("Das Gerät wird serverseitig abgemeldet. Für erneuten Zugriff muss der Pairing-Code wieder genehmigt werden.")
                }
            }
            .navigationTitle("Einstellungen")
        }
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
            ForEach(tradeoff.gains, id: \.self) { gain in
                row(icon: "checkmark.circle.fill", tint: Theme.Colors.statusSuccess, text: gain)
            }
            ForEach(tradeoff.costs, id: \.self) { cost in
                row(icon: "minus.circle.fill", tint: Theme.Colors.statusWarning, text: cost)
            }
        }
        .padding(.vertical, 4)
    }

    private func row(icon: String, tint: Color, text: String) -> some View {
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
