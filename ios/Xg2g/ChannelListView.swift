import SwiftUI
import UIKit

@MainActor
private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
    Haptics.shared.impact(style)
}

/// Live TV station list & TV Pro inspired EPG with Quick Time-Jumps (Jetzt, 20:15, 22:00, Tage-Picker),
/// Genre filtering (Spielfilme, Serien, Sport, Doku...), View Switcher (Liste vs Magazin),
/// expandable multi-day schedules, direct 1-tap timer programming, and instant playback.
struct ChannelListView: View {

    @Bindable var model: AppModel
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - 1. Material Dynamic Filter Chip Carousel
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            // 🔴 Jetzt Live
                            let isNow = model.selectedTimeFilter == .now && model.selectedGenre == .all
                            Button {
                                triggerHaptic(.light)
                                withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                    model.selectedTimeFilter = .now
                                    model.selectedGenre = .all
                                }
                            } label: {
                                HStack(spacing: 5) {
                                    PulsingLiveDot(size: 6)
                                    Text("Jetzt Live")
                                        .font(.system(size: 13, weight: isNow ? .bold : .medium))
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 7)
                                .background(isNow ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                .foregroundStyle(isNow ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
                                .overlay { if !isNow { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                            }
                            .buttonStyle(.plain)

                            // ⭐️ Favoriten
                            if !model.favoriteChannelIDs.isEmpty {
                                let isFav = model.selectedBouquet?.id == AppModel.favoritesBouquetID
                                Button {
                                    triggerHaptic(.light)
                                    Task {
                                        if isFav {
                                            await model.selectBouquet(nil)
                                        } else {
                                            await model.selectBouquet(ChannelBouquet(id: AppModel.favoritesBouquetID, name: "Favoriten"))
                                        }
                                    }
                                } label: {
                                    HStack(spacing: 4) {
                                        Image(systemName: isFav ? "star.fill" : "star")
                                            .font(.system(size: 11))
                                        Text("Favoriten")
                                            .font(.system(size: 13, weight: isFav ? .bold : .medium))
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 7)
                                    .background(isFav ? Color.yellow : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                    .foregroundStyle(isFav ? Color.black : Theme.Colors.textPrimary)
                                    .overlay { if !isFav { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                                }
                                .buttonStyle(.plain)
                            }

                            // 🍿 20:15
                            let isPrime = model.selectedTimeFilter == .primeTimeTonight
                            Button {
                                triggerHaptic(.light)
                                withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                    model.selectedTimeFilter = .primeTimeTonight
                                }
                            } label: {
                                HStack(spacing: 4) {
                                    Image(systemName: "popcorn.fill")
                                        .font(.system(size: 11))
                                    Text("20:15")
                                        .font(.system(size: 13, weight: isPrime ? .bold : .medium))
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 7)
                                .background(isPrime ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                .foregroundStyle(isPrime ? Color.white : Theme.Colors.textPrimary)
                                .overlay { if !isPrime { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                            }
                            .buttonStyle(.plain)

                            // 🌙 22:00
                            let isLate = model.selectedTimeFilter == .lateNightTonight
                            Button {
                                triggerHaptic(.light)
                                withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                    model.selectedTimeFilter = .lateNightTonight
                                }
                            } label: {
                                HStack(spacing: 4) {
                                    Image(systemName: "moon.fill")
                                        .font(.system(size: 11))
                                    Text("22:00")
                                        .font(.system(size: 13, weight: isLate ? .bold : .medium))
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 7)
                                .background(isLate ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                .foregroundStyle(isLate ? Color.white : Theme.Colors.textPrimary)
                                .overlay { if !isLate { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                            }
                            .buttonStyle(.plain)

                            Divider()
                                .frame(height: 18)
                                .background(Theme.Colors.borderSubtle)

                            // Genre Chips (Sport, Spielfilme, Serien, Dokus, News...)
                            ForEach(EpgGenre.allCases.filter { $0 != .all }) { genre in
                                let isGenreSelected = model.selectedGenre == genre
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        model.selectedGenre = isGenreSelected ? .all : genre
                                    }
                                } label: {
                                    HStack(spacing: 4) {
                                        Image(systemName: genre.icon)
                                            .font(.system(size: 11))
                                        Text(genre.rawValue)
                                            .font(.system(size: 13, weight: isGenreSelected ? .bold : .medium))
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 7)
                                    .background(isGenreSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                    .foregroundStyle(isGenreSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
                                    .overlay { if !isGenreSelected { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    }
                    .fadingHorizontalEdges(fadeWidth: 16)
                    .background(Theme.Colors.surfaceElevated.opacity(0.25))

                    // MARK: - Main EPG Grid Content
                    let currentChannels = model.filteredChannels
                    Group {
                        if model.channels.isEmpty && model.isLoadingChannels {
                            Spacer()
                            ProgressView("Lade Sender und EPG-Daten…")
                                .tint(Theme.Colors.accentAction)
                                .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else if currentChannels.isEmpty {
                            Spacer()
                            ContentUnavailableView(
                                model.searchQuery.isEmpty ? (model.selectedGenre != .all ? "Keine \(model.selectedGenre.rawValue)" : "Keine Sender") : "Keine Treffer",
                                systemImage: model.selectedGenre != .all ? model.selectedGenre.icon : "tv.slash",
                                description: Text(model.searchQuery.isEmpty ? "Keine Sendungen für den gewählten Filter gefunden." : "Kein Sender entspricht deiner Suche.")
                            )
                            .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else {
                            let isRegular = sizeClass == .regular
                            ScrollView {
                                VStack(spacing: 16) {
                                    // 1. Favorite Spotlight Hero Banner (strictly only for favorite channels)
                                    if let spotlight = spotlightItem {
                                        GoogleSpotlightHero(
                                            channel: spotlight.channel,
                                            entry: spotlight.entry,
                                            model: model,
                                            onPlay: {
                                                model.playingChannel = spotlight.channel
                                            },
                                            onShowInfo: {
                                                selectedDetail = ProgramDetailPayload(channel: spotlight.channel, entry: spotlight.entry)
                                            },
                                            onRecord: {
                                                Task {
                                                    let ok = await model.scheduleProgramTimer(channel: spotlight.channel, entry: spotlight.entry)
                                                    if ok {
                                                        triggerHaptic(.medium)
                                                        withAnimation {
                                                            recordConfirmationMessage = "„\(spotlight.entry.title)“ programmiert"
                                                        }
                                                    }
                                                }
                                            }
                                        )
                                    }

                                    // 2. Favorites Quick Rail (if not already viewing the favorites bouquet)
                                    if model.selectedBouquet?.id != AppModel.favoritesBouquetID && !model.favoriteChannels.isEmpty && model.searchQuery.isEmpty {
                                        FavoritesQuickRail(
                                            channels: model.favoriteChannels,
                                            model: model,
                                            onPlay: { channel in
                                                model.playingChannel = channel
                                            },
                                            onShowInfo: { channel, entry in
                                                selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                                            }
                                        )
                                    }

                                    // 3. Recently Played Rail ("ZULETZT GESPIELT" - Sleek Glass Cards)
                                    if !model.recentChannels.isEmpty && model.searchQuery.isEmpty {
                                        RecentlyWatchedRail(
                                            channels: model.recentChannels,
                                            model: model,
                                            onPlay: { channel in
                                                model.playingChannel = channel
                                            },
                                            onShowInfo: { channel, entry in
                                                selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                                            }
                                        )
                                    }

                                    // 3. Station Grid Header
                                    HStack {
                                        Text(model.selectedBouquet?.name ?? "Alle Sender")
                                            .font(.headline.weight(.bold))
                                            .foregroundStyle(Theme.Colors.textPrimary)

                                        Spacer()

                                        Text("\(currentChannels.count) Sender")
                                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                    }
                                    .padding(.horizontal, 2)

                                    // 4. Responsive Channel Cards Grid (Optimized for Widescreen Landscape & Portrait)
                                    LazyVGrid(
                                        columns: [
                                            GridItem(.adaptive(minimum: 280, maximum: 420), spacing: 12)
                                        ],
                                        spacing: 12
                                    ) {
                                        ForEach(currentChannels) { channel in
                                            ChannelRow(
                                                channel: channel,
                                                nowNext: model.schedule[channel.serviceRef],
                                                fullSchedule: model.fullEpg[channel.serviceRef] ?? [],
                                                previewHours: model.epgPreviewHours,
                                                timeFilter: model.selectedTimeFilter,
                                                targetShow: model.show(for: channel, at: model.selectedTimeFilter),
                                                isFavorite: model.isFavorite(channel),
                                                onPlay: {
                                                    model.playingChannel = channel
                                                },
                                                onShowInfo: { entry in
                                                    selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                                                },
                                                onRecord: { entry in
                                                    Task {
                                                        let success = await model.scheduleProgramTimer(channel: channel, entry: entry)
                                                        if success {
                                                            triggerHaptic(.medium)
                                                            withAnimation {
                                                                recordConfirmationMessage = "„\(entry.title)“ programmiert"
                                                            }
                                                        }
                                                    }
                                                }
                                            )
                                            .contextMenu {
                                                Button {
                                                    model.playingChannel = channel
                                                } label: {
                                                    Label("Live schauen", systemImage: "play.fill")
                                                }

                                                if let now = model.schedule[channel.serviceRef]?.now {
                                                    Button {
                                                        selectedDetail = ProgramDetailPayload(channel: channel, entry: now)
                                                    } label: {
                                                        Label("Sendungsdetails", systemImage: "info.circle")
                                                    }

                                                    Button {
                                                        Task { _ = await model.scheduleProgramTimer(channel: channel, entry: now) }
                                                    } label: {
                                                        Label("„\(now.title)“ aufnehmen", systemImage: "record.circle")
                                                    }
                                                }

                                                if let next = model.schedule[channel.serviceRef]?.next {
                                                    Button {
                                                        Task { _ = await model.scheduleProgramTimer(channel: channel, entry: next) }
                                                    } label: {
                                                        Label("„\(next.title)“ aufnehmen", systemImage: "record.circle")
                                                    }
                                                }

                                                Button {
                                                    model.toggleFavorite(channel)
                                                } label: {
                                                    Label(
                                                        model.isFavorite(channel) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen",
                                                        systemImage: model.isFavorite(channel) ? "star.slash" : "star"
                                                    )
                                                }
                                            }
                                        }
                                    }
                                }
                                .padding(.horizontal, isRegular ? 20 : 12)
                                .padding(.vertical, 12)
                                .safeAreaPadding(.bottom, 80)
                            }
                            .refreshable {
                                await model.refreshLiveContent()
                            }
                        }
                    }
                }

                // MARK: - Toast Banner (Timer Scheduled)
                if let message = recordConfirmationMessage {
                    VStack {
                        Spacer()
                        HStack(spacing: 8) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(Theme.Colors.statusSuccess)
                            Text(message)
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                        .shadow(color: .black.opacity(0.35), radius: 10, y: 5)
                        .padding(.bottom, 20)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    .onAppear {
                        Task {
                            try? await Task.sleep(for: .seconds(3))
                            withAnimation { recordConfirmationMessage = nil }
                        }
                    }
                }
            }
            .navigationTitle(model.selectedBouquet?.name ?? "Alle Sender")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Menu {
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(nil) }
                        } label: {
                            HStack {
                                Text("Alle Sender (\(model.channels.count))")
                                if model.selectedBouquet == nil {
                                    Image(systemName: "checkmark")
                                }
                            }
                        }

                        if !model.favoriteChannelIDs.isEmpty {
                            Button {
                                triggerHaptic(.light)
                                Task { await model.selectBouquet(ChannelBouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
                            } label: {
                                HStack {
                                    Text("Favoriten (\(model.favoriteChannelIDs.count))")
                                    if model.selectedBouquet?.id == AppModel.favoritesBouquetID {
                                        Image(systemName: "checkmark")
                                    }
                                }
                            }
                        }

                        if !model.bouquets.isEmpty {
                            Divider()
                            ForEach(model.bouquets) { bouquet in
                                Button {
                                    triggerHaptic(.light)
                                    Task { await model.selectBouquet(bouquet) }
                                } label: {
                                    HStack {
                                        Text("\(bouquet.name)\(bouquet.servicesCount > 0 ? " (\(bouquet.servicesCount))" : "")")
                                        if model.selectedBouquet?.id == bouquet.id {
                                            Image(systemName: "checkmark")
                                        }
                                    }
                                }
                            }
                        }
                    } label: {
                        HStack(spacing: 5) {
                            Text(model.selectedBouquet?.name ?? "Alle Sender")
                                .font(.headline.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Image(systemName: "chevron.down.circle.fill")
                                .font(.system(size: 13, weight: .semibold))
                                .foregroundStyle(Theme.Colors.accentAction)
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(Theme.Colors.surfaceElevated.opacity(0.8), in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                }

                ToolbarItem(placement: .primaryAction) {
                    // Genre Filter Menu
                    Menu {
                        Picker("Genre", selection: $model.selectedGenre) {
                            ForEach(EpgGenre.allCases) { genre in
                                Label(genre.rawValue, systemImage: genre.icon).tag(genre)
                            }
                        }
                    } label: {
                        Image(systemName: model.selectedGenre == .all ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                            .font(.system(size: 19))
                            .foregroundStyle(model.selectedGenre == .all ? Theme.Colors.textSecondary : Theme.Colors.accentLive)
                    }
                }
            }
            .searchable(text: $model.searchQuery, prompt: "Sender oder Sendungen suchen…")
            .sheet(item: $selectedDetail) { payload in
                ProgramDetailSheet(
                    channel: payload.channel,
                    entry: payload.entry,
                    channelSchedule: model.channelSchedule(for: payload.channel),
                    model: model,
                    onRecord: { entry in
                        Task {
                            let ok = await model.scheduleProgramTimer(channel: payload.channel, entry: entry)
                            if ok {
                                triggerHaptic(.medium)
                                withAnimation {
                                    recordConfirmationMessage = "„\(entry.title)“ programmiert"
                                }
                            }
                        }
                    }
                )
            }
        }
    }

    // MARK: - Spotlight Hero Item (Only Favorites!)
    private var spotlightItem: (channel: Channel, entry: NowNext.Entry)? {
        guard model.searchQuery.isEmpty else { return nil }

        // STRICTLY ONLY favorite channels
        for channel in model.favoriteChannels {
            if let show = model.show(for: channel, at: model.selectedTimeFilter) {
                return (channel, show)
            }
        }

        return nil
    }
}

// MARK: - Google TV Spotlight Hero Banner (Highlights for Favorite Channels)

struct GoogleSpotlightHero: View {
    let channel: Channel
    let entry: NowNext.Entry
    let model: AppModel
    var onPlay: () -> Void
    var onShowInfo: () -> Void
    var onRecord: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header: "HIGHLIGHT" + Channel Logo + Live Pulsing Badge
            HStack(spacing: 10) {
                ChannelLogo(url: channel.logoURL, name: channel.name, size: 38)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 5) {
                        if let number = channel.number {
                            Text(number)
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(channel.name)
                            .font(.system(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Image(systemName: "star.fill")
                            .font(.system(size: 10))
                            .foregroundStyle(.yellow)
                    }

                    HStack(spacing: 5) {
                        PulsingLiveDot(size: 6)
                        Text(model.selectedTimeFilter == .now ? "FAVORIT JETZT LIVE" : "FAVORIT HIGHLIGHT")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                    }
                }

                Spacer()

                let g = entry.genre(channelName: channel.name)
                if g != .all {
                    Label(g.rawValue, systemImage: g.icon)
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(Theme.Colors.accentLive)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                }
            }

            // Title & Synopsis
            VStack(alignment: .leading, spacing: 4) {
                Text(entry.title)
                    .font(.title2.weight(.bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)

                if let desc = entry.description, !desc.isEmpty {
                    Text(desc)
                        .font(.subheadline)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(2)
                }
            }

            // Live Scrubber (if on air)
            if let progress = entry.progress(at: .now) {
                InfuseScrubber(
                    progress: progress,
                    startTime: entry.formattedStartTime,
                    endTime: entry.formattedEndTime,
                    remainingText: entry.remainingMinutes(at: .now).map { "noch \($0) Min" }
                )
            }

            // Google-Style Action Buttons
            HStack(spacing: 10) {
                // Big 1-Tap Play Button
                Button(action: onPlay) {
                    HStack(spacing: 6) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 13, weight: .bold))
                        Text("Jetzt ansehen")
                            .font(.system(size: 14, weight: .bold))
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 10)
                    .background(Theme.Colors.accentAction, in: Capsule())
                    .foregroundStyle(.white)
                }
                .buttonStyle(.plain)

                // 1-Tap Record Button
                Button(action: onRecord) {
                    HStack(spacing: 5) {
                        Image(systemName: "record.circle")
                            .font(.system(size: 13, weight: .semibold))
                        Text("Aufnehmen")
                            .font(.system(size: 13, weight: .semibold))
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                // Info / Wiederholungen Button
                Button(action: onShowInfo) {
                    Image(systemName: "info.circle")
                        .font(.system(size: 16))
                        .padding(10)
                        .background(Theme.Colors.surfaceElevated, in: Circle())
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)
            }
            .padding(.top, 4)
        }
        .padding(18)
        .background(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [
                            Theme.Colors.surfaceElevated.opacity(0.95),
                            Color(red: 0.08, green: 0.12, blue: 0.18)
                        ],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
        )
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Theme.Gradients.liveAuraBorder, lineWidth: 1.2)
        )
        .shadow(color: Theme.Colors.accentLive.opacity(0.12), radius: 12, y: 4)
    }
}

// MARK: - Favorites Quick Access Rail (Top Highlight Carousel)

struct FavoritesQuickRail: View {
    let channels: [Channel]
    let model: AppModel
    var onPlay: (Channel) -> Void
    var onShowInfo: (Channel, NowNext.Entry) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "star.fill")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(.yellow)
                Text("MEINE FAVORITEN")
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Spacer()

                Text("\(channels.count) Sender")
                    .font(.system(size: 10, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .padding(.horizontal, 2)

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 12) {
                    ForEach(channels) { channel in
                        let nowNext = model.schedule[channel.serviceRef]
                        Button {
                            triggerHaptic(.light)
                            onPlay(channel)
                        } label: {
                            HStack(spacing: 10) {
                                ChannelLogo(url: channel.logoURL, name: channel.name, size: 36)

                                VStack(alignment: .leading, spacing: 2) {
                                    HStack(spacing: 4) {
                                        if let number = channel.number {
                                            Text(number)
                                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                                .foregroundStyle(Theme.Colors.accentAction)
                                                .padding(.horizontal, 4)
                                                .padding(.vertical, 1)
                                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))
                                        }

                                        Text(channel.name)
                                            .font(.system(size: 13, weight: .bold))
                                            .foregroundStyle(Theme.Colors.textPrimary)
                                            .lineLimit(1)
                                    }

                                    if let now = nowNext?.now {
                                        Text(now.title)
                                            .font(.system(size: 11, weight: .medium))
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                            .lineLimit(1)
                                    } else {
                                        Text("Live TV")
                                            .font(.system(size: 11))
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                    }
                                }

                                Image(systemName: "play.circle.fill")
                                    .font(.system(size: 18))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.leading, 2)
                            }
                            .padding(.horizontal, 12)
                            .padding(.vertical, 8)
                            .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                            .overlay(
                                RoundedRectangle(cornerRadius: 12, style: .continuous)
                                    .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                            )
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }
}

// MARK: - Recently Played Rail ("ZULETZT GESPIELT" - Premium Frosted Glass Cards)

struct RecentlyWatchedRail: View {
    let channels: [Channel]
    let model: AppModel
    var onPlay: (Channel) -> Void
    var onShowInfo: (Channel, NowNext.Entry) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "bolt.fill")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("ZULETZT GESPIELT")
                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Spacer()

                Text("\(channels.count) Sender")
                    .font(.system(size: 10, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .padding(.horizontal, 2)

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 12) {
                    ForEach(channels) { channel in
                        let nowNext = model.schedule[channel.serviceRef]
                        RecentChannelCard(
                            channel: channel,
                            nowNext: nowNext,
                            onPlay: { onPlay(channel) },
                            onShowInfo: { entry in onShowInfo(channel, entry) }
                        )
                    }
                }
            }
        }
    }
}

// MARK: - Recent Channel Card Component (Infuse / Apple TV+ Aesthetics)

struct RecentChannelCard: View {
    let channel: Channel
    let nowNext: NowNext?
    var onPlay: () -> Void
    var onShowInfo: (NowNext.Entry) -> Void

    var body: some View {
        Button {
            triggerHaptic(.light)
            onPlay()
        } label: {
            VStack(alignment: .leading, spacing: 9) {
                // Top Header: Logo + Channel Name & Number + Live Pulse
                HStack(spacing: 10) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 38)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            if let number = channel.number {
                                Text(number)
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1.5)
                                    .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                            }

                            Text(channel.name)
                                .font(.system(size: 14, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                        }

                        if let now = nowNext?.now {
                            HStack(spacing: 5) {
                                PulsingLiveDot(size: 5)
                                Text(now.formattedTimeRange)
                                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }
                        }
                    }

                    Spacer(minLength: 4)

                    if let now = nowNext?.now, let remaining = now.remainingMinutes(at: .now) {
                        Text("noch \(remaining)m")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                            .padding(.horizontal, 7)
                            .padding(.vertical, 3)
                            .background(Theme.Colors.accentLive.opacity(0.14), in: Capsule())
                    }
                }

                // Middle: Show Title & Progress Scrubber
                if let now = nowNext?.now {
                    VStack(alignment: .leading, spacing: 5) {
                        HStack(alignment: .top) {
                            Text(now.title)
                                .font(.system(size: 13, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            Spacer(minLength: 4)

                            Button {
                                triggerHaptic(.light)
                                onShowInfo(now)
                            } label: {
                                Image(systemName: "info.circle")
                                    .font(.system(size: 13))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                    .padding(2)
                            }
                            .buttonStyle(.plain)
                        }

                        // Gradient Live Progress Bar
                        if let fraction = now.progress(at: .now) {
                            GeometryReader { geo in
                                ZStack(alignment: .leading) {
                                    Capsule()
                                        .fill(Color.white.opacity(0.12))
                                        .frame(height: 3)

                                    Capsule()
                                        .fill(
                                            LinearGradient(
                                                colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                                startPoint: .leading,
                                                endPoint: .trailing
                                            )
                                        )
                                        .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(fraction))), height: 3)
                                }
                            }
                            .frame(height: 3)
                        }
                    }
                } else {
                    Text("Keine Programminformationen")
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .padding(.vertical, 2)
                }

                // Bottom: Action Pill
                HStack {
                    HStack(spacing: 5) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 10, weight: .bold))
                        Text("Weiterschauen")
                            .font(.system(size: 11, weight: .bold))
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 4.5)
                    .background(Theme.Colors.accentAction.opacity(0.18), in: Capsule())
                    .foregroundStyle(Theme.Colors.accentAction)
                    .overlay(Capsule().strokeBorder(Theme.Colors.accentAction.opacity(0.35), lineWidth: 0.8))

                    Spacer()

                    if let next = nowNext?.next {
                        Text("Danach: \(next.title)")
                            .font(.system(size: 10))
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .lineLimit(1)
                            .frame(maxWidth: 105, alignment: .trailing)
                    }
                }
            }
            .padding(12)
            .frame(width: 250)
            .background(
                RoundedRectangle(cornerRadius: 15, style: .continuous)
                    .fill(
                        LinearGradient(
                            colors: [
                                Theme.Colors.surfaceElevated.opacity(0.95),
                                Color(red: 0.08, green: 0.11, blue: 0.16)
                            ],
                            startPoint: .topLeading,
                            endPoint: .bottomTrailing
                        )
                    )
            )
            .overlay(
                RoundedRectangle(cornerRadius: 15, style: .continuous)
                    .strokeBorder(Theme.Gradients.liveAuraBorder, lineWidth: 0.8)
            )
            .shadow(color: Theme.Colors.accentLive.opacity(0.08), radius: 8, y: 3)
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Time Filter Pill Component

struct TimeFilterPill: View {
    let filter: AppModel.TimeFilter
    let isSelected: Bool
    let onSelect: () -> Void

    var body: some View {
        Button {
            onSelect()
        } label: {
            HStack(spacing: 6) {
                if filter == .now {
                    PulsingLiveDot(size: 6)
                } else {
                    Image(systemName: filter.icon)
                        .font(.system(size: 11))
                        .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.accentAction)
                }

                Text(filter.label)
                    .font(.system(size: 13, weight: isSelected ? .bold : .medium))
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(
                isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85),
                in: Capsule()
            )
            .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
            .overlay {
                if !isSelected {
                    Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                }
            }
        }
        .buttonStyle(.plain)
    }
}

// MARK: - TV Pro Magazine Card (Visual & Cinematic)

struct MagazineChannelCard: View {

    let channel: Channel
    let targetShow: NowNext.Entry?
    let timeFilter: AppModel.TimeFilter
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Top Bar: Logo, Name, Number & Genre
            HStack(spacing: 10) {
                Button(action: onPlay) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 44)
                }
                .buttonStyle(.plain)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text(number)
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(channel.name)
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if isFavorite {
                            Image(systemName: "star.fill")
                                .font(.caption2)
                                .foregroundStyle(.yellow)
                        }
                    }

                    if let show = targetShow {
                        HStack(spacing: 6) {
                            if timeFilter == .now {
                                PulsingLiveDot(size: 5)
                            }
                            Text(show.formattedTimeRange)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(timeFilter == .now ? Theme.Colors.accentLive : Theme.Colors.accentAction)

                            Text("• \(show.durationMinutes)m")
                                .font(.system(size: 10, weight: .regular))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                }

                Spacer(minLength: 4)

                // Genre Tag
                if let show = targetShow {
                    let g = show.genre(channelName: channel.name)
                    if g != .all {
                        Text(g.rawValue)
                            .font(.system(size: 10, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Theme.Colors.surfaceElevated, in: Capsule())
                    }
                }
            }

            // Main Content Area: Show Title & Synopsis
            if let show = targetShow {
                Button {
                    onShowInfo(show)
                } label: {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(show.title)
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(2)
                            .multilineTextAlignment(.leading)

                        if let desc = show.description, !desc.isEmpty {
                            Text(desc)
                                .font(.system(size: 13))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(3)
                                .multilineTextAlignment(.leading)
                        }
                    }
                }
                .buttonStyle(.plain)

                // Scrubber (if running now)
                if timeFilter == .now, let fraction = show.progress(at: .now) {
                    InfuseScrubber(
                        progress: fraction,
                        startTime: show.formattedStartTime,
                        endTime: show.formattedEndTime,
                        remainingText: nil
                    )
                    .padding(.top, 2)
                }
            } else {
                Text("Keine Programminformationen für diese Sendezeit verfügbar")
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .padding(.vertical, 8)
            }

            Divider()
                .background(Theme.Colors.borderSubtle)

            // Bottom Action Bar (Play, Record, Details)
            HStack(spacing: 12) {
                // Play Live Button
                Button(action: onPlay) {
                    HStack(spacing: 6) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 12))
                        Text(timeFilter == .now ? "Live schauen" : "Sender starten")
                            .font(.system(size: 12, weight: .bold))
                    }
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 7)
                    .background(Theme.Colors.accentAction, in: Capsule())
                }
                .buttonStyle(.plain)

                Spacer()

                if let show = targetShow {
                    // Record / Timer Button
                    Button {
                        onRecord(show)
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "record.circle")
                                .font(.system(size: 14))
                            Text("Aufnehmen")
                                .font(.system(size: 11, weight: .semibold))
                        }
                        .foregroundStyle(Theme.Colors.statusError)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .background(Theme.Colors.statusError.opacity(0.12), in: Capsule())
                    }
                    .buttonStyle(.plain)

                    // Details Info Button
                    Button {
                        onShowInfo(show)
                    } label: {
                        Image(systemName: "info.circle")
                            .font(.system(size: 16))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .padding(6)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(Theme.Gradients.cardSurface)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(timeFilter == .now ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: 1)
        )
        .shadow(
            color: timeFilter == .now ? Theme.Colors.accentLive.opacity(0.16) : Color.black.opacity(0.22),
            radius: timeFilter == .now ? 10 : 5,
            y: timeFilter == .now ? 2 : 2
        )
    }
}

