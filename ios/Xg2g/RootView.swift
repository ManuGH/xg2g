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
                MainTabView(model: model)
            }
        }
        .preferredColorScheme(.dark)
        .tint(Theme.Colors.accentAction)
        .task { await model.start() }
        .onContinueUserActivity(HandoffCoordinator.activityType) { userActivity in
            guard let serviceRef = HandoffCoordinator.extractServiceRef(from: userActivity) else { return }
            Task { @MainActor in
                if model.channels.isEmpty {
                    await model.loadChannels()
                }
                if let target = model.channels.first(where: { $0.id == serviceRef || $0.serviceRef == serviceRef }) {
                    model.playingChannel = target
                }
            }
        }
    }
}

// MARK: - Main Tab View

struct MainTabView: View {

    @Bindable var model: AppModel

    var body: some View {
        TabView(selection: $model.selectedTab) {
            ChannelListView(model: model)
                .tabItem {
                    Label(Tab.liveTV.rawValue, systemImage: Tab.liveTV.systemImage)
                }
                .tag(Tab.liveTV)

            RecordingsView(model: model)
                .tabItem {
                    Label(Tab.recordings.rawValue, systemImage: Tab.recordings.systemImage)
                }
                .tag(Tab.recordings)

            TimersView(model: model)
                .tabItem {
                    Label(Tab.timers.rawValue, systemImage: Tab.timers.systemImage)
                }
                .tag(Tab.timers)

            SettingsView(model: model)
                .tabItem {
                    Label(Tab.settings.rawValue, systemImage: Tab.settings.systemImage)
                }
                .tag(Tab.settings)
        }
    }
}

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
                    .font(.system(size: 64))
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
                    TextField("tv.example oder 192.168.1.10:8080", text: $typed)
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

                    if let error = model.lastError {
                        Text(error)
                            .font(.footnote)
                            .foregroundStyle(Theme.Colors.statusError)
                    }

                    Button(action: connect) {
                        Text("Verbinden")
                            .font(.headline)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 14)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.Colors.accentAction)
                    .disabled(typed.trimmingCharacters(in: .whitespaces).isEmpty)
                }

                Spacer()
            }
            .padding(32)
        }
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
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            VStack(spacing: 28) {
                Spacer()

                Image(systemName: "lock.shield")
                    .font(.system(size: 56))
                    .foregroundStyle(Theme.Colors.accentLive)
                    .padding()
                    .background(Theme.Colors.surfaceGlass, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.Colors.borderElevated, lineWidth: 1))

                VStack(spacing: 8) {
                    Text(model.state == .needsRePairing ? "Gerät erneut koppeln" : "Gerät koppeln")
                        .font(.title2.bold())
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .multilineTextAlignment(.center)

                    if let invitation {
                        Text("Gib diesen Code in xg2g auf einem bereits gekoppelten Gerät ein:")
                            .font(.subheadline)
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .multilineTextAlignment(.center)

                        Text(invitation.userCode)
                            .font(.system(size: 42, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                            .textSelection(.enabled)
                            .padding(.vertical, 16)
                            .padding(.horizontal, 24)
                            .glassCard(cornerRadius: 16)

                        if isWaiting {
                            ProgressView("Warte auf Genehmigung…")
                                .tint(Theme.Colors.accentLive)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }
                    } else {
                        Text("Dieses Gerät benötigt eine einmalige Genehmigung, bevor Streams gestartet werden können.")
                            .font(.subheadline)
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .multilineTextAlignment(.center)

                        Button("Kopplung starten") {
                            Task { invitation = await model.beginPairing() }
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(Theme.Colors.accentAction)
                    }
                }

                if let error = model.lastError {
                    Text(error)
                        .font(.footnote)
                        .foregroundStyle(Theme.Colors.statusError)
                }

                Spacer()
            }
            .padding(32)
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
}
