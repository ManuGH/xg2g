import SwiftUI
import UIKit

@MainActor
private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
    UIImpactFeedbackGenerator(style: style).impactOccurred()
}

/// Live TV station list & TV Pro inspired EPG with Quick Time-Jumps (Jetzt, 20:15, 22:00, Tage-Picker),
/// Genre filtering (Spielfilme, Serien, Sport, Doku...), View Switcher (Liste vs Magazin),
/// expandable multi-day schedules, direct 1-tap timer programming, and instant playback.
struct ChannelListView: View {

    @Bindable var model: AppModel
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - 1. Bouquet Filter Bar (Horizontal Scroll)
                    if !model.bouquets.isEmpty {
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 8) {
                                // "Alle Sender" Pill
                                Button {
                                    triggerHaptic(.light)
                                    Task { await model.selectBouquet(nil) }
                                } label: {
                                    HStack(spacing: 6) {
                                        Image(systemName: "tv")
                                            .font(.caption2)
                                        Text("Alle Sender")
                                            .font(.subheadline.weight(model.selectedBouquet == nil && model.favoriteChannelIDs.isEmpty ? .semibold : .regular))
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 7)
                                    .background(
                                        model.selectedBouquet == nil && model.favoriteChannelIDs.isEmpty ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                                        in: Capsule()
                                    )
                                    .foregroundStyle(model.selectedBouquet == nil && model.favoriteChannelIDs.isEmpty ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                    .overlay(
                                        Capsule().strokeBorder(model.selectedBouquet == nil && model.favoriteChannelIDs.isEmpty ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                    )
                                }
                                .buttonStyle(.plain)

                                // "Favoriten" Pill
                                if !model.favoriteChannelIDs.isEmpty {
                                    Button {
                                        triggerHaptic(.light)
                                        Task { await model.selectBouquet(Bouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
                                    } label: {
                                        HStack(spacing: 6) {
                                            Image(systemName: "star.fill")
                                                .font(.caption2)
                                                .foregroundStyle(Theme.Colors.accentLive)
                                            Text("Favoriten")
                                                .font(.subheadline.weight(model.selectedBouquet?.id == AppModel.favoritesBouquetID ? .semibold : .regular))
                                            Text("\(model.favoriteChannelIDs.count)")
                                                .font(.caption2.monospacedDigit())
                                                .foregroundStyle(Theme.Colors.textTertiary)
                                        }
                                        .padding(.horizontal, 14)
                                        .padding(.vertical, 7)
                                        .background(
                                            model.selectedBouquet?.id == AppModel.favoritesBouquetID ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                                            in: Capsule()
                                        )
                                        .foregroundStyle(model.selectedBouquet?.id == AppModel.favoritesBouquetID ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                        .overlay(
                                            Capsule().strokeBorder(model.selectedBouquet?.id == AppModel.favoritesBouquetID ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                        )
                                    }
                                    .buttonStyle(.plain)
                                }

                                // Server Bouquet Pills
                                ForEach(model.bouquets) { bouquet in
                                    let isSelected = model.selectedBouquet?.id == bouquet.id
                                    Button {
                                        triggerHaptic(.light)
                                        Task { await model.selectBouquet(bouquet) }
                                    } label: {
                                        HStack(spacing: 6) {
                                            Text(bouquet.name)
                                                .font(.subheadline.weight(isSelected ? .semibold : .regular))

                                            if bouquet.servicesCount > 0 {
                                                Text("\(bouquet.servicesCount)")
                                                    .font(.caption2.monospacedDigit())
                                                    .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textTertiary)
                                            }
                                        }
                                        .padding(.horizontal, 14)
                                        .padding(.vertical, 7)
                                        .background(
                                            isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                                            in: Capsule()
                                        )
                                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                        .overlay(
                                            Capsule().strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                        )
                                    }
                                    .buttonStyle(.plain)
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                        }
                        .background(Theme.Colors.surfaceElevated.opacity(0.35))
                    }

                    // MARK: - 2. TV Pro Quick Time-Jumps & Day-Picker Bar
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(model.availableTimeFilters) { filter in
                                let isSelected = model.selectedTimeFilter == filter
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                                        model.selectedTimeFilter = filter
                                    }
                                } label: {
                                    HStack(spacing: 6) {
                                        Image(systemName: filter.icon)
                                            .font(.caption2)
                                            .foregroundStyle(isSelected ? Theme.Colors.bgBase : (filter == .now ? Theme.Colors.accentLive : Theme.Colors.accentAction))

                                        Text(filter.label)
                                            .font(.system(size: 13, weight: isSelected ? .bold : .medium))
                                    }
                                    .padding(.horizontal, 13)
                                    .padding(.vertical, 6)
                                    .background(
                                        isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated,
                                        in: Capsule()
                                    )
                                    .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
                                    .overlay(
                                        Capsule().strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                    )
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 6)
                    }
                    .background(Theme.Colors.bgBase.opacity(0.8))

                    // MARK: - 3. Genre Selector & View Mode Switcher
                    HStack(spacing: 10) {
                        // Genre horizontal scroll
                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 6) {
                                ForEach(EpgGenre.allCases) { genre in
                                    let isSelected = model.selectedGenre == genre
                                    Button {
                                        triggerHaptic(.light)
                                        withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                            model.selectedGenre = genre
                                        }
                                    } label: {
                                        HStack(spacing: 4) {
                                            Image(systemName: genre.icon)
                                                .font(.system(size: 10))
                                            Text(genre.rawValue)
                                                .font(.system(size: 11, weight: isSelected ? .bold : .regular))
                                        }
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 5)
                                        .background(
                                            isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                                            in: Capsule()
                                        )
                                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textTertiary)
                                        .overlay(
                                            Capsule().strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle.opacity(0.6), lineWidth: 1)
                                        )
                                    }
                                    .buttonStyle(.plain)
                                }
                            }
                            .padding(.horizontal, 16)
                        }

                        // View Mode Switcher (Liste vs Magazin)
                        HStack(spacing: 2) {
                            ForEach(EpgViewMode.allCases) { mode in
                                let isSelected = model.epgViewMode == mode
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        model.epgViewMode = mode
                                    }
                                } label: {
                                    Image(systemName: mode.icon)
                                        .font(.system(size: 12, weight: isSelected ? .bold : .regular))
                                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textTertiary)
                                        .padding(.horizontal, 8)
                                        .padding(.vertical, 5)
                                        .background(isSelected ? Theme.Colors.surfaceElevated : Color.clear, in: RoundedRectangle(cornerRadius: 6))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(2)
                        .background(Theme.Colors.surfaceGlass, in: RoundedRectangle(cornerRadius: 8))
                        .overlay(
                            RoundedRectangle(cornerRadius: 8).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
                        )
                        .padding(.trailing, 16)
                    }
                    .padding(.vertical, 4)

                    // MARK: - 4. Main EPG Grid Content
                    Group {
                        if model.channels.isEmpty && model.isLoadingChannels {
                            Spacer()
                            ProgressView("Lade Sender und EPG-Daten…")
                                .tint(Theme.Colors.accentAction)
                                .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else if model.filteredChannels.isEmpty {
                            Spacer()
                            ContentUnavailableView(
                                model.searchQuery.isEmpty ? (model.selectedGenre != .all ? "Keine \(model.selectedGenre.rawValue)" : "Keine Sender") : "Keine Treffer",
                                systemImage: model.selectedGenre != .all ? model.selectedGenre.icon : "tv.slash",
                                description: Text(model.searchQuery.isEmpty ? "Keine Sendungen für den gewählten Filter gefunden." : "Kein Sender entspricht deiner Suche.")
                            )
                            .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else {
                            ScrollView {
                                if model.epgViewMode == .magazine {
                                    // MARK: - TV Pro Magazine Cards Grid
                                    LazyVGrid(
                                        columns: [
                                            GridItem(.adaptive(minimum: 320, maximum: 540), spacing: 14)
                                        ],
                                        spacing: 14
                                    ) {
                                        ForEach(model.filteredChannels) { channel in
                                            MagazineChannelCard(
                                                channel: channel,
                                                targetShow: model.show(for: channel, at: model.selectedTimeFilter),
                                                timeFilter: model.selectedTimeFilter,
                                                isFavorite: model.isFavorite(channel),
                                                onPlay: { model.playingChannel = channel },
                                                onShowInfo: { show in
                                                    selectedDetail = ProgramDetailPayload(channel: channel, entry: show)
                                                },
                                                onRecord: { show in
                                                    Task {
                                                        let ok = await model.scheduleProgramTimer(channel: channel, entry: show)
                                                        if ok {
                                                            triggerHaptic(.medium)
                                                            withAnimation {
                                                                recordConfirmationMessage = "„\(show.title)“ programmiert"
                                                            }
                                                        }
                                                    }
                                                }
                                            )
                                        }
                                    }
                                    .padding(14)
                                } else {
                                    // MARK: - Standard Interactive List Rows
                                    LazyVGrid(
                                        columns: [
                                            GridItem(.adaptive(minimum: 340, maximum: 540), spacing: 14)
                                        ],
                                        spacing: 14
                                    ) {
                                        ForEach(model.filteredChannels) { channel in
                                            ChannelRow(
                                                channel: channel,
                                                nowNext: model.schedule[channel.serviceRef],
                                                fullSchedule: model.fullEpg[channel.serviceRef] ?? [],
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
                                                if let now = model.schedule[channel.serviceRef]?.now {
                                                    Button {
                                                        selectedDetail = ProgramDetailPayload(channel: channel, entry: now)
                                                    } label: {
                                                        Label("Sendungsdetails ansehen", systemImage: "info.circle")
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
                                    .padding(14)
                                }
                            }
                            .refreshable {
                                await model.loadChannels()
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
            .navigationTitle("Live TV")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $model.searchQuery, prompt: "Sender oder Sendungen suchen…")
            .sheet(item: $selectedDetail) { payload in
                ProgramDetailSheet(
                    channel: payload.channel,
                    entry: payload.entry,
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
            .fullScreenCover(item: $model.playingChannel) { channel in
                PlayerScreen(model: model, channel: channel)
            }
        }
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
                if let show = targetShow, show.genre != .all {
                    Text(show.genre.rawValue)
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
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
            RoundedRectangle(cornerRadius: 16)
                .fill(Theme.Colors.surfaceElevated.opacity(0.75))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16)
                .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
        )
    }
}

// MARK: - Program Detail Payload

struct ProgramDetailPayload: Identifiable {
    var id: String { "\(channel.id)_\(entry.id)" }
    let channel: Channel
    let entry: NowNext.Entry
}

// MARK: - Program Detail Sheet

struct ProgramDetailSheet: View {

    let channel: Channel
    let entry: NowNext.Entry
    var onRecord: (NowNext.Entry) -> Void = { _ in }
    @Environment(\.dismiss) private var dismiss

    @State private var isRecording = false
    @State private var recordSuccess: Bool?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        // Header: Channel Logo + Channel Name + Time
                        HStack(spacing: 14) {
                            ChannelLogo(url: channel.logoURL, name: channel.name, size: 56)

                            VStack(alignment: .leading, spacing: 4) {
                                Text(channel.name)
                                    .font(.headline)
                                    .foregroundStyle(Theme.Colors.textPrimary)

                                Text(entry.formattedDayHeader)
                                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)

                                Text("\(entry.formattedTimeRange) (\(entry.durationMinutes) Min)")
                                    .font(.subheadline.monospacedDigit())
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }

                            Spacer()
                        }
                        .padding(.bottom, 4)

                        // Genre Tag
                        if entry.genre != .all {
                            HStack {
                                Label(entry.genre.rawValue, systemImage: entry.genre.icon)
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 4)
                                    .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())
                                Spacer()
                            }
                        }

                        // Show Title
                        Text(entry.title)
                            .font(.title2.weight(.bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        // Live Scrubber (if currently on air)
                        if let fraction = entry.progress(at: .now) {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("LÄUFT JETZT LIVE")
                                    .font(.caption.weight(.bold).monospaced())
                                    .foregroundStyle(Theme.Colors.accentLive)

                                InfuseScrubber(
                                    progress: fraction,
                                    startTime: entry.formattedStartTime,
                                    endTime: entry.formattedEndTime,
                                    remainingText: entry.remainingMinutes(at: .now).map { "noch \($0) Min" }
                                )
                            }
                            .padding(14)
                            .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 12))
                        }

                        // Full Description / Synopsis
                        VStack(alignment: .leading, spacing: 8) {
                            Text("INHALTSANGABE")
                                .font(.caption.weight(.bold).monospaced())
                                .foregroundStyle(Theme.Colors.textTertiary)

                            if let desc = entry.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.body)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineSpacing(4)
                            } else {
                                Text("Keine ausführliche Beschreibung für diese Sendung vorhanden.")
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }

                        Spacer(minLength: 20)

                        // 1-Click Timer Button
                        Button {
                            Task {
                                isRecording = true
                                onRecord(entry)
                                try? await Task.sleep(for: .milliseconds(500))
                                isRecording = false
                                recordSuccess = true
                            }
                        } label: {
                            HStack {
                                Spacer()
                                if isRecording {
                                    ProgressView()
                                        .tint(.white)
                                } else if recordSuccess == true {
                                    Image(systemName: "checkmark")
                                    Text("Timer programmiert")
                                        .font(.headline)
                                } else {
                                    Image(systemName: "record.circle")
                                    Text("Timer aufnehmen")
                                        .font(.headline)
                                }
                                Spacer()
                            }
                            .padding(.vertical, 14)
                            .background(recordSuccess == true ? Theme.Colors.statusSuccess : Theme.Colors.statusError, in: RoundedRectangle(cornerRadius: 12))
                            .foregroundStyle(.white)
                        }
                        .disabled(isRecording || recordSuccess == true)
                    }
                    .padding(20)
                }
            }
            .navigationTitle("Sendungsdetails")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Schließen") { dismiss() }
                        .foregroundStyle(Theme.Colors.accentAction)
                }
            }
        }
    }
}