// MARK: - Program Detail Payload

struct ProgramDetailPayload: Identifiable {
    var id: String { "\(channel.id)_\(entry.id)" }
    let channel: Channel
    let entry: NowNext.Entry
}

// MARK: - Channel Row (Modern Sleek TV Card - Optimized for Landscape & Portrait)

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    let fullSchedule: [NowNext.Entry]
    var previewHours: AppModel.EpgPreviewHours = .fourHours
    var timeFilter: AppModel.TimeFilter = .now
    var targetShow: NowNext.Entry? = nil
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        let displayedShow = targetShow ?? nowNext?.now

        Button {
            triggerHaptic(.light)
            onPlay()
        } label: {
            VStack(alignment: .leading, spacing: 9) {
                // MARK: - Header: Logo + Channel Name & Number + Genre/Live Badge
                HStack(spacing: 10) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 38)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            if let number = channel.number {
                                Text(number)
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1.5)
                                    .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                            }

                            Text(channel.name)
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            if isFavorite {
                                Image(systemName: "star.fill")
                                    .font(.system(size: 10))
                                    .foregroundStyle(.yellow)
                            }
                        }
                    }

                    Spacer(minLength: 4)

                    if let show = displayedShow {
                        let currentGenre = show.genre(channelName: channel.name)
                        HStack(spacing: 6) {
                            if timeFilter == .now {
                                PulsingLiveDot(size: 5)
                            }
                            if currentGenre != .all {
                                Text(currentGenre.rawValue)
                                    .font(.system(size: 10, weight: .semibold))
                                    .foregroundStyle(timeFilter == .now ? Theme.Colors.accentLive : Theme.Colors.textSecondary)
                                    .padding(.horizontal, 7)
                                    .padding(.vertical, 2.5)
                                    .background(
                                        timeFilter == .now ? Theme.Colors.accentLive.opacity(0.14) : Theme.Colors.surfaceElevated,
                                        in: Capsule()
                                    )
                            }
                        }
                    }
                }

                // MARK: - Show Details & Clean Timing
                if let show = displayedShow {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(alignment: .top) {
                            Text(show.title)
                                .font(.system(size: 14, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            Spacer(minLength: 4)

                            // Quick Info Button to open Detail Sheet
                            Button {
                                triggerHaptic(.light)
                                onShowInfo(show)
                            } label: {
                                Image(systemName: "info.circle")
                                    .font(.system(size: 14))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                    .padding(2)
                            }
                            .buttonStyle(.plain)
                        }

                        HStack(spacing: 6) {
                            Text(show.formattedTimeRange)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textSecondary)

                            if timeFilter == .now, let remaining = show.remainingMinutes(at: .now) {
                                Text("• noch \(remaining) Min")
                                    .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            } else {
                                Text("• \(show.durationMinutes) Min")
                                    .font(.system(size: 11, weight: .regular))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }

                        if let desc = show.description, !desc.isEmpty {
                            Text(desc)
                                .font(.system(size: 12))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .lineLimit(1)
                                .padding(.top, 1)
                        }
                    }

                    // Sleek Live Progress Bar
                    if timeFilter == .now, let fraction = show.progress(at: .now) {
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule()
                                    .fill(Color.white.opacity(0.12))
                                    .frame(height: 3)

                                Capsule()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(fraction))), height: 3)
                            }
                        }
                        .frame(height: 3)
                        .padding(.top, 2)
                    }
                } else {
                    Text("Keine Programminformationen verfügbar")
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .padding(.vertical, 4)
                }

                // MARK: - Next Show Preview ("Danach")
                if timeFilter == .now, let next = nowNext?.next {
                    HStack(spacing: 6) {
                        Text("DANACH:")
                            .font(.system(size: 9, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)

                        Text(next.formattedStartTime)
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)

                        Text(next.title)
                            .font(.system(size: 11))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)

                        Spacer()
                    }
                    .padding(.top, 2)
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .fill(Theme.Gradients.cardSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(timeFilter == .now ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: 0.8)
            )
            .shadow(
                color: timeFilter == .now ? Theme.Colors.accentLive.opacity(0.12) : Color.black.opacity(0.18),
                radius: 6,
                y: 2
            )
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Channel Logo View (Infuse Style: Clean, Floating & Transparent with In-Memory Cache)

