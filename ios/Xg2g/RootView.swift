// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// The one place that decides which screen the app is on.
///
/// Routing off `AppState` rather than off a pile of optionals: every screen
/// here corresponds to exactly one state, so there is no combination that lands
/// on a view nobody designed.
struct RootView: View {

    @State private var model = AppModel()

    var body: some View {
        Group {
            switch model.state {
            case .needsServer:
                ServerSetupView(model: model)
            case .needsPairing, .needsRePairing:
                PairingView(model: model)
            case .ready:
                ChannelListView(model: model)
            }
        }
        .task { await model.start() }
    }
}

// MARK: - Setup

struct ServerSetupView: View {

    let model: AppModel
    @State private var typed = ""

    var body: some View {
        VStack(spacing: 24) {
            Image(systemName: "tv")
                .font(.system(size: 64))
                .foregroundStyle(.tint)

            Text("Connect to xg2g")
                .font(.title.bold())

            Text("Enter the address of your xg2g server.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            TextField("tv.example or 192.168.1.10:8080", text: $typed)
                .textFieldStyle(.roundedBorder)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .submitLabel(.go)
                .onSubmit { connect() }

            if let error = model.lastError {
                Text(error)
                    .font(.footnote)
                    .foregroundStyle(.red)
            }

            Button("Continue", action: connect)
                .buttonStyle(.borderedProminent)
                .disabled(typed.trimmingCharacters(in: .whitespaces).isEmpty)

            Spacer()
        }
        .padding(32)
    }

    private func connect() {
        Task { await model.useServer(typed) }
    }
}

// MARK: - Pairing

struct PairingView: View {

    let model: AppModel
    @State private var invitation: EnrollmentCoordinator.Invitation?
    @State private var isWaiting = false

    var body: some View {
        VStack(spacing: 24) {
            Image(systemName: "lock.shield")
                .font(.system(size: 56))
                .foregroundStyle(.tint)

            Text(model.state == .needsRePairing ? "Pair this device again" : "Pair this device")
                .font(.title.bold())
                .multilineTextAlignment(.center)

            if let invitation {
                Text("Enter this code in xg2g on another device:")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)

                Text(invitation.userCode)
                    .font(.system(.largeTitle, design: .monospaced).bold())
                    .textSelection(.enabled)
                    .padding(.vertical, 8)

                if isWaiting {
                    ProgressView("Waiting for approval…")
                }
            } else {
                Text("This device needs approval before it can watch anything.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)

                Button("Start pairing") {
                    Task { invitation = await model.beginPairing() }
                }
                .buttonStyle(.borderedProminent)
            }

            if let error = model.lastError {
                Text(error).font(.footnote).foregroundStyle(.red)
            }

            Spacer()
        }
        .padding(32)
        // The poll lives here rather than in the coordinator: how often to ask,
        // and for how long, is a screen's decision. A coordinator that looped on
        // its own would keep polling a screen the user had already left.
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
