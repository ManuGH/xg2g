// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Multi-day TV guide.
///
/// Three modes over one projection, because a guide gets asked three different
/// questions: what is on *now* (`onAir`), what is on *next* (`timeline`), and
/// what does the evening *look like* (`grid`). The previous single view answered
/// none of them well — it listed every programme of a channel in one unbounded
/// card, so reading across channels at a given time was impossible.
///
/// Clock time is the organising principle throughout: monospaced digits in a
/// fixed leading column in the list modes, a ruler and a live now-line in the
/// grid. Broadcast amber marks on-air state and nothing else.
struct GuideView: View {

    @Bindable var model: AppModel
    @Environment(\.horizontalSizeClass) private var sizeClass

    @AppStorage("guideDisplayMode") private var storedMode: String = GuideMode.grid.rawValue
    @AppStorage("guideShowSpotlight") private var showSpotlight: Bool = true
    @State private var selectedDayOffset: Int = 0
    @State private var anchor: GuideAnchor = .now
    @State private var selectedGenre: EpgGenre = .all
    @State private var guideSearchText = ""
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    private var currentMode: GuideMode {
        if storedMode == "Zeitschiene" {
            return .channels
        }
        return GuideMode(rawValue: storedMode) ?? .grid
    }

    private var modeBinding: Binding<GuideMode> {
        Binding(
            get: { currentMode },
            set: { storedMode = $0.rawValue }
        )
    }

    /// Built by `rebuildProjection`, never by `body`.
    @State private var projection: GuideProjection = .empty
    @State private var hasBuiltProjection = false

    private var contentBottomPadding: CGFloat {
        let isPad = UIDevice.current.userInterfaceIdiom == .pad
        return model.playbackManager.presentationMode == .miniplayer
            ? (isPad ? 96 : 140)
            : (isPad ? 24 : 80)
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    windowBar
                    content
                }

