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
                    if !model.bouquets.isEmpty {
                        BouquetPicker(model: model)
                            .padding(.top, 8)
                            .padding(.bottom, 4)
                            .background(Theme.Colors.surfaceElevated.opacity(0.5))
                    }

                    // Time Window Filter Pills (Jetzt vs 20:15)
                    HStack(spacing: 8) {
                        ForEach(AppModel.TimeFilter.allCases) { filter in
                            Button {
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
                                .padding(.horizontal, 10)
                                .padding(.vertical, 4)
                                .background(
                                    model.selectedTimeFilter == filter
                                        ? Theme.Colors.accentAction
                                        : Theme.Colors.surfaceGlass,
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
                    .background(Theme.Colors.surfaceElevated.opacity(0.3))

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
                            List(model.filteredChannels) { channel in
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
                                .buttonStyle(.plain)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                                .swipeActions(edge: .leading, allowsFullSwipe: true) {
                                    Button {
                                        model.toggleFavorite(channel)
                                    } label: {
                                        Label(
                                            model.isFavorite(channel) ? "Entfernen" : "Favorit",
                                            systemImage: model.isFavorite(channel) ? "star.slash.fill" : "star.fill"
                                        )
                                    }
                                    .tint(Theme.Colors.accentLive)
                                }
                                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                    Button {
                                        Task {
                                            _ = await model.recordLiveNow(channel: channel)
                                        }
                                    } label: {
                                        Label("Aufnehmen", systemImage: "record.circle")
                                    }
                                    .tint(Theme.Colors.statusError)
                                }
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
                            .listStyle(.plain)
                            .scrollContentBackground(.hidden)
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
            .presentationDetents([.medium, .fraction(0.8)])
            .presentationDragIndicator(.visible)
        }
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
    @State private var recordSuccess: Bool?
    @State private var isRecording = false

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                ScrollView {
                    VStack(alignment: .leading, spacing: 18) {
                        // Header: Channel & Time
                        HStack(spacing: 12) {
                            ChannelLogo(url: payload.channel.logoURL, name: payload.channel.name)

                            VStack(alignment: .leading, spacing: 2) {
                                Text(payload.channel.name)
                                    .font(.headline)
                                    .foregroundStyle(Theme.Colors.textPrimary)

                                Text(payload.entry.formattedTimeRange)
                                    .font(.subheadline.monospacedDigit())
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }

                            Spacer()

                            if let remaining = payload.entry.remainingMinutes(at: .now) {
                                Text("noch \(remaining)m")
                                    .font(.caption.weight(.semibold).monospacedDigit())
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 4)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                            }
                        }
                        .padding(14)
                        .glassCard(cornerRadius: 14)

                        // Title & Progress
                        VStack(alignment: .leading, spacing: 8) {
                            Text(payload.entry.title)
                                .font(.title2.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            if let progress = payload.entry.progress(at: .now) {
                                VStack(alignment: .leading, spacing: 4) {
                                    ProgressView(value: progress)
                                        .progressViewStyle(.linear)
                                        .tint(Theme.Colors.accentLive)

                                    HStack {
                                        Text("\(Int(progress * 100))% vergangen")
                                            .font(.caption2.monospacedDigit())
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                        Spacer()
                                        Text("Endet um \(payload.entry.formattedEndTime)")
                                            .font(.caption2.monospacedDigit())
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                    }
                                }
                            }
                        }

                        // Description / Synopsis
                        if let desc = payload.entry.description, !desc.isEmpty {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("ÜBER DIESE SENDUNG")
                                    .font(.system(size: 11, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)

                                Text(desc)
                                    .font(.body)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineSpacing(4)
                            }
                            .padding(14)
                            .glassCard(cornerRadius: 14)
                        }

                        // Actions
                        VStack(spacing: 10) {
                            Button(action: onPlay) {
                                HStack {
                                    Image(systemName: "play.fill")
                                    Text("Sender Jetzt Live Ansehen")
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
                                HStack {
                                    Image(systemName: recordSuccess == true ? "checkmark.circle.fill" : "record.circle")
                                    Text(recordSuccess == true ? "Timer Erstellt!" : (isRecording ? "Programmiere…" : "Sendung Aufnehmen"))
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
                    Button("Fertig") {
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
    var count: Int?
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
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
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(
                isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
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

// MARK: - Channel Row

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    var timeFilter: AppModel.TimeFilter = .now
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        Button(action: onPlay) {
            HStack(spacing: 12) {
                ChannelLogo(url: channel.logoURL, name: channel.name)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text(number)
                                .font(.caption.monospacedDigit().weight(.semibold))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .frame(minWidth: 24, alignment: .leading)
                        }
                        Text(channel.name)
                            .font(.body.weight(.semibold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if isFavorite {
                            Image(systemName: "star.fill")
                                .font(.caption2)
                                .foregroundStyle(Theme.Colors.accentLive)
                        }

                        Spacer()
                    }

                    if timeFilter == .primeTime, let next = nowNext?.next {
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(spacing: 5) {
                                Text("20:15")
                                    .font(.system(size: 8, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 4)
                                    .padding(.vertical, 1)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))

                                Text(next.title)
                                    .font(.caption.weight(.medium))
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineLimit(1)
                            }

                            Text(next.formattedTimeRange)
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    } else if let now = nowNext?.now {
                        HStack(spacing: 5) {
                            PulsingLiveDot(size: 5)
                            Text(now.title)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(1)
                        }

                        if let fraction = now.progress(at: .now) {
                            HStack(spacing: 6) {
                                ProgressView(value: fraction)
                                    .progressViewStyle(.linear)
                                    .tint(Theme.Colors.accentLive)

                                if let remaining = now.remainingMinutes(at: .now) {
                                    Text("noch \(remaining)m")
                                        .font(.system(size: 9, weight: .medium, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.accentLive)
                                } else {
                                    Text("\(Int(fraction * 100))%")
                                        .font(.system(size: 9, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                }
                            }
                        }

                        if let next = nowNext?.next {
                            HStack(spacing: 4) {
                                Text("Danach:")
                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                Text("\(next.title) (\(next.formattedEndTime))")
                                    .font(.system(size: 9))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                    .lineLimit(1)
                            }
                        }
                    } else {
                        Text("Keine Programminformationen")
                            .font(.caption)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }

                Spacer(minLength: 0)

                // Info Button for Program Sheet
                if let entry = (timeFilter == .primeTime ? nowNext?.next : nowNext?.now) {
                    Button {
                        onShowInfo(entry)
                    } label: {
                        Image(systemName: "info.circle")
                            .font(.body)
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .padding(6)
                    }
                    .buttonStyle(.plain)
                }

                Image(systemName: "play.circle.fill")
                    .font(.title3)
                    .foregroundStyle(Theme.Colors.accentAction)
            }
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }
    }
}

// MARK: - Channel Logo (High-Res with Smart Fallback)

struct ChannelLogo: View {

    let url: URL?
    let name: String

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(white: 0.12))
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
                )

            if let url {
                AsyncImage(url: url) { phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFit()
                            .padding(3)
                    case .failure, .empty:
                        fallbackView
                    @unknown default:
                        fallbackView
                    }
                }
            } else {
                fallbackView
            }
        }
        .frame(width: 56, height: 40)
    }

    private var fallbackView: some View {
        VStack(spacing: 0) {
            Text(initials)
                .font(.system(size: 11, weight: .bold, design: .rounded))
                .foregroundStyle(Theme.Colors.textPrimary)
        }
        .padding(2)
    }

    private var initials: String {
        let clean = name.replacingOccurrences(of: "HD", with: "")
            .replacingOccurrences(of: "UHD", with: "")
            .replacingOccurrences(of: "Austria", with: "")
            .trimmingCharacters(in: .whitespaces)
        let words = clean.split(separator: " ")
        if words.count >= 2 {
            return "\(words[0].prefix(2))\(words[1].prefix(1))".uppercased()
        }
        return String(clean.prefix(3)).uppercased()
    }
}
