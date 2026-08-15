// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Live TV station list with Safari WebUI visual fidelity, responsive multi-column iPadOS grid,
/// expandable upcoming EPG schedules, direct timer programming, and 1-tap playback.
struct ChannelListView: View {

    @Bindable var model: AppModel
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - Bouquet Filter Bar (Horizontal Scroll)
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
                            .padding(.vertical, 10)
                        }
                        .background(Theme.Colors.surfaceElevated.opacity(0.4))
                    }

                    // MARK: - Time Filter Bar (JETZT vs 20:15 HAUPTABEND)
                    HStack(spacing: 12) {
                        ForEach(AppModel.TimeFilter.allCases) { filter in
                            let isSelected = model.selectedTimeFilter == filter
                            Button {
                                triggerHaptic(.light)
                                model.selectedTimeFilter = filter
                            } label: {
                                HStack(spacing: 6) {
                                    Image(systemName: filter == .now ? "play.circle.fill" : "moon.stars.fill")
                                        .font(.caption)

                                    Text(filter.rawValue)
                                        .font(.subheadline.weight(isSelected ? .bold : .medium))
                                }
                                .padding(.horizontal, 16)
                                .padding(.vertical, 8)
                                .background(
                                    isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated,
                                    in: Capsule()
                                )
                                .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textSecondary)
                                .overlay(
                                    Capsule().strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                )
                            }
                            .buttonStyle(.plain)
                        }

                        Spacer()

                        // Channel Counter
                        Text("\(model.filteredChannels.count) Sender")
                            .font(.caption.monospacedDigit().weight(.medium))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)

                    // MARK: - Channel Grid Content
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
                                model.searchQuery.isEmpty ? "Keine Sender" : "Keine Treffer",
                                systemImage: "tv.slash",
                                description: Text(model.searchQuery.isEmpty ? (model.lastError ?? "Keine Sender für dieses Bouquet gefunden.") : "Kein Sender entspricht deiner Suche.")
                            )
                            .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else {
                            ScrollView {
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
                                                    Label("„\(next.title)“ programmieren", systemImage: "clock.badge.checkmark")
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
                                .padding(.horizontal, 16)
                                .padding(.vertical, 10)
                            }
                            .refreshable {
                                await model.loadChannels(bouquet: model.selectedBouquet?.name)
                            }
                        }
                    }
                }

                // MARK: - Floating Toast Notification
                if let message = recordConfirmationMessage {
                    VStack {
                        Spacer()
                        HStack(spacing: 8) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(Theme.Colors.statusSuccess)
                            Text(message)
                                .font(.subheadline.bold())
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Colors.borderElevated, lineWidth: 1))
                        .shadow(color: Color.black.opacity(0.35), radius: 10, y: 4)
                        .padding(.bottom, 24)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    .task {
                        try? await Task.sleep(for: .seconds(2.5))
                        withAnimation { recordConfirmationMessage = nil }
                    }
                }
            }
            .navigationTitle("Live TV")
            .searchable(text: $model.searchQuery, prompt: "Sender oder Sendung suchen…")
            .toolbar {
                if model.isLoadingChannels && !model.channels.isEmpty {
                    ProgressView()
                        .tint(Theme.Colors.accentAction)
                }
            }
        }
        .task {
            if model.channels.isEmpty {
                await model.loadInitialData()
            }
        }
        .fullScreenCover(item: $model.playingChannel) { channel in
            PlayerScreen(model: model, channel: channel)
        }
        .sheet(item: $selectedDetail) { detail in
            ProgramDetailSheet(model: model, payload: detail) {
                selectedDetail = nil
                model.playingChannel = detail.channel
            }
            .presentationDetents([.medium, .fraction(0.85)])
            .presentationDragIndicator(.visible)
        }
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        let generator = UIImpactFeedbackGenerator(style: style)
        generator.impactOccurred()
    }
}

// MARK: - Program Detail Payload

struct ProgramDetailPayload: Identifiable {
    var id: String { "\(channel.id)_\(entry.start.timeIntervalSince1970)" }
    let channel: Channel
    let entry: NowNext.Entry
}

// MARK: - Program Detail Sheet

struct ProgramDetailSheet: View {

    let model: AppModel
    let payload: ProgramDetailPayload
    let onPlay: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var isRecording = false
    @State private var recordSuccess: Bool?

    private var isLiveNow: Bool {
        let now = Date.now
        return now >= payload.entry.start && now <= payload.entry.end
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        // Channel & Broadcast Info Card
                        HStack(spacing: 12) {
                            ChannelLogo(url: payload.channel.logoURL, name: payload.channel.name, size: 52)

                            VStack(alignment: .leading, spacing: 4) {
                                Text(payload.channel.name)
                                    .font(.headline)
                                    .foregroundStyle(Theme.Colors.textPrimary)

                                Text(payload.entry.formattedTimeRange)
                                    .font(.subheadline.monospaced())
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }

                            Spacer()

                            if let remaining = payload.entry.remainingMinutes(at: .now) {
                                Text("noch \(remaining)m")
                                    .font(.caption.weight(.bold).monospacedDigit())
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 5)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                            }
                        }
                        .padding(14)
                        .glassCard(cornerRadius: 14)