                if let message = recordConfirmationMessage {
                    confirmationToast(message)
                }
            }
            .task(id: inputs) {
                await rebuildProjection()
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { bouquetMenu }
                ToolbarItem(placement: .principal) { modePicker }
                ToolbarItem(placement: .topBarTrailing) {
                    HStack(spacing: 12) {
                        if currentMode == .grid {
                            spotlightToggle
                        }
                        genreMenu
                    }
                }
            }
            .searchable(text: $guideSearchText, prompt: "Sendung oder Sender suchen…")
            .sheet(item: $selectedDetail) { payload in
                ProgramDetailSheet(
                    channel: payload.channel,
                    entry: payload.entry,
                    channelSchedule: model.channelSchedule(for: payload.channel),
                    model: model,
                    onRecord: { entry in record(entry, on: payload.channel) }
                )
            }
            .task {
                if model.fullEpg.isEmpty {
                    await model.loadChannels()
                } else if let last = model.lastDataRefreshTime,
                          Date().timeIntervalSince(last) > 300 {
                    await model.refreshLiveContent()
                }
            }
        }
    }

    // MARK: - Chrome

    /// The mode switch stands in for the screen title. It is the first decision
    /// a reader makes here, so it takes the most prominent slot rather than
    /// sitting in a third row of chips.
    private var modePicker: some View {
        Picker("Darstellung", selection: modeBinding.animation(.easeInOut(duration: 0.2))) {
            ForEach(GuideMode.allCases) { mode in
                Label(mode.rawValue, systemImage: mode.symbol).tag(mode)
            }
        }
        .pickerStyle(.segmented)
        .frame(maxWidth: 240)
        .onChange(of: storedMode) { _, _ in
            triggerHaptic(.light)
        }
    }

    /// Day and start time in a single row: the day as a menu, the anchors as
    /// chips. Both used to be full-width horizontal scrollers stacked on top of
    /// each other, which cost two rows of height and hid the genre filter
    /// off-screen behind a divider.
    private var windowBar: some View {
        HStack(spacing: 10) {
            Menu {
                ForEach(0..<7, id: \.self) { offset in
                    Button {
                        triggerHaptic(.light)
                        selectedDayOffset = offset
                    } label: {
                        HStack {
                            Text(dayTitle(for: offset))
                            Text(dayDateString(for: offset))
                            if selectedDayOffset == offset {
                                Image(systemName: "checkmark")
                            }
                        }
                    }
                }
            } label: {
                HStack(spacing: 5) {
                    Text(dayTitle(for: selectedDayOffset))
                        .font(.system(size: 13, weight: .bold))
                    Image(systemName: "chevron.down")
                        .font(.system(size: 10, weight: .bold))
                }
                .foregroundStyle(Theme.Colors.textPrimary)
                .padding(.horizontal, 11)
                .padding(.vertical, 6)
                .background(Theme.Colors.surfaceElevated, in: Capsule())
                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
            }

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(availableAnchors) { option in
                        let isSelected = anchor == option
                        Button {
                            triggerHaptic(.light)
                            withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                anchor = option
                            }
                        } label: {
                            Text(option.rawValue)
                                .font(.system(size: 12, weight: isSelected ? .bold : .medium, design: .monospaced))
                                .padding(.horizontal, 11)
                                .padding(.vertical, 6)
                                .background(
                                    isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated.opacity(0.6),
                                    in: Capsule()
                                )
                                .foregroundStyle(isSelected ? .white : Theme.Colors.textSecondary)
                                .contentShape(Capsule())
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.trailing, 16)
            }
            .fadingHorizontalEdges(fadeWidth: 12)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(Theme.Colors.bgBase)
    }

    private var bouquetMenu: some View {
        Menu {
            Button {
                triggerHaptic(.light)
                Task { await model.selectBouquet(nil) }
            } label: {
                HStack {
                    Text("Alle Sender")
                    if model.selectedBouquet == nil { Image(systemName: "checkmark") }
                }
            }

            if !model.favoriteChannelIDs.isEmpty {
                Button {
                    triggerHaptic(.light)
                    Task {
                        await model.selectBouquet(
                            ChannelBouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")
                        )
                    }
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
            Image(systemName: "tv.badge.wifi")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Theme.Colors.accentAction)
        }
        .accessibilityLabel("Senderliste: \(model.selectedBouquet?.name ?? "Alle Sender")")
    }

    /// Genres moved out of the chip row. They refine a result set rather than
    /// define it, and eight of them in a shared horizontal scroller meant most
    /// were never seen.
    private var genreMenu: some View {
        Menu {
            ForEach(EpgGenre.allCases) { genre in
                Button {
                    triggerHaptic(.light)
                    selectedGenre = genre
                } label: {
                    HStack {
                        Label(genre.rawValue, systemImage: genre.icon)
                        if selectedGenre == genre { Image(systemName: "checkmark") }
                    }
                }
            }
        } label: {
            Image(systemName: selectedGenre == .all ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(selectedGenre == .all ? Theme.Colors.textSecondary : Theme.Colors.accentLive)
        }
        .accessibilityLabel("Genre: \(selectedGenre.rawValue)")
    }

    private var spotlightToggle: some View {
        Button {
            triggerHaptic(.light)
            withAnimation(.easeInOut(duration: 0.25)) {
                showSpotlight.toggle()
            }
        } label: {
            Image(systemName: showSpotlight ? "info.circle.fill" : "info.circle")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(showSpotlight ? Theme.Colors.accentLive : Theme.Colors.textSecondary)
        }
        .accessibilityLabel(showSpotlight ? "Vorschau-Banner ausblenden" : "Vorschau-Banner einblenden")
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        if !hasBuiltProjection {
            centeredState {
                ProgressView("Programm wird geladen…")
                    .tint(Theme.Colors.accentAction)
                    .foregroundStyle(Theme.Colors.textSecondary)
            }
        } else if projection.isEmpty {
            centeredState {
                ContentUnavailableView(
                    emptyTitle,
                    systemImage: "calendar.badge.exclamationmark",
                    description: Text(emptyDescription)
                )
                .foregroundStyle(Theme.Colors.textSecondary)
            }
        } else {
            // One clock for every live element on screen, ticking twice a minute.
            TimelineView(.periodic(from: .now, by: 30)) { context in
                switch currentMode {
                case .grid:
                    GuideGrid(
                        projection: projection,
                        now: context.date,
                        bottomPadding: contentBottomPadding,
                        onOpen: { entry in
                            selectedDetail = ProgramDetailPayload(channel: entry.channel, entry: entry.show)
                        },
                        onPlay: { channel in
                            triggerHaptic(.light)
                            model.playingChannel = channel
                        },
                        onRecord: { show, channel in
                            record(show, on: channel)
                        }
                    )
                case .channels:
                    channelCardList(now: context.date)
                }
            }
        }
    }

    private func channelCardList(now: Date) -> some View {
        ScrollView {
            LazyVStack(spacing: 12) {
                ForEach(projection.channels) { schedule in
                    InteractiveChannelCard(
                        channel: schedule.channel,
                        shows: schedule.shows,
                        now: now,
                        isFavorite: model.isFavorite(schedule.channel),
                        bouquetName: model.selectedBouquet?.name,
                        onPlay: {
                            triggerHaptic(.light)
                            model.playingChannel = schedule.channel
                        },
                        onToggleFavorite: {
                            model.toggleFavorite(schedule.channel)
                        },
                        onShowInfo: { show in
                            selectedDetail = ProgramDetailPayload(channel: schedule.channel, entry: show)
                        },
                        onRecord: { show in
                            record(show, on: schedule.channel)
                        }
                    )
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .safeAreaPadding(.bottom, contentBottomPadding)
        }
        .refreshable { await model.refreshLiveContent() }
    }

    private func centeredState<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack {
            Spacer()
            content()
            Spacer()
        }
    }

    private func confirmationToast(_ message: String) -> some View {
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

    // MARK: - Actions

    private func record(_ entry: NowNext.Entry, on channel: Channel) {
        Task {
            let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
            if ok {
                triggerHaptic(.medium)
                withAnimation {
                    recordConfirmationMessage = "„\(entry.title)“ programmiert"
                }
            }
        }
    }

    // MARK: - Projection

    private struct GuideInputs: Equatable {
        let dayOffset: Int
        let anchor: GuideAnchor
        let genre: EpgGenre
        let query: String
        let contentRevision: Int
        let modelGenre: EpgGenre
        let modelQuery: String
        let modelTimeFilter: AppModel.TimeFilter
        let modelBouquetID: String?
        let favoritesCount: Int
        /// Minute bucket, so a programme that has ended stops being "on air".
        /// Zero outside the modes that depend on the clock, so browsing a future
        /// day does not rebuild once a minute for nothing.
        let minuteBucket: Int
    }

    private var inputs: GuideInputs {
        let tracksClock = selectedDayOffset == 0 && anchor == .now
        return GuideInputs(
            dayOffset: selectedDayOffset,
            anchor: anchor,
            genre: selectedGenre,
            query: guideSearchText,
            contentRevision: model.contentRevision,
            modelGenre: model.selectedGenre,
            modelQuery: model.searchQuery,
            modelTimeFilter: model.selectedTimeFilter,
            modelBouquetID: model.selectedBouquet?.id,
            favoritesCount: model.favoriteChannelIDs.count,
            minuteBucket: tracksClock ? Int(Date.now.timeIntervalSince1970 / 60) : 0
        )
    }

    private func rebuildProjection() async {
        // Only typing needs the delay; a filter tap should feel immediate.
        if !guideSearchText.isEmpty {
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
        }

        let channels = model.filteredChannels
        let epg = model.fullEpg
        let dayOffset = selectedDayOffset
        let currentAnchor = anchor
        let genre = selectedGenre
        let query = guideSearchText
        let now = Date.now

        let built = await Task.detached(priority: .userInitiated) {
            GuideProjectionBuilder.build(
                channels: channels,
                epg: epg,
                dayOffset: dayOffset,
                anchor: currentAnchor,
                genre: genre,
                searchText: query,
                now: now
            )
        }.value

        guard !Task.isCancelled else { return }
        projection = built
        hasBuiltProjection = true
    }

    // MARK: - Helpers

    /// "Jetzt" only makes sense for today.
    private var availableAnchors: [GuideAnchor] {
        selectedDayOffset == 0
            ? GuideAnchor.allCases
            : GuideAnchor.allCases.filter { $0 != .now }
    }

    private var emptyTitle: String {
        if !guideSearchText.isEmpty { return "Keine Treffer für „\(guideSearchText)“" }
        if selectedGenre != .all { return "Nichts in \(selectedGenre.rawValue)" }
        return "Keine Sendungen im Zeitraum"
    }

    private var emptyDescription: String {
        if !guideSearchText.isEmpty { return "Andere Schreibweise oder anderer Tag." }
        if selectedGenre != .all { return "Genre zurücksetzen oder Zeitraum wechseln." }
        return "Wähle einen anderen Tag oder eine andere Startzeit."
    }

    private func dayTitle(for offset: Int) -> String {
        let date = Calendar.current.date(byAdding: .day, value: offset, to: Date()) ?? Date()
        if offset == 0 { return "Heute" }
        if offset == 1 { return "Morgen" }
        return Self.weekdayFormatter.string(from: date)
    }

    private func dayDateString(for offset: Int) -> String {
        let date = Calendar.current.date(byAdding: .day, value: offset, to: Date()) ?? Date()
        return Self.dayMonthFormatter.string(from: date)
    }

    private static let weekdayFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "de_DE")
        f.dateFormat = "EEEE"
        return f
    }()

    private static let dayMonthFormatter: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "de_DE")
        f.dateFormat = "d.M."
        return f
    }()
}

@MainActor
private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
    Haptics.shared.impact(style)
}
