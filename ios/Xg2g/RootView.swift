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
    @State private var isStarting = false

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            VStack(spacing: 24) {
                Spacer()

                Image(systemName: "lock.shield")
                    .font(.system(size: 56))
                    .foregroundStyle(Theme.Colors.accentLive)
                    .padding()
                    .background(Theme.Colors.surfaceGlass, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.Colors.borderElevated, lineWidth: 1))

                VStack(spacing: 12) {
                    Text(model.state == .needsRePairing ? "Gerät erneut koppeln" : "Gerät koppeln")
                        .font(.title2.bold())
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .multilineTextAlignment(.center)

                    Text(model.serverURLString)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.accentAction)
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
                            .font(.system(size: 42, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                            .textSelection(.enabled)
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
                        VStack(spacing: 12) {
                            ProgressView()
                                .tint(Theme.Colors.accentAction)
                            Text("Generiere P-256 Hardwareschlüssel & starte Kopplung…")
                                .font(.footnote)
                                .foregroundStyle(Theme.Colors.textSecondary)
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
