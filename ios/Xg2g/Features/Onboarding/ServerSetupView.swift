// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Setup

struct ServerSetupView: View {

    let model: AppModel
    @State private var typed = ""

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            VStack(spacing: 28) {
                Spacer()

                Image(systemName: "tv")
                    .font(.app(size: 64))
                    .foregroundStyle(Theme.Colors.accentAction)
                    .padding()
                    .background(Theme.Colors.surfaceGlass, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.Colors.borderElevated, lineWidth: 1))

                VStack(spacing: 8) {
                    Text("Mit xg2g verbinden")
                        .font(.title2.bold())
                        .foregroundStyle(Theme.Colors.textPrimary)

                    Text("Gib die Adresse deines xg2g-Servers ein.")
                        .font(.subheadline)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .multilineTextAlignment(.center)
                }

                VStack(spacing: 16) {
                    TextField("tv.example oder xg2g.home.matrixcentral.de", text: $typed)
                        .padding()
                        .background(Theme.Colors.surfaceElevated)
                        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                        .overlay(
                            RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .strokeBorder(Theme.Colors.borderElevated, lineWidth: 1)
                        )
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .submitLabel(.go)
                        .onSubmit { connect() }

                    HStack(spacing: 8) {
                        Button {
                            typed = "xg2g.home.matrixcentral.de"
                            connect()
                        } label: {
                            Label("xg2g.home.matrixcentral.de", systemImage: "bolt.horizontal.circle")
                                .font(.caption.weight(.medium))
                        }
                        .buttonStyle(.bordered)
                        .tint(Theme.Colors.accentAction)
                    }

                    if let error = model.lastError {
                        Text(error)
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.statusError)
                    }

                    Button {
                        connect()
                    } label: {
                        Text("Verbinden")
                            .font(.headline)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 12)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.Colors.accentAction)
                    .disabled(typed.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                .frame(maxWidth: 420)

                Spacer()
            }
            .padding(28)
        }
    }

    private func connect() {
        let trimmed = typed.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return }
        Task { await model.useServer(trimmed) }
    }
}