final class LogoImageCache: @unchecked Sendable {
    static let shared = LogoImageCache()
    private let cache = NSCache<NSString, UIImage>()

    init() {
        cache.countLimit = 500
        cache.totalCostLimit = 32 * 1024 * 1024
    }

    /// Rounds a required pixel size up to a shared bucket.
    ///
    /// The app draws logos at six different point sizes; keyed verbatim that
    /// would be six decoded copies of every logo. Two buckets cover all of them
    /// and still decode far below the source resolution.
    static func bucket(forPointSize points: CGFloat, scale: CGFloat) -> Int {
        let pixels = Int((points * scale).rounded(.up))
        let rounded = ((pixels + 63) / 64) * 64
        return max(128, rounded)
    }

    /// Every bucket `bucket(forPointSize:scale:)` can currently produce, ascending.
    /// Used only to look up "whatever we already have" — a miss just means no
    /// artwork yet, which is what an uncached logo produced before as well.
    private static let knownBuckets = [128, 192, 256, 512]

    func image(for url: URL, bucket: Int) -> UIImage? {
        cache.object(forKey: Self.key(url, bucket))
    }

    /// The largest cached rendition for this URL, for consumers that do not draw
    /// at a fixed size — Now Playing artwork, for instance.
    func anyImage(for url: URL) -> UIImage? {
        for bucket in Self.knownBuckets.reversed() {
            if let image = cache.object(forKey: Self.key(url, bucket)) {
                return image
            }
        }
        return nil
    }

