// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Detailed modal sheet displaying rich EPG programme metadata, live scrubbers,
/// swipe gestures for navigating prior/future shows, and episode reruns with 1-click DVR timer booking.
struct ProgramDetailSheet: View {

    let channel: Channel
    let initialEntry: NowNext.Entry
    var channelSchedule: [NowNext.Entry] = []
    var model: AppModel? = nil
    var onRecord: (NowNext.Entry) -> Void = { _ in }
    @Environment(\.dismiss) private var dismiss

    @State private var currentEntryID: String
    @State private var isRecording = false
    @State private var recordSuccess: Bool?

    init(
        channel: Channel,
        entry: NowNext.Entry,
        channelSchedule: [NowNext.Entry] = [],
        model: AppModel? = nil,
        reruns: [RerunItem] = [],
        onRecord: @escaping (NowNext.Entry) -> Void = { _ in }
    ) {
        self.channel = channel
        self.initialEntry = entry
        self.channelSchedule = channelSchedule
        self.model = model
        self.onRecord = onRecord
        _currentEntryID = State(initialValue: entry.id)
    }

    private var allShows: [NowNext.Entry] {
        if channelSchedule.isEmpty {
            return [initialEntry]
        }
        var list = channelSchedule
        if !list.contains(where: { $0.id == initialEntry.id }) {
            list.append(initialEntry)
        }
        return list.sorted { $0.start < $1.start }
    }

    private var currentIndex: Int {
        allShows.firstIndex(where: { $0.id == currentEntryID }) ?? 0
    }

    private var currentEntry: NowNext.Entry {
        if allShows.indices.contains(currentIndex) {
            return allShows[currentIndex]
        }
        return initialEntry
    }

    private var hasPrevious: Bool {
        currentIndex > 0
    }

    private var hasNext: Bool {
        currentIndex < allShows.count - 1
    }

    private var previousEntry: NowNext.Entry? {
        hasPrevious ? allShows[currentIndex - 1] : nil
    }

    private var nextEntry: NowNext.Entry? {
        hasNext ? allShows[currentIndex + 1] : nil
    }

