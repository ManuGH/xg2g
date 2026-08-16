// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Dedicated Multi-Day TV Guide & Full EPG Program Schedule
///
/// Features:
/// - 7-Day Timeline Navigation (Heute, Morgen, Wochentage)
/// - Prime-Time & Time-Jump Shortcuts (Ganztägig, 20:15, 22:00)
/// - Comprehensive Genre & Bouquet Filtering
/// - Smart Rerun / Wiederholungen Detection across all channels
/// - 1-Tap Instant Timer Programming
struct GuideView: View {

    @Bindable var model: AppModel
    @Environment(\.horizontalSizeClass) private var sizeClass

    @State private var selectedDayOffset: Int = 0
    @State private var selectedTimeWindow: TimeWindow = .allDay
    @State private var selectedGenre: EpgGenre = .all
    @State private var guideSearchText = ""
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    enum TimeWindow: String, CaseIterable, Identifiable {
        case allDay = "Ganztägig"
        case now = "🔴 Jetzt"
        case primeTime = "20:15"
        case lateNight = "22:00"

        var id: String { rawValue }
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - 1. Top Days Navigation Bar
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(0..<7, id: \.self) { offset in
                                let isSelected = selectedDayOffset == offset
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        selectedDayOffset = offset
                                        if offset > 0 && selectedTimeWindow == .now {
                                            selectedTimeWindow = .allDay
                                        }
                                    }
                                } label: {
                                    VStack(spacing: 2) {
                                        Text(dayTitle(for: offset))
                                            .font(.system(size: 13, weight: isSelected ? .bold : .medium))
                                        Text(dayDateString(for: offset))
                                            .font(.system(size: 10, weight: .semibold, design: .monospaced))
                                            .opacity(0.8)
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 6)
                                    .background(
                                        isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85),
                                        in: RoundedRectangle(cornerRadius: 10, style: .continuous)
                                    )
                                    .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
                                    .overlay {
                                        if !isSelected {
                                            RoundedRectangle(cornerRadius: 10, style: .continuous)
                                                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                                        }
                                    }
                                    .contentShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    }
                    .background(Theme.Colors.surfaceElevated.opacity(0.25))

                    // MARK: - 2. Time & Genre Filter Row
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            // Time Window Jumps
                            ForEach(availableTimeWindows) { window in
                                let isSelected = selectedTimeWindow == window
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        selectedTimeWindow = window
                                    }
                                } label: {
                                    Text(window.rawValue)
                                        .font(.system(size: 12, weight: isSelected ? .bold : .medium))
                                        .padding(.horizontal, 11)
                                        .padding(.vertical, 5)
                                        .background(isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated.opacity(0.6), in: Capsule())
                                        .foregroundStyle(isSelected ? .white : Theme.Colors.textSecondary)
                                        .contentShape(Capsule())
                                }
                                .buttonStyle(.plain)
                            }

                            Divider()
                                .frame(height: 16)
                                .background(Theme.Colors.borderSubtle)

                            // Genre Filter Badges
                            ForEach(EpgGenre.allCases) { genre in
                                let isSelected = selectedGenre == genre
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        selectedGenre = genre
                                    }
                                } label: {
                                    HStack(spacing: 4) {
                                        if genre != .all {
                                            Image(systemName: genre.icon)
                                                .font(.system(size: 10))
                                        }
                                        Text(genre.rawValue)
                                            .font(.system(size: 12, weight: isSelected ? .bold : .medium))
                                    }
                                    .padding(.horizontal, 11)
                                    .padding(.vertical, 5)
                                    .background(isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.6), in: Capsule())
                                    .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textSecondary)
                                    .contentShape(Capsule())
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 6)
                    }
                    .background(Theme.Colors.bgBase)

                    // MARK: - 3. Guide Schedule List
                    let channelSchedules = computedChannelSchedules

                    if channelSchedules.isEmpty {
                        Spacer()
                        ContentUnavailableView(
                            guideSearchText.isEmpty ? "Keine Sendungen gefunden" : "Keine Treffer für „\(guideSearchText)“",
                            systemImage: "calendar.badge.exclamationmark",
                            description: Text("Passe deinen Datums-, Zeit- oder Genre-Filter an.")
                        )
                        .foregroundStyle(Theme.Colors.textSecondary)
                        Spacer()
                    } else {
                        let isRegular = sizeClass == .regular
                        ScrollView {
                            LazyVGrid(
                                columns: [
                                    GridItem(.adaptive(minimum: isRegular ? 360 : 300, maximum: 540), spacing: isRegular ? 16 : 12)
                                ],
                                spacing: isRegular ? 16 : 12
                            ) {
                                ForEach(channelSchedules, id: \.channel.id) { item in
                                    GuideChannelCard(
                                        channel: item.channel,
                                        shows: item.shows,
                                        model: model,
                                        onPlay: {
                                            model.playingChannel = item.channel
                                        },
                                        onShowInfo: { show in
                                            selectedDetail = ProgramDetailPayload(channel: item.channel, entry: show)
                                        },
                                        onRecord: { show in
                                            Task {
                                                let ok = await model.scheduleProgramTimer(channel: item.channel, entry: show)
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
                            .padding(.horizontal, isRegular ? 20 : 12)
                            .padding(.vertical, isRegular ? 16 : 12)
                            .safeAreaPadding(.bottom, 80)
                        }
                        .refreshable {
                            await model.loadChannels()
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
                            withAnimation {
                                recordConfirmationMessage = nil
                            }
                        }
                    }
                }
            }
            .navigationTitle("TV-Guide")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Menu {
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(nil) }
                        } label: {
                            HStack {
                                Text("Alle Sender")
                                if model.selectedBouquet == nil {
                                    Image(systemName: "checkmark")
                                }
                            }
                        }

                        if !model.favoriteChannelIDs.isEmpty {
                            Button {
                                triggerHaptic(.light)
                                Task { await model.selectBouquet(Bouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
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
                                .font(.subheadline.weight(.bold))
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
            }
            .searchable(text: $guideSearchText, prompt: "Sendung, Film, Schauspieler suchen…")
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
            .task {
                if model.fullEpg.isEmpty {
                    await model.loadChannels()
                }
            }
        }
    }

    // MARK: - Computed Schedules & Filtering

    private struct ChannelScheduleItem {
        let channel: Channel
        let shows: [NowNext.Entry]
    }

    private var computedChannelSchedules: [ChannelScheduleItem] {
        let calendar = Calendar.current
        let today = calendar.startOfDay(for: .now)
        guard let targetDayStart = calendar.date(byAdding: .day, value: selectedDayOffset, to: today),
              let targetDayEnd = calendar.date(byAdding: .day, value: 1, to: targetDayStart) else {
            return []
        }

        let query = guideSearchText.trimmingCharacters(in: .whitespaces).lowercased()

        var items: [ChannelScheduleItem] = []

        for channel in model.filteredChannels {
            var shows = model.fullEpg[channel.serviceRef] ?? []

            // Filter for the target day
            shows = shows.filter { show in
                show.start < targetDayEnd && show.end > targetDayStart
            }

            // Filter for Time Window
            switch selectedTimeWindow {
            case .allDay:
                break
            case .now:
                shows = shows.filter { $0.start <= .now && $0.end > .now }
            case .primeTime:
                var components = calendar.dateComponents([.year, .month, .day], from: targetDayStart)
                components.hour = 20
                components.minute = 15
                if let primeDate = calendar.date(from: components) {
                    shows = shows.filter { $0.start <= primeDate && $0.end > primeDate }
                }
            case .lateNight:
                var components = calendar.dateComponents([.year, .month, .day], from: targetDayStart)
                components.hour = 22
                components.minute = 0
                if let lateDate = calendar.date(from: components) {
                    shows = shows.filter { $0.start <= lateDate && $0.end > lateDate }
                }
            }

            // Filter for Genre
            if selectedGenre != .all {
                shows = shows.filter { show in
                    show.matches(genre: selectedGenre, channelName: channel.name)
                }
            }

            // Filter for Search Query
            if !query.isEmpty {
                shows = shows.filter { show in
                    show.title.lowercased().contains(query) ||
                    (show.description?.lowercased().contains(query) ?? false) ||
                    channel.name.lowercased().contains(query)
                }
            }

            if !shows.isEmpty {
                items.append(ChannelScheduleItem(channel: channel, shows: shows))
            }
        }

        return items
    }

    // MARK: - Helpers

    private var availableTimeWindows: [TimeWindow] {
        if selectedDayOffset == 0 {
            return [.allDay, .now, .primeTime, .lateNight]
        } else {
            return [.allDay, .primeTime, .lateNight]
        }
    }

    private func dayTitle(for offset: Int) -> String {
        if offset == 0 { return "Heute" }
        if offset == 1 { return "Morgen" }
        let target = Calendar.current.date(byAdding: .day, value: offset, to: .now) ?? .now
        let f = DateFormatter()
        f.locale = Locale(identifier: "de_DE")
        f.dateFormat = "EEEE"
        return f.string(from: target)
    }

    private func dayDateString(for offset: Int) -> String {
        let target = Calendar.current.date(byAdding: .day, value: offset, to: .now) ?? .now
        let f = DateFormatter()
        f.locale = Locale(identifier: "de_DE")
        f.dateFormat = "d. MMM"
        return f.string(from: target)
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        UIImpactFeedbackGenerator(style: style).impactOccurred()
    }
}

// MARK: - Guide Channel Card

struct GuideChannelCard: View {

    let channel: Channel
    let shows: [NowNext.Entry]
    let model: AppModel
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // Channel Header Bar
            HStack(spacing: 12) {
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
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                    }

                    Text("\(shows.count) Sendungen gelistet")
                        .font(.system(size: 11, weight: .medium, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textTertiary)
                }

                Spacer()

                // 1-Tap Play Button
                Button(action: onPlay) {
                    Image(systemName: "play.circle.fill")
                        .font(.system(size: 28))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                .buttonStyle(.plain)
            }

            Divider()
                .background(Theme.Colors.borderSubtle)

            // Timeline Show Rows
            VStack(spacing: 8) {
                ForEach(shows) { show in
                    HStack(spacing: 12) {
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

                                    let g = show.genre(channelName: channel.name)
                                    if g != .all {
                                        Text(g.rawValue)
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

                        // 1-Click Timer Button
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
                    .padding(.vertical, 2)
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
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1)
        )
    }
}
