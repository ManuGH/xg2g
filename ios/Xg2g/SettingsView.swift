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