    private var currentReruns: [RerunItem] {
        model?.findReruns(for: currentEntry, excludingChannelID: channel.id) ?? []
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - Davor / Danach Quick Zap Pager Bar
                    if allShows.count > 1 {
                        HStack(spacing: 8) {
                            // Davor Button
                            Button {
                                goToPrevious()
                            } label: {
                                HStack(spacing: 4) {
                                    Image(systemName: "chevron.left")
                                        .font(.system(size: 11, weight: .bold))
                                    if let prev = previousEntry {
                                        Text(prev.title)
                                            .lineLimit(1)
                                    } else {
                                        Text("Davor")
                                    }
                                }
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(hasPrevious ? Theme.Colors.textPrimary : Theme.Colors.textDisabled)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 6)
                                .background(Theme.Colors.surfaceElevated, in: Capsule())
                            }
                            .disabled(!hasPrevious)
                            .buttonStyle(.plain)

                            Spacer()

                            Text("\(currentIndex + 1) von \(allShows.count)")
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)

                            Spacer()

                            // Danach Button
                            Button {
                                goToNext()
                            } label: {
                                HStack(spacing: 4) {
                                    if let next = nextEntry {
                                        Text(next.title)
                                            .lineLimit(1)
                                    } else {
                                        Text("Danach")
                                    }
                                    Image(systemName: "chevron.right")
                                        .font(.system(size: 11, weight: .bold))
                                }
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(hasNext ? Theme.Colors.textPrimary : Theme.Colors.textDisabled)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 6)
                                .background(Theme.Colors.surfaceElevated, in: Capsule())
                            }
                            .disabled(!hasNext)
                            .buttonStyle(.plain)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                        .background(Theme.Colors.surfaceElevated.opacity(0.4))
                    }

                    // MARK: - Main Scrollable Content with Horizontal Swipe Gesture
                    ScrollView {
                        VStack(alignment: .leading, spacing: 20) {
                            // Header: Channel Logo + Channel Name + Time
                            HStack(spacing: 14) {
                                ChannelLogo(url: channel.logoURL, name: channel.name, size: 56)

                                VStack(alignment: .leading, spacing: 4) {
                                    Text(channel.name)
                                        .font(.headline)
                                        .foregroundStyle(Theme.Colors.textPrimary)

                                    Text(currentEntry.formattedDayHeader)
                                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.accentLive)

                                    Text("\(currentEntry.formattedTimeRange) (\(currentEntry.durationMinutes) Min)")
                                        .font(.subheadline.monospacedDigit())
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }

                                Spacer()
                            }
                            .padding(.bottom, 4)

                            // Genre Tag
                            let currentGenre = currentEntry.genre(channelName: channel.name)
                            if currentGenre != .all {
                                HStack {
                                    Label(currentGenre.rawValue, systemImage: currentGenre.icon)
                                        .font(.caption.weight(.semibold))
                                        .foregroundStyle(Theme.Colors.accentAction)
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 4)
                                        .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())
                                    Spacer()
                                }
                            }

                            // Show Title
                            Text(currentEntry.title)
                                .font(.title2.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            // Live Scrubber (if currently on air)
                            if let fraction = currentEntry.progress(at: .now) {
                                VStack(alignment: .leading, spacing: 6) {
                                    Text("LÄUFT JETZT LIVE")
                                        .font(.caption.weight(.bold).monospaced())
                                        .foregroundStyle(Theme.Colors.accentLive)

                                    InfuseScrubber(
                                        progress: fraction,
                                        startTime: currentEntry.formattedStartTime,
                                        endTime: currentEntry.formattedEndTime,
                                        remainingText: currentEntry.remainingMinutes(at: .now).map { "noch \($0) Min" }
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

                                if let desc = currentEntry.description, !desc.isEmpty {
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

                            // MARK: - Wiederholungen & Weitere Sendetermine
                            let reruns = currentReruns
                            if !reruns.isEmpty {
                                VStack(alignment: .leading, spacing: 10) {
                                    HStack(spacing: 6) {
                                        Image(systemName: "repeat")
                                            .font(.caption.weight(.bold))
                                        Text("WEITERE SENDETERMINE & FOLGEN (\(reruns.count))")
                                            .font(.caption.weight(.bold).monospaced())
                                    }
                                    .foregroundStyle(Theme.Colors.accentLive)

                                    VStack(spacing: 8) {
                                        ForEach(reruns) { rerun in
                                            ExpandableRerunCard(rerun: rerun, onRecord: onRecord)
                                        }
                                    }
                                }
                            }

                            Spacer(minLength: 20)

                            // 1-Click Timer Button
                            Button {
                                Task {
                                    isRecording = true
                                    onRecord(currentEntry)
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
                    .simultaneousGesture(
                        DragGesture(minimumDistance: 25)
                            .onEnded { value in
                                let horizontal = value.translation.width
                                let vertical = value.translation.height
                                guard abs(horizontal) > abs(vertical) * 1.5, abs(horizontal) > 40 else { return }
                                if horizontal < 0 {
                                    // Swipe Left -> Nächste Sendung (Danach)
                                    goToNext()
                                } else {
                                    // Swipe Right -> Vorherige Sendung (Davor)
                                    goToPrevious()
                                }
                            }
                    )
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

    private func goToPrevious() {
        guard hasPrevious else { return }
        triggerHaptic(.light)
        withAnimation(.spring(response: 0.3, dampingFraction: 0.85)) {
            currentEntryID = allShows[currentIndex - 1].id
            recordSuccess = nil
        }
    }

    private func goToNext() {
        guard hasNext else { return }
        triggerHaptic(.light)
        withAnimation(.spring(response: 0.3, dampingFraction: 0.85)) {
            currentEntryID = allShows[currentIndex + 1].id
            recordSuccess = nil
        }
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}

// MARK: - Expandable Rerun & Episode Card (Full EPG Synopsis on Tap)

struct ExpandableRerunCard: View {

    let rerun: RerunItem
    var onRecord: (NowNext.Entry) -> Void

    @State private var isExpanded = false
    @State private var isRecording = false
    @State private var recordSuccess = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header Row (Tap to expand/collapse full episode synopsis)
            Button {
                triggerHaptic(.light)
                withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: 12) {
                    ChannelLogo(url: rerun.channel.logoURL, name: rerun.channel.name, size: 36)

                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(rerun.channel.name)
                                .font(.system(size: 13, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            Text("•")
                                .font(.caption2)
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text(rerun.formattedRelativeTime)
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                        }

                        if let desc = rerun.entry.description, !desc.isEmpty {
                            Text(desc)
                                .font(.caption)
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(isExpanded ? nil : 1)
                        } else {
                            Text(rerun.entry.title)
                                .font(.caption)
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .lineLimit(1)
                        }
                    }

                    Spacer()

                    Image(systemName: "chevron.down")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .rotationEffect(.degrees(isExpanded ? 180 : 0))
                }
                .padding(12)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            // Expanded Details & Description
            if isExpanded {
                VStack(alignment: .leading, spacing: 10) {
                    Divider()
                        .background(Theme.Colors.borderSubtle)
                        .padding(.horizontal, 12)

                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 8) {
                            Text(rerun.entry.title)
                                .font(.system(size: 14, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            Spacer()

                            Text("\(rerun.entry.formattedTimeRange) (\(rerun.entry.durationMinutes) Min)")
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }

                        if let desc = rerun.entry.description, !desc.isEmpty {
                            Text(desc)
                                .font(.system(size: 13))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineSpacing(3)
                                .fixedSize(horizontal: false, vertical: true)
                        } else {
                            Text("Keine separate Folgen-Beschreibung vorhanden.")
                                .font(.system(size: 12))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                    .padding(.horizontal, 12)

                    // 1-Click Timer Button for this specific episode
                    Button {
                        triggerHaptic(.medium)
                        Task {
                            isRecording = true
                            onRecord(rerun.entry)
                            try? await Task.sleep(for: .milliseconds(400))
                            isRecording = false
                            recordSuccess = true
                        }
                    } label: {
                        HStack(spacing: 6) {
                            Spacer()
                            if isRecording {
                                ProgressView()
                                    .tint(.white)
                            } else if recordSuccess {
                                Image(systemName: "checkmark")
                                Text("Folge programmiert")
                                    .font(.system(size: 12, weight: .bold))
                            } else {
                                Image(systemName: "record.circle")
                                Text("Diese Folge aufnehmen")
                                    .font(.system(size: 12, weight: .bold))
                            }
                            Spacer()
                        }
                        .padding(.vertical, 8)
                        .background(
                            recordSuccess ? Theme.Colors.statusSuccess : Theme.Colors.statusError.opacity(0.85),
                            in: RoundedRectangle(cornerRadius: 8, style: .continuous)
                        )
                        .foregroundStyle(.white)
                    }
                    .buttonStyle(.plain)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 10)
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .background(
            Theme.Colors.surfaceElevated,
            in: RoundedRectangle(cornerRadius: 12, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
        )
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}
