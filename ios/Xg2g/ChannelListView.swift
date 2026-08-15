// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct ChannelListView: View {

    @Bindable var model: AppModel

    @State private var selectedDetail: ProgramDetailPayload?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // Bouquet Picker Bar
                    if !model.bouquets.isEmpty {
                        BouquetPicker(model: model)
                            .padding(.vertical, 8)
                            .background(Theme.Colors.surfaceElevated.opacity(0.4))
                    }

                    // Time Window Filter Pills (Jetzt vs 20:15 / Primetime)
                    HStack(spacing: 8) {
                        ForEach(AppModel.TimeFilter.allCases) { filter in
                            Button {
                                triggerHaptic(.light)
                                model.selectedTimeFilter = filter
                            } label: {
                                HStack(spacing: 5) {
                                    if filter == .now {
                                        PulsingLiveDot(size: 4)
                                    } else {
                                        Image(systemName: "moon.stars.fill")
                                            .font(.caption2)
                                            .foregroundStyle(model.selectedTimeFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.accentLive)
                                    }
                                    Text(filter.rawValue)
                                        .font(.caption.weight(model.selectedTimeFilter == filter ? .semibold : .regular))
                                }
                                .padding(.horizontal, 12)
                                .padding(.vertical, 6)
                                .background(
                                    model.selectedTimeFilter == filter
                                        ? Theme.Colors.accentAction
                                        : Theme.Colors.surfaceElevated,
                                    in: Capsule()
                                )
                                .foregroundStyle(model.selectedTimeFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                .overlay(
                                    Capsule().strokeBorder(model.selectedTimeFilter == filter ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                )
                            }
                            .buttonStyle(.plain)
                        }
                        Spacer()
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.surfaceElevated.opacity(0.2))

                    Group {
                        if model.channels.isEmpty && model.isLoadingChannels {
                            Spacer()
                            ProgressView("Lade Senderliste…")
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
                                LazyVStack(spacing: 10) {
                                    ForEach(model.filteredChannels) { channel in
                                        ChannelRow(
                                            channel: channel,
                                            nowNext: model.schedule[channel.serviceRef],
                                            timeFilter: model.selectedTimeFilter,
                                            isFavorite: model.isFavorite(channel),
                                            onPlay: {
                                                model.playingChannel = channel
                                            },
                                            onShowInfo: { entry in
                                                selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
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
                    VStack(alignment: .leading, spacing: 16) {
                        // Channel & Broadcast Info Card
                        HStack(spacing: 12) {
                            ChannelLogo(url: payload.channel.logoURL, name: payload.channel.name)

                            VStack(alignment: .leading, spacing: 4) {
                                Text(payload.channel.name)
                                    .font(.headline)
                                    .foregroundStyle(Theme.Colors.textPrimary)

                                Text(payload.entry.formattedTimeRange)
                                    .font(.subheadline.monospaced())
                                    .foregroundStyle(Theme.Colors.textSecondary)
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
                        .padding(14)
                        .glassCard(cornerRadius: 14)

                        // Description / Synopsis
                        VStack(alignment: .leading, spacing: 8) {
                            Text("BESCHREIBUNG")
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text(payload.entry.description?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false
                                 ? payload.entry.description!
                                 : "Keine ausführliche Beschreibung für diese Sendung verfügbar.")
                                .font(.body)
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineSpacing(5)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(16)
                        .glassCard(cornerRadius: 14)

                        // Actions
                        VStack(spacing: 12) {
                            Button(action: onPlay) {
                                HStack(spacing: 8) {
                                    Image(systemName: "play.fill")
                                    Text(isLiveNow ? "Sendung Jetzt Anschauen" : "Sender Live Starten")
                                }
                                .font(.headline)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 14)
                                .background(Theme.Colors.accentAction, in: RoundedRectangle(cornerRadius: 12))
                                .foregroundStyle(Theme.Colors.textPrimary)
                            }
                            .buttonStyle(.plain)

                            Button {
                                Task {
                                    isRecording = true
                                    let success = await model.scheduleProgramTimer(channel: payload.channel, entry: payload.entry)
                                    recordSuccess = success
                                    isRecording = false
                                }
                            } label: {
                                HStack(spacing: 8) {
                                    Image(systemName: recordSuccess == true ? "checkmark.circle.fill" : "record.circle")
                                    Text(recordSuccess == true ? "Aufnahme Geplant!" : (isRecording ? "Programmiere…" : "Sendung Aufnehmen"))
                                }
                                .font(.subheadline.weight(.semibold))
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 12)
                                .background(
                                    recordSuccess == true ? Theme.Colors.statusSuccess.opacity(0.2) : Theme.Colors.surfaceGlass,
                                    in: RoundedRectangle(cornerRadius: 12)
                                )
                                .foregroundStyle(recordSuccess == true ? Theme.Colors.statusSuccess : Theme.Colors.textPrimary)
                                .overlay(
                                    RoundedRectangle(cornerRadius: 12)
                                        .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
                                )
                            }
                            .buttonStyle(.plain)
                            .disabled(isRecording || recordSuccess == true)
                        }
                    }
                    .padding(16)
                }
            }
            .navigationTitle("Sendungsdetails")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Schließen") {
                        dismiss()
                    }
                    .foregroundStyle(Theme.Colors.accentAction)
                }
            }
        }
    }
}

// MARK: - Bouquet Picker Pills

struct BouquetPicker: View {

    let model: AppModel

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                if !model.favoriteChannelIDs.isEmpty {
                    BouquetPill(
                        title: "Favoriten",
                        icon: "star.fill",
                        count: model.favoriteChannelIDs.count,
                        isSelected: model.selectedBouquet?.id == AppModel.favoritesBouquetID
                    ) {
                        Task { await model.selectBouquet(Bouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
                    }
                }

                BouquetPill(
                    title: "Alle Sender",
                    count: model.selectedBouquet == nil && model.favoriteChannelIDs.isEmpty ? model.channels.count : nil,
                    isSelected: model.selectedBouquet == nil
                ) {
                    Task { await model.selectBouquet(nil) }
                }

                ForEach(model.bouquets) { bouquet in
                    BouquetPill(
                        title: bouquet.name,
                        count: bouquet.servicesCount > 0 ? bouquet.servicesCount : nil,
                        isSelected: model.selectedBouquet?.id == bouquet.id
                    ) {
                        Task { await model.selectBouquet(bouquet) }
                    }
                }
            }
            .padding(.horizontal, 16)
        }
    }
}