    /// Stores with an explicit `cost`. Without one every entry counts as zero and
    /// `totalCostLimit` never evicts anything — only `countLimit` applied.
    func store(_ image: UIImage, for url: URL, bucket: Int) {
        let cost = image.cgImage.map { $0.bytesPerRow * $0.height } ?? bucket * bucket * 4
        cache.setObject(image, forKey: Self.key(url, bucket), cost: cost)
    }

    private static func key(_ url: URL, _ bucket: Int) -> NSString {
        "\(url.absoluteString)|\(bucket)" as NSString
    }

    /// Decodes straight to the target size via ImageIO.
    ///
    /// `UIImage(data:)` defers decoding until draw time, which put a full
    /// resolution bitmap decode on the main thread during scrolling. The
    /// thumbnail path decodes once, off the main thread, at the size actually
    /// drawn.
    static func downsampledImage(from data: Data, bucket: Int) -> UIImage? {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else {
            return nil
        }

        let thumbnailOptions = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: bucket
        ] as CFDictionary

        guard let cgImage = CGImageSourceCreateThumbnailAtIndex(source, 0, thumbnailOptions) else {
            return nil
        }
        return UIImage(cgImage: cgImage)
    }
}

struct ChannelLogo: View {
    let url: URL?
    let name: String
    var size: CGFloat = 48

