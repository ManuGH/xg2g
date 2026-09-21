// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Pairing View

struct PairingView: View {

    let model: AppModel

    @State private var invitation: EnrollmentCoordinator.Invitation?
    @State private var isStarting = false
    @State private var isWaiting = false

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            VStack(spacing: 28) {
                Spacer()

                Image(systemName: "lock.shield")
                    .font(.app(size: 64))
                    .foregroundStyle(Theme.Colors.accentLive)
                    .padding()
                    .background(Theme.Colors.surfaceGlass, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.Colors.borderElevated, lineWidth: 1))

                VStack(spacing: 8) {
                    Text("Geräte-Kopplung")
                        .font(.title2.bold())
                        .foregroundStyle(Theme.Colors.textPrimary)

                    Text(model.serverURLString)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())

                    if let invitation {
                        Text("Gib diesen Code in deiner Web-Admin-Konsole unter Geräte ein:")
                            .font(.subheadline)
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .multilineTextAlignment(.center)
                            .padding(.top, 8)

                        Text(invitation.userCode)
                            .font(.app(size: 42, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
#if !os(tvOS)
                            .textSelection(.enabled)
#endif
                            .padding(.vertical, 16)
                            .padding(.horizontal, 28)
                            .glassCard(cornerRadius: 16)

                        if isWaiting {
                            HStack(spacing: 8) {
                                ProgressView()
                                    .tint(Theme.Colors.accentLive)
                                Text("Warte auf Bestätigung in der Admin-Konsole…")
                                    .font(.footnote)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                            .padding(.top, 8)
                        }
                    } else if isStarting {
                        VStack(spacing: 8) {
                            ProgressView()
                                .tint(Theme.Colors.accentAction)
                            Text("Sichere Verbindung wird eingerichtet…")
                                .font(.footnote.weight(.medium))
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Text("Schlüsselspeicher wird vorbereitet")
                                .font(.caption)
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                        .padding(.vertical, 24)
                    } else {
                        Text("Dieses Gerät benötigt eine einmalige Genehmigung, bevor Streams gestartet werden können.")
                            .font(.subheadline)
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .multilineTextAlignment(.center)

                        Button {
                            startPairing()
                        } label: {
                            Text("Kopplung starten")
                                .font(.headline)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 12)
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(Theme.Colors.accentAction)
                    }
                }

                if let error = model.lastError {
                    VStack(spacing: 8) {
                        HStack(spacing: 6) {
                            Image(systemName: "exclamationmark.triangle.fill")
                            Text(error)
                        }
                        .font(.footnote)
                        .foregroundStyle(Theme.Colors.statusError)
                        .multilineTextAlignment(.center)

                        Button("Anderen Server wählen") {
                            model.changeServer()
                        }
                        .font(.footnote.bold())
                        .foregroundStyle(Theme.Colors.accentAction)
                        .padding(.top, 4)
                    }
                    .padding()
                    .background(Theme.Colors.statusError.opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
                    .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.Colors.statusError.opacity(0.3), lineWidth: 1))
                }

                Spacer()
            }
            .padding(28)
            .task {
                if invitation == nil {
                    startPairing()
                }
            }
            .task(id: invitation?.pairingID) {
                guard invitation != nil else { return }
                isWaiting = true
                defer { isWaiting = false }

                while !Task.isCancelled {
                    if await model.pairingStatus() == .approved {
                        await model.completePairing()
                        return
                    }
                    try? await Task.sleep(for: .seconds(2))
                }
            }
        }
    }

    private func startPairing() {
        guard !isStarting else { return }
        isStarting = true
        Task {
            invitation = await model.beginPairing()
            isStarting = false
        }
    }
}
