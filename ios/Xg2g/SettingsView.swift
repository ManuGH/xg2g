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
                    // MARK: - 1. Ebene: Wiedergabe
                    Section {
                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "play.rectangle.on.rectangle", backgroundColor: Color.indigo)
                            Picker("Wiedergabemodus", selection: $model.playbackEngine) {
                                ForEach(AppModel.PlaybackEngine.allCases) { engine in
                                    Text(engine.displayName).tag(engine)
                                }
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        PlaybackEngineComparison(selected: model.playbackEngine)

                        // Kontextsensitive Qualitätsoptionen
                        if model.playbackEngine == .native {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "sparkles", backgroundColor: Theme.Colors.accentLive)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Qualität")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Original · Keine Video-Transkodierung · Minimale Serverlast")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                                Spacer()
                                Text("Original")
                                    .font(.subheadline.bold())
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }
                        } else {
                            HStack(spacing: 12) {
                                SettingsIconBadge(systemName: "bolt.badge.automatic", backgroundColor: Theme.Colors.accentLive)
                                Picker("Streaming-Qualität", selection: $model.qualityPreference) {
                                    ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                                        Text(pref.displayName).tag(pref)
                                    }
                                }
                                .foregroundStyle(Theme.Colors.textPrimary)
                            }
                        }

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "wifi", backgroundColor: Color.teal)
                            Text("Netzwerkzustand")
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
                        if model.playbackEngine == .auto {
                            Text("Im Modus 'Automatisch' entscheidet der xg2g Planner dynamisch über die optimale Pipeline basierend auf Netzwerk, Gerätefähigkeiten und Serverressourcen.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        } else if model.playbackEngine == .native {
                            Text("Native Live-TV liefert das Signal unverändert an die VideoToolbox-Hardware-Pipeline mit minimaler Latenz.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        } else {
                            Text("Server-Streaming (HLS) ermöglicht Pause/Timeshift, externe Nutzung und adaptive Bitrate; xg2g entscheidet, ob Copy, Remux oder Transcode nötig ist.")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - 2. Ebene: Offline & Downloads
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

                    // MARK: - 3. Ebene: Erweitert & Diagnose
                    Section {
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

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "gauge.with.dots.needle.bottom.50percent", backgroundColor: Color.cyan)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Aktiver Wiedergabeplan")
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
                                    Text("Streaming-Technik & Diagnose")
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                    Text("Pipeline-Profile, Test-Overrides & Diagnose-Status")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }
                    } header: {
                        Text("Erweitert & Diagnose")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Aufnahmen und programmierte Timer sind von diesen Einstellungen nicht betroffen — sie laufen immer über den Server und funktionieren in allen Konfigurationen.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Verbindung & Infrastruktur
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
                            Text("Server-Status")
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

                        HStack(spacing: 12) {
                            SettingsIconBadge(systemName: "antenna.radiowaves.left.and.right", backgroundColor: Color.orange)
                            VStack(alignment: .leading, spacing: 2) {
                                Text("Receiver-Adresse (Experten-Fallback)")
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
                        Text("Verbindung & Infrastruktur")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Die Receiver-Adresse dient ausschließlich als optionaler lokaler Fallback für unmanaged Direct-Ingest im Heimnetz. Standardmäßig koordiniert xg2g alle Streams.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    // MARK: - Geräteidentität
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


                    // MARK: - Sitzung
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

/// Detailed diagnostics and test overrides view for developers & power users.
struct DiagnosticPipelineOverrideView: View {

    @Bindable var model: AppModel

    var body: some View {
        List {
            Section {
                Label(
                    "Manuelle Overrides umgehen die automatische Pipeline-Auswahl des xg2g Planners und dienen zu Entwicklungs- und Diagnosezwecken.",
                    systemImage: "exclamationmark.triangle.fill"
                )
                .font(.footnote)
                .foregroundStyle(Theme.Colors.statusWarning)
            } header: {
                Text("Hinweis")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            Section {
                Picker("Pipeline-Override", selection: $model.qualityPreference) {
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
                Text("Test-Override")
                    .foregroundStyle(Theme.Colors.textTertiary)
            } footer: {
                Text("Legt die technische Backend-Pipeline fest: Copy fMP4, Copy MPEG-TS, QSV Closed-GOP Normalisierung oder Transcoding.")
                    .font(.footnote)
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)

            Section {
                HStack {
                    Text("Aktiver Intent")
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Spacer()
                    Text(model.qualityPreference.rawValue)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Theme.Colors.accentAction)
                }

                HStack {
                    Text("Technische Pipeline")
                        .foregroundStyle(Theme.Colors.textPrimary)
                    Spacer()
                    Text(model.qualityPreference.technicalDetails)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
            } header: {
                Text("Telemetrie-Details")
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .listRowBackground(Theme.Colors.surfaceElevated)
        }
        .scrollContentBackground(.hidden)
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .navigationTitle("Streaming-Technik")
        .navigationBarTitleDisplayMode(.inline)
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