    @State private var loadedImage: UIImage?

    private var bucket: Int {
        LogoImageCache.bucket(forPointSize: size, scale: UIScreen.main.scale)
    }

    var body: some View {
        let currentImage = loadedImage ?? (url.flatMap { LogoImageCache.shared.image(for: $0, bucket: bucket) })

        ZStack {
            if let img = currentImage {
                Image(uiImage: img)
                    .resizable()
                    .scaledToFit()
                    .frame(width: size, height: size)
                    .shadow(color: Color.black.opacity(0.55), radius: 3, y: 1.5)
            } else if let url {
                fallbackBadge
                    .task(id: url) {
                        await loadLogo(from: url)
                    }
            } else {
                fallbackBadge
            }
        }
        .frame(width: size, height: size)
    }

    private func loadLogo(from url: URL) async {
        let target = bucket

        if let cached = LogoImageCache.shared.image(for: url, bucket: target) {
            loadedImage = cached
            return
        }

        guard let data = await MediaFetcher.imageData(from: url), !Task.isCancelled else {
            return
        }

        // Decoding is CPU work on a scrolling list's critical path, so it runs
        // off the main actor rather than implicitly during draw.
        let image = await Task.detached(priority: .utility) {
            LogoImageCache.downsampledImage(from: data, bucket: target)
        }.value

        guard let image, !Task.isCancelled else { return }
        LogoImageCache.shared.store(image, for: url, bucket: target)
        loadedImage = image
    }

    private var fallbackBadge: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                .fill(Color.white.opacity(0.06))
                .overlay(
                    RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                        .strokeBorder(Color.white.opacity(0.12), lineWidth: 1)
                )

            Text(String(name.prefix(2)).uppercased())
                .font(.system(size: size * 0.35, weight: .bold, design: .rounded))
                .foregroundStyle(Theme.Colors.textSecondary)
        }
        .frame(width: size, height: size)
        .shadow(color: Color.black.opacity(0.3), radius: 3, y: 1.5)
    }
}