                        // Title & Progress
                        VStack(alignment: .leading, spacing: 10) {
                            Text(payload.entry.title)
                                .font(.title2.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            if let progress = payload.entry.progress(at: .now) {
                                InfuseScrubber(
                                    progress: progress,
                                    startTime: payload.entry.formattedStartTime,
                                    endTime: payload.entry.formattedEndTime,
                                    remainingText: nil
                                )
                            }
                        }

                        // Description
                        if let description = payload.entry.description, !description.isEmpty {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("BESCHREIBUNG")
                                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)

                                Text(description)
                                    .font(.body)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineSpacing(4)
                            }
                            .padding(16)
                            .glassCard(cornerRadius: 14)
                        }

                        // Action Buttons
                        VStack(spacing: 12) {
                            Button {
                                dismiss()
                                onPlay()
                            } label: {
                                HStack {
                                    Image(systemName: "play.fill")
                                    Text(isLiveNow ? "Sender live ansehen" : "Sender starten")
                                        .font(.headline)
                                }
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 14)
                            }
                            .buttonStyle(.borderedProminent)
                            .tint(Theme.Colors.accentAction)

                            // Record Button
                            Button {
                                Task {
                                    isRecording = true
                                    let ok = await model.scheduleProgramTimer(channel: payload.channel, entry: payload.entry)
                                    recordSuccess = ok
                                    isRecording = false
                                }
                            } label: {
                                HStack {
                                    if isRecording {
                                        ProgressView()
                                            .tint(Theme.Colors.textPrimary)
                                    } else if recordSuccess == true {
                                        Image(systemName: "checkmark")
                                        Text("Aufnahme programmiert!")
                                    } else {
                                        Image(systemName: "record.circle")
                                        Text(isLiveNow ? "Jetzt live aufnehmen" : "Timer programmieren")
                                    }
                                }
                                .font(.headline)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 12)
                            }
                            .buttonStyle(.bordered)
                            .tint(recordSuccess == true ? Theme.Colors.statusSuccess : Theme.Colors.statusError)
                            .disabled(isRecording || recordSuccess == true)
                        }
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
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    @State private var isExpanded = false

    var body: some View {
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

                    if let now = nowNext?.now {
                        HStack(spacing: 6) {
                            PulsingLiveDot(size: 5)
                            Text(now.formattedTimeRange)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    }
                }

                Spacer(minLength: 4)

                // Remaining Time Pill
                if let now = nowNext?.now, let remaining = now.remainingMinutes(at: .now) {
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

            // MARK: - Current Program Details
            if let now = nowNext?.now {
                VStack(alignment: .leading, spacing: 8) {
                    Button {
                        onShowInfo(now)
                    } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(now.title)
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(2)
                                .multilineTextAlignment(.leading)

                            if let desc = now.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineLimit(2)
                                    .multilineTextAlignment(.leading)
                            }
                        }
                    }
                    .buttonStyle(.plain)

                    // Precision Infuse Live Scrubber
                    if let fraction = now.progress(at: .now) {
                        InfuseScrubber(
                            progress: fraction,
                            startTime: now.formattedStartTime,
                            endTime: now.formattedEndTime,
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

            // MARK: - Next Show Preview ("DANACH")
            if let next = nowNext?.next {
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

                    // Expand / Collapse Chevron Button (Safari WebUI Parity)
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
            }

            // MARK: - Expandable Full Upcoming EPG Schedule (Safari WebUI Parity)
            if isExpanded {
                VStack(spacing: 10) {
                    Divider()
                        .background(Theme.Colors.borderSubtle)

                    HStack {
                        Label("WEITERER TAGESABLAUF", systemImage: "clock.arrow.circlepath")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                        Spacer()
                    }
                    .padding(.top, 2)

                    let upcomingShows = fullSchedule.filter { $0.start >= (nowNext?.now?.end ?? .now) }
                    if upcomingShows.isEmpty {
                        Text("Keine weiteren Sendungen im EPG-Puffer.")
                            .font(.caption)
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .padding(.vertical, 4)
                    } else {
                        ForEach(upcomingShows.prefix(6)) { show in
                            HStack(spacing: 10) {
                                Text(show.formattedStartTime)
                                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .frame(width: 44, alignment: .leading)

                                VStack(alignment: .leading, spacing: 2) {
                                    Text(show.title)
                                        .font(.subheadline.weight(.semibold))
                                        .foregroundStyle(Theme.Colors.textPrimary)
                                        .lineLimit(1)

                                    if let desc = show.description, !desc.isEmpty {
                                        Text(desc)
                                            .font(.caption2)
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                            .lineLimit(1)
                                    }
                                }

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
                            .padding(.vertical, 4)
                        }
                    }
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(16)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
    }
}

// MARK: - Channel Logo (High-Res with Smart Fallback)

struct ChannelLogo: View {

    let url: URL?
    let name: String
    var size: CGFloat = 52

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 12)
                .fill(Theme.Colors.surfaceElevated)
                .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))

            if let url {
                AsyncImage(url: url) { phase in
                    switch phase {
                    case .empty:
                        ProgressView()
                            .scaleEffect(0.6)
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFit()
                            .padding(5)
                    case .failure:
                        fallbackText
                    @unknown default:
                        fallbackText
                    }
                }
            } else {
                fallbackText
            }
        }
        .frame(width: size, height: size)
        .shadow(color: Color.black.opacity(0.2), radius: 3, y: 1)
    }

    private var fallbackText: some View {
        Text(initials(from: name))
            .font(.system(size: size * 0.35, weight: .bold))
            .foregroundStyle(Theme.Colors.accentAction)
    }

    private func initials(from name: String) -> String {
        let cleaned = name.replacingOccurrences(of: " HD", with: "")
            .replacingOccurrences(of: " UHD", with: "")
            .trimmingCharacters(in: .whitespaces)
        let words = cleaned.split(separator: " ")
        if words.count >= 2 {
            return String(words[0].prefix(1) + words[1].prefix(1)).uppercased()
        }
        return String(cleaned.prefix(2)).uppercased()
    }
}
