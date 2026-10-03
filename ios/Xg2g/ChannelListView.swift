import SwiftUI
import UIKit

@MainActor
private func triggerHaptic(_ style: Haptics.FeedbackStyle) {
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
                    if !model.searchQuery.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        let searchResult = SmartSearchEngine.search(
                            query: model.searchQuery,
                            channels: model.channels,
                            schedule: model.schedule,
                            fullEpg: model.fullEpg,
                            now: Date.now
                        )
                        SmartSearchResultsView(
                            result: searchResult,
                            onPlayChannel: { channel in
                                model.playingChannel = channel
                            },
                            onOpenShowDetail: { channel, entry in
                                selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                            },
                            onRecordShow: { channel, entry in
                                Task {
                                    let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
                                    if ok {
                                        triggerHaptic(.medium)
                                        withAnimation {
                                            recordConfirmationMessage = String(localized: "“\(entry.title)” scheduled")
                                        }
                                    }
                                }
                            }
                        )
                    } else {
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
                                    Text("Live Now")
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
                                            await model.selectBouquet(AppModel.favoritesBouquet)
                                        }
                                    }
                                } label: {
                                    HStack(spacing: 4) {
                                        Image(systemName: isFav ? "star.fill" : "star")
                                            .font(.system(size: 11))
                                        Text("Favorites")
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
                                    Text(AppModel.TimeFilter.formatPresetTime(hour: 20, minute: 15))
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
                                    Text(AppModel.TimeFilter.formatPresetTime(hour: 22, minute: 0))
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
                                        Text(genre.localizedTitle)
                                            .font(.system(size: 13, weight: isGenreSelected ? .bold : .medium))
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 7)
                                    .background(isGenreSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85), in: Capsule())
                                    .foregroundStyle(isGenreSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
                                    .overlay { if !isGenreSelected { Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8) } }
                                    .accessibilityLabel(String(localized: "Genre: \(String(localized: genre.localizedTitle))"))
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
                            ProgressView("Loading channels and EPG data…")
                                .tint(Theme.Colors.accentAction)
                                .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else if currentChannels.isEmpty {
                            Spacer()
                            let emptyTitle: String = {
                                if !model.searchQuery.isEmpty {
                                    return String(localized: "No Results")
                                }
                                if model.selectedGenre != .all {
                                    return String(localized: "No \(String(localized: model.selectedGenre.localizedTitle)) broadcasts")
                                }
                                return String(localized: "No Channels")
                            }()
                            ContentUnavailableView(
                                emptyTitle,
                                systemImage: model.selectedGenre != .all ? model.selectedGenre.icon : "tv.slash",
                                description: Text(model.searchQuery.isEmpty ? String(localized: "No broadcasts found for the selected filter.") : String(localized: "No channel matches your search."))
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
                                                            recordConfirmationMessage = String(localized: "“\(spotlight.entry.title)” scheduled")
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
                                        Text(model.selectedBouquet?.displayName ?? String(localized: "All Channels"))
                                            .font(.headline.weight(.bold))
                                            .foregroundStyle(Theme.Colors.textPrimary)

                                        Spacer()

                                        Text(String(localized: "\(currentChannels.count) channels"))
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
                                                                recordConfirmationMessage = String(localized: "“\(entry.title)” scheduled")
                                                            }
                                                        }
                                                    }
                                                }
                                            )
                                            .contextMenu {
                                                Button {
                                                    model.playingChannel = channel
                                                } label: {
                                                    Label("Watch Live", systemImage: "play.fill")
                                                }

                                                if let now = model.schedule[channel.serviceRef]?.now {
                                                    Button {
                                                        selectedDetail = ProgramDetailPayload(channel: channel, entry: now)
                                                    } label: {
                                                        Label("Programme Details", systemImage: "info.circle")
                                                    }

                                                    Button {
                                                        Task { _ = await model.scheduleProgramTimer(channel: channel, entry: now) }
                                                    } label: {
                                                        Label(String(localized: "Record “\(now.title)”"), systemImage: "record.circle")
                                                    }
                                                }

                                                if let next = model.schedule[channel.serviceRef]?.next {
                                                    Button {
                                                        Task { _ = await model.scheduleProgramTimer(channel: channel, entry: next) }
                                                    } label: {
                                                        Label(String(localized: "Record “\(next.title)”"), systemImage: "record.circle")
                                                    }
                                                }

                                                Button {
                                                    model.toggleFavorite(channel)
                                                } label: {
                                                    Label(
                                                        model.isFavorite(channel) ? String(localized: "Remove from Favorites") : String(localized: "Add to Favorites"),
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
            .navigationTitle(model.selectedBouquet?.displayName ?? String(localized: "All Channels"))
            #if !os(tvOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Menu {
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(nil) }
                        } label: {
                            HStack {
                                Text("\(String(localized: "All Channels")) (\(model.channels.count))")
                                if model.selectedBouquet == nil {
                                    Image(systemName: "checkmark")
                                }
                            }
                        }

                        if !model.favoriteChannelIDs.isEmpty {
                            Button {
                                triggerHaptic(.light)
                                Task { await model.selectBouquet(AppModel.favoritesBouquet) }
                            } label: {
                                HStack {
                                    Text("\(String(localized: "Favorites")) (\(model.favoriteChannelIDs.count))")
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
                                        Text("\(bouquet.displayName)\(bouquet.servicesCount > 0 ? " (\(bouquet.servicesCount))" : "")")
                                        if model.selectedBouquet?.id == bouquet.id {
                                            Image(systemName: "checkmark")
                                        }
                                    }
                                }
                            }
                        }
                    } label: {
                        HStack(spacing: 5) {
                            Text(model.selectedBouquet?.displayName ?? String(localized: "All Channels"))
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
                                Label(String(localized: genre.localizedTitle), systemImage: genre.icon).tag(genre)
                            }
                        }
                    } label: {
                        Image(systemName: model.selectedGenre == .all ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                            .font(.system(size: 19))
                            .foregroundStyle(model.selectedGenre == .all ? Theme.Colors.textSecondary : Theme.Colors.accentLive)
                    }
                    .accessibilityLabel(String(localized: "Genre: \(String(localized: model.selectedGenre.localizedTitle))"))
                }
            }
            .searchable(text: $model.searchQuery, prompt: Text("Search channels or programmes…"))
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
                                    recordConfirmationMessage = String(localized: "“\(entry.title)” scheduled")
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
