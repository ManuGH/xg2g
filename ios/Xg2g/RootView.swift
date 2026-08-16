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
                AdaptiveAppNavigation(model: model)
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

// MARK: - Adaptive App Navigation (iPhone TabView vs iPadOS NavigationSplitView)

struct AdaptiveAppNavigation: View {

    @Bindable var model: AppModel
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    var body: some View {
        if horizontalSizeClass == .regular {
            // MARK: - iPadOS NavigationSplitView with Glass Sidebar
            NavigationSplitView {
                iPadSidebar(model: model)
            } detail: {
                switch model.selectedTab {
                case .liveTV:
                    ChannelListView(model: model)
                case .guide:
                    GuideView(model: model)
                case .recordings:
                    RecordingsView(model: model)
                case .timers:
                    TimersView(model: model)
                case .settings:
                    SettingsView(model: model)
                }
            }
            .navigationSplitViewStyle(.balanced)
        } else {
            // MARK: - iOS Compact TabView
            MainTabView(model: model)
        }
    }
}

// MARK: - iPadOS Sidebar

struct iPadSidebar: View {

    @Bindable var model: AppModel

    var body: some View {
        List {
            Section("Mediathek") {
                ForEach(Tab.allCases) { tab in
                    let isSelected = model.selectedTab == tab
                    Button {
                        triggerHaptic(.light)
                        model.selectedTab = tab
                    } label: {
                        HStack(spacing: 12) {
                            SettingsIconBadge(
                                systemName: tab.systemImage,
                                backgroundColor: isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated
                            )

                            Text(tab.rawValue)
                                .font(.system(size: 15, weight: isSelected ? .bold : .medium))
                                .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                            Spacer()

                            if isSelected {
                                Image(systemName: "chevron.right")
                                    .font(.system(size: 11, weight: .bold))
                                    .foregroundStyle(Theme.Colors.accentAction)
                            }
                        }
                        .padding(.vertical, 2)
                        .padding(.horizontal, 4)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .listRowBackground(
                        isSelected
                            ? RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .fill(Theme.Gradients.sidebarActiveSelection)
                                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.Colors.accentAction.opacity(0.3), lineWidth: 1))
                            : nil
                    )
                }
            }

            if (model.selectedTab == .liveTV || model.selectedTab == .guide) && !model.bouquets.isEmpty {
                Section("Bouquets & Sendergruppen") {
                    // Alle Sender
                    let isAllSelected = model.selectedBouquet == nil
                    Button {
                        triggerHaptic(.light)
                        Task { await model.selectBouquet(nil) }
                    } label: {
                        HStack(spacing: 10) {
                            Image(systemName: "tv.circle.fill")
                                .font(.system(size: 18))
                                .foregroundStyle(isAllSelected ? Theme.Colors.accentAction : Theme.Colors.textTertiary)

                            Text("Alle Sender")
                                .font(.system(size: 14, weight: isAllSelected ? .bold : .medium))
                                .foregroundStyle(isAllSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                            Spacer()

                            Text("\(model.channels.count)")
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.surfaceElevated, in: Capsule())
                        }
                        .padding(.vertical, 2)
                    }
                    .buttonStyle(.plain)

                    // Favoriten
                    if !model.favoriteChannelIDs.isEmpty {
                        let isFavSelected = model.selectedBouquet?.id == AppModel.favoritesBouquetID
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(Bouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
                        } label: {
                            HStack(spacing: 10) {
                                Image(systemName: "star.circle.fill")
                                    .font(.system(size: 18))
                                    .foregroundStyle(Theme.Colors.accentLive)

                                Text("Favoriten")
                                    .font(.system(size: 14, weight: isFavSelected ? .bold : .medium))
                                    .foregroundStyle(isFavSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                                Spacer()

                                Text("\(model.favoriteChannelIDs.count)")
                                    .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 7)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                            }
                            .padding(.vertical, 2)
                        }
                        .buttonStyle(.plain)
                    }

                    // Bouquets
                    ForEach(model.bouquets) { bouquet in
                        let isSelected = model.selectedBouquet?.id == bouquet.id
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(bouquet) }
                        } label: {
                            HStack(spacing: 10) {
                                Image(systemName: "folder.fill")
                                    .font(.system(size: 16))
                                    .foregroundStyle(isSelected ? Theme.Colors.accentAction : Theme.Colors.textTertiary)

                                Text(bouquet.name)
                                    .font(.system(size: 14, weight: isSelected ? .bold : .medium))
                                    .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                    .lineLimit(1)

                                Spacer()

                                if bouquet.servicesCount > 0 {
                                    Text("\(bouquet.servicesCount)")
                                        .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                        .padding(.horizontal, 7)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                                }
                            }
                            .padding(.vertical, 2)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }

            Section {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 6) {
                        PulsingLiveDot(size: 6)
                        Text(model.serverURLString)
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    }
                    Text("xg2g Broadcast System • 2026")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textDisabled)
                }
                .padding(10)
                .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                .padding(.vertical, 2)
            }
            .listRowBackground(Color.clear)
        }
        .listStyle(.sidebar)
        .scrollContentBackground(.hidden)
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .navigationSplitViewColumnWidth(min: 270, ideal: 300, max: 360)
        .navigationTitle("xg2g TV")
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        UIImpactFeedbackGenerator(style: style).impactOccurred()
    }
}

// MARK: - Main Tab View (Compact iPhone Mode)

struct MainTabView: View {

    @Bindable var model: AppModel

    var body: some View {
        TabView(selection: $model.selectedTab) {
            ChannelListView(model: model)
                .tabItem {
                    Label(Tab.liveTV.rawValue, systemImage: Tab.liveTV.systemImage)
                }
                .tag(Tab.liveTV)

            GuideView(model: model)
                .tabItem {
                    Label(Tab.guide.rawValue, systemImage: Tab.guide.systemImage)
                }
                .tag(Tab.guide)

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
                    .font(.system(size: 64))
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