// MARK: - Channel Row (Safari WebUI Aesthetics + Expandable Schedule)

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    let fullSchedule: [NowNext.Entry]
    var timeFilter: AppModel.TimeFilter = .now
    var targetShow: NowNext.Entry? = nil
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    @State private var isExpanded = false

    var body: some View {
        let displayedShow = targetShow ?? nowNext?.now

        VStack(alignment: .leading, spacing: 14) {
            // MARK: - Channel Header Bar
            HStack(spacing: 12) {
                // Channel Logo (1-Tap Play)
                Button(action: onPlay) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 52)
                }
                .buttonStyle(.plain)

                // Channel Name & Number Badge
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text(number)
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(channel.name)
                            .font(.system(size: 17, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if isFavorite {
                            Image(systemName: "star.fill")
                                .font(.caption)
                                .foregroundStyle(.yellow)
                        }
                    }

                    if let show = displayedShow {
                        HStack(spacing: 6) {
                            if timeFilter == .now {
                                PulsingLiveDot(size: 5)
                            }
                            Text(show.formattedTimeRange)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(timeFilter == .now ? Theme.Colors.accentLive : Theme.Colors.accentAction)
                        }
                    }
                }

                Spacer(minLength: 4)

                // Remaining Time Pill (only if running now)
                if timeFilter == .now, let now = nowNext?.now, let remaining = now.remainingMinutes(at: .now) {
                    Text("noch \(remaining)m")
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentLive)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                }

                // Play Button
                Button(action: onPlay) {
                    Image(systemName: "play.circle.fill")
                        .font(.system(size: 32))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                .buttonStyle(.plain)
            }

            // MARK: - Displayed Program Details
            if let show = displayedShow {
                VStack(alignment: .leading, spacing: 8) {
                    Button {
                        onShowInfo(show)
                    } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(show.title)
                                    .font(.system(size: 15, weight: .bold))
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                    .lineLimit(2)
                                    .multilineTextAlignment(.leading)

                                Spacer()

                                if show.genre != .all {
                                    Text(show.genre.rawValue)
                                        .font(.system(size: 10, weight: .semibold))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .padding(.horizontal, 7)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                                }
                            }

                            if let desc = show.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineLimit(2)
                                    .multilineTextAlignment(.leading)
                            }
                        }
                    }
                    .buttonStyle(.plain)

                    // Precision Infuse Live Scrubber (only for current live show)
                    if timeFilter == .now, let fraction = show.progress(at: .now) {
                        InfuseScrubber(
                            progress: fraction,
                            startTime: show.formattedStartTime,
                            endTime: show.formattedEndTime,
                            remainingText: nil
                        )
                        .padding(.top, 2)
                    }
                }
            } else {
                Text("Keine Programminformationen verfügbar")
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textTertiary)
            }

            // MARK: - Next Show Preview ("DANACH") & Expand Button
            if timeFilter == .now, let next = nowNext?.next {
                HStack(spacing: 8) {
                    Text("DANACH:")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(next.formattedStartTime)
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)

                    Text(next.title)
                        .font(.subheadline)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)

                    Spacer()

                    // Expand / Collapse Chevron Button
                    Button {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                            isExpanded.toggle()
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Text(isExpanded ? "Weniger" : "Programm")
                                .font(.system(size: 11, weight: .semibold))
                            Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                                .font(.system(size: 10, weight: .bold))
                        }
                        .foregroundStyle(Theme.Colors.accentAction)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())
                    }
                    .buttonStyle(.plain)
                }
                .padding(.top, 2)
            } else {
                HStack {
                    Spacer()
                    // Expand / Collapse Chevron Button for future time filters
                    Button {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                            isExpanded.toggle()
                        }
                    } label: {
                        HStack(spacing: 4) {
                            Text(isExpanded ? "Weniger" : "Tagesablauf")
                                .font(.system(size: 11, weight: .semibold))
                            Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                                .font(.system(size: 10, weight: .bold))
                        }
                        .foregroundStyle(Theme.Colors.accentAction)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())
                    }
                    .buttonStyle(.plain)
                }
                .padding(.top, 2)
            }

            // MARK: - Expandable Full Multi-Day EPG Schedule
            if isExpanded {
                VStack(spacing: 12) {
                    Divider()
                        .background(Theme.Colors.borderSubtle)

                    let upcomingShows = fullSchedule.filter { $0.start >= (nowNext?.now?.end ?? .now) }
                    if upcomingShows.isEmpty {
                        Text("Keine weiteren Sendungen im EPG-Puffer vorhanden.")
                            .font(.caption)
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .padding(.vertical, 8)
                    } else {
                        let grouped = Dictionary(grouping: upcomingShows, by: \.formattedDayHeader)
                        let sortedDayHeaders = upcomingShows.map(\.formattedDayHeader).uniqued()

                        ForEach(sortedDayHeaders, id: \.self) { dayHeader in
                            VStack(alignment: .leading, spacing: 8) {
                                HStack {
                                    Text(dayHeader)
                                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.accentLive)
                                        .padding(.horizontal, 8)
                                        .padding(.vertical, 3)
                                        .background(Theme.Colors.accentLive.opacity(0.12), in: RoundedRectangle(cornerRadius: 4))

                                    Spacer()
                                }
                                .padding(.top, 4)

                                if let showsForDay = grouped[dayHeader] {
                                    ForEach(showsForDay) { show in
                                        HStack(spacing: 10) {
                                            Text(show.formattedStartTime)
                                                .font(.system(size: 12, weight: .bold, design: .monospaced))
                                                .foregroundStyle(Theme.Colors.accentAction)
                                                .frame(width: 44, alignment: .leading)

                                            Button {
                                                onShowInfo(show)
                                            } label: {
                                                VStack(alignment: .leading, spacing: 2) {
                                                    HStack(spacing: 6) {
                                                        Text(show.title)
                                                            .font(.subheadline.weight(.semibold))
                                                            .foregroundStyle(Theme.Colors.textPrimary)
                                                            .lineLimit(1)

                                                        if show.genre != .all {
                                                            Text(show.genre.rawValue)
                                                                .font(.system(size: 9, weight: .semibold))
                                                                .foregroundStyle(Theme.Colors.textTertiary)
                                                        }
                                                    }

                                                    if let desc = show.description, !desc.isEmpty {
                                                        Text(desc)
                                                            .font(.caption2)
                                                            .foregroundStyle(Theme.Colors.textTertiary)
                                                            .lineLimit(1)
                                                    }
                                                }
                                            }
                                            .buttonStyle(.plain)

                                            Spacer()

                                            // 1-Click Timer Recording Button
                                            Button {
                                                onRecord(show)
                                            } label: {
                                                Image(systemName: "record.circle")
                                                    .font(.system(size: 20))
                                                    .foregroundStyle(Theme.Colors.statusError)
                                                    .padding(4)
                                            }
                                            .buttonStyle(.plain)
                                        }
                                        .padding(.vertical, 3)
                                    }
                                }
                            }
                        }
                    }
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 16)
                .fill(Theme.Colors.surfaceElevated.opacity(0.65))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16)
                .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
        )
    }
}

// MARK: - Channel Logo View

struct ChannelLogo: View {
    let url: URL?
    let name: String
    var size: CGFloat = 48

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.22)
                .fill(Theme.Colors.surfaceGlass)
                .frame(width: size, height: size)
                .overlay(
                    RoundedRectangle(cornerRadius: size * 0.22)
                        .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
                )

            if let url {
                AsyncImage(url: url) { phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFit()
                            .padding(size * 0.12)
                    case .failure, .empty:
                        fallbackText
                    @unknown default:
                        fallbackText
                    }
                }
                .frame(width: size, height: size)
            } else {
                fallbackText
            }
        }
        .frame(width: size, height: size)
    }

    private var fallbackText: some View {
        Text(String(name.prefix(2)).uppercased())
            .font(.system(size: size * 0.35, weight: .bold, design: .rounded))
            .foregroundStyle(Theme.Colors.textTertiary)
    }
}

