// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct SettingsView: View {

    let model: AppModel
    @State private var showingRevokeConfirmation = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                List {
                    Section {
                        HStack {
                            Text("Server-Adresse")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.serverURLString)
                                .font(.subheadline.monospaced())
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack {
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
                    Section {
                        Picker("Streaming-Modus", selection: Binding(
                            get: { model.qualityPreference },
                            set: { model.qualityPreference = $0 }
                        )) {
                            ForEach(AppModel.StreamingQualityPreference.allCases) { pref in
                                Text(pref.displayName).tag(pref)
                            }
                        }
                        .foregroundStyle(Theme.Colors.textPrimary)

                        HStack {
                            Text("Aktuelle Verbindung")
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
                        Text("Wiedergabe & Bandbreite")
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } footer: {
                        Text("Im Modus 'Automatisch' wird im WLAN verlustfreies 1:1 Direct-Streaming (0% CPU, Originalbitrate) verwendet und bei Mobilfunk datensparend mit AV1/HEVC transcodiert.")
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .listRowBackground(Theme.Colors.surfaceElevated)

                    Section {
                        HStack {
                            Text("Gerätename")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceName)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack {
                            Text("Gerätetyp")
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Spacer()
                            Text(model.currentDeviceType)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        HStack {
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
                            HStack {
                                Spacer()
                                Label("Gerät trennen & abmelden", systemImage: "rectangle.portrait.and.arrow.right")
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