struct BouquetPill: View {

    let title: String
    var icon: String? = nil
    var count: Int? = nil
    let isSelected: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 6) {
                if let icon {
                    Image(systemName: icon)
                        .font(.caption2)
                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.accentLive)
                }

                Text(title)
                    .font(.subheadline.weight(isSelected ? .semibold : .regular))

                if let count {
                    Text("\(count)")
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary.opacity(0.8) : Theme.Colors.textTertiary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(
                isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated,
                in: Capsule()
            )
            .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
            .overlay(
                Capsule()
                    .strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Premium Glass Channel Card

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    var timeFilter: AppModel.TimeFilter = .now
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Top Row: Logo, Number, Name, Live Tag, Remaining Pill, Play Button
            HStack(spacing: 12) {
                // Channel Logo (Direct Play)
                Button(action: onPlay) {
                    ChannelLogo(url: channel.logoURL, name: channel.name)
                }
                .buttonStyle(.plain)

                // Channel Name & Number
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text(number)
                                .font(.caption2.monospacedDigit().bold())
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(channel.name)
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if isFavorite {
                            Image(systemName: "star.fill")
                                .font(.caption2)
                                .foregroundStyle(.yellow)
                        }
                    }
                }

                Spacer(minLength: 4)

                // Live Badge or Remaining Pill
                if let now = nowNext?.now, let remaining = now.remainingMinutes(at: .now) {
                    HStack(spacing: 4) {
                        PulsingLiveDot(size: 4)
                        Text("noch \(remaining)m")
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Theme.Colors.accentLive.opacity(0.12), in: Capsule())
                }

                // Quick Play Action Button
                Button(action: onPlay) {
                    Image(systemName: "play.circle.fill")
                        .font(.system(size: 26))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                .buttonStyle(.plain)
            }

            // Middle Row: Current Program Info & Precision Live Scrubber
            Button {
                if let entry = (timeFilter == .primeTime ? nowNext?.next : nowNext?.now) {
                    onShowInfo(entry)
                } else {
                    onPlay()
                }
            } label: {
                VStack(alignment: .leading, spacing: 6) {
                    if timeFilter == .primeTime, let next = nowNext?.next {
                        HStack(spacing: 6) {
                            Text("20:15")
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1)
                                .background(Theme.Colors.accentLive.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))

                            Text(next.title)
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                        }

                        Text(next.formattedTimeRange)
                            .font(.system(size: 11, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    } else if let now = nowNext?.now {
                        Text(now.title)
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if let description = now.description, !description.isEmpty {
                            Text(description)
                                .font(.caption)
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(1)
                        }

                        if let fraction = now.progress(at: .now) {
                            InfuseScrubber(
                                progress: fraction,
                                startTime: now.formattedStartTime,
                                endTime: now.formattedEndTime,
                                remainingText: nil
                            )
                            .padding(.top, 2)
                        }

                        if let next = nowNext?.next {
                            HStack(spacing: 6) {
                                Text("DANACH:")
                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)

                                Text(next.title)
                                    .font(.caption2)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineLimit(1)

                                Spacer()

                                Text(next.formattedTimeRange)
                                    .font(.system(size: 9, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                            .padding(.top, 2)
                        }
                    } else {
                        Text("Keine Programminformationen verfügbar")
                            .font(.caption)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.plain)
        }
        .padding(14)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
    }
}

// MARK: - Channel Logo (High-Res with Smart Fallback)

struct ChannelLogo: View {

    let url: URL?
    let name: String
    var size: CGFloat = 46

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 10)
                .fill(Theme.Colors.surfaceElevated)
                .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))

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
                            .padding(4)
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
