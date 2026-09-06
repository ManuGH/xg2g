// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Shared vocabulary for the guide.
///
/// The screen has one organising idea: clock time is the spine. Every mode puts
/// monospaced digits in the same leading column at the same width, so the eye
/// can run down them regardless of which mode is showing. `timeColumnWidth` is
/// what makes that true, and nothing should hard-code around it.
enum GuideMetrics {
    static let timeColumnWidth: CGFloat = 46
    static let rowVerticalPadding: CGFloat = 10
    static let logoSize: CGFloat = 36
}

/// What the leading column of a row shows.
///
/// Always the key the list is *sorted by*, never just whichever value happens to
/// be available. "Jetzt" is ordered by channel, so a column of start times there
/// would read as a broken sequence; the timeline is ordered by time, so a column
/// of channel numbers would do the same.
enum GuideRowLeading {
    case startTime
    case channelNumber
}

/// The monospaced reading that anchors a row.
struct GuideLeadingLabel: View {
    let kind: GuideRowLeading
    let show: NowNext.Entry
    let channel: Channel
    var isLive: Bool = false

    var body: some View {
        Text(text)
            .font(.system(size: 13, weight: .semibold, design: .monospaced))
            .foregroundStyle(isLive ? Theme.Colors.accentLive : Theme.Colors.textSecondary)
            .frame(width: GuideMetrics.timeColumnWidth, alignment: .leading)
            .monospacedDigit()
            .lineLimit(1)
    }

    private var text: String {
        switch kind {
        case .startTime:
            return Self.formatter.string(from: show.start)
        case .channelNumber:
            return channel.number ?? "–"
        }
    }

    private static let formatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        f.timeZone = .current
        return f
    }()
}

/// How far a running programme has progressed.
///
/// The single amber accent the guide spends its boldness on: it marks "on air"
/// in the list modes and reappears as the now-line in the grid. Nothing else on
/// the screen uses this colour.
struct GuideProgressBar: View {
    let progress: Double
    var height: CGFloat = 3

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule()
                    .fill(Theme.Colors.accentLive.opacity(0.18))

                Capsule()
                    .fill(Theme.Colors.accentLive)
                    .frame(width: max(2, proxy.size.width * progress.clamped01))
            }
        }
        .frame(height: height)
    }
}

/// One programme, as it appears in both list modes.
///
/// Deliberately three things and no more: when, what, and record. The
/// description moved to the detail sheet — at caption size under a title it
/// read as noise, and it was the main reason rows could not be scanned.
struct GuideShowRow: View {
    let channel: Channel
    let show: NowNext.Entry
    let now: Date
    var leading: GuideRowLeading = .startTime
    var showsChannel: Bool = true
    var onOpen: () -> Void
    var onPlay: () -> Void
    var onRecord: () -> Void

    private var progress: Double? { show.progress(at: now) }
    private var isLive: Bool { progress != nil }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            GuideLeadingLabel(kind: leading, show: show, channel: channel, isLive: isLive)

            if showsChannel {
                Button(action: onPlay) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: GuideMetrics.logoSize)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(channel.name) abspielen")
            }

            Button(action: onOpen) {
                VStack(alignment: .leading, spacing: 4) {
                    if showsChannel {
                        Text(channel.name)
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .lineLimit(1)
                    }

                    Text(show.title)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                        .fixedSize(horizontal: false, vertical: true)

                    if let progress, let remaining = show.remainingMinutes(at: now) {
                        HStack(spacing: 8) {
                            GuideProgressBar(progress: progress)
                                .frame(maxWidth: 120)
                            Text("noch \(remaining) Min")
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    } else {
                        Text("\(show.durationMinutes) Min")
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            Button(action: onRecord) {
                Image(systemName: "record.circle")
                    .font(.system(size: 19))
                    .foregroundStyle(Theme.Colors.statusError)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("„\(show.title)“ aufnehmen")
        }
        .padding(.vertical, GuideMetrics.rowVerticalPadding)
    }
}

/// Section header for a half-hour slot.
///
/// The time is set larger than any title on the screen: in this mode the clock
/// is the subject and the programmes are its contents, and the hierarchy should
/// say so without needing a background or a rule.
struct GuideSlotHeader: View {
    let start: Date
    let count: Int
    var isCurrent: Bool = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(Self.formatter.string(from: start))
                .font(.system(size: 22, weight: .bold, design: .monospaced))
                .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                .monospacedDigit()

            if isCurrent {
                PulsingLiveDot(size: 7)
            }

            Text("\(count) \(count == 1 ? "Sendung" : "Sendungen")")
                .font(.system(size: 11, weight: .medium, design: .monospaced))
                .foregroundStyle(Theme.Colors.textTertiary)

            Spacer()
        }
        .padding(.horizontal, 16)
        .padding(.top, 18)
        .padding(.bottom, 8)
        .background(Theme.Colors.bgBase.opacity(0.94))
    }

    private static let formatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        f.timeZone = .current
        return f
    }()
}

// MARK: - Interactive Channel Card (WebUI & OpenWebif Architecture)

/// High-density, rich channel card matching the xg2g WebUI and OpenWebif layout:
/// - Channel Header (Logo, Number, Name, Bouquet badge, Watch button, Favorite button)
/// - Currently running show (Start/End time, title, 1-tap record, progress bar with start, %, end)
/// - Immediate "Danach" preview (Next show time, title, duration)
/// - Expandable day schedule ("Weitere Sendungen anzeigen") revealing upcoming shows with 1-tap timer booking.
struct InteractiveChannelCard: View {
    let channel: Channel
    let shows: [NowNext.Entry]
    let now: Date
    var isFavorite: Bool = false
    var bouquetName: String? = nil
    var onPlay: () -> Void = {}
    var onToggleFavorite: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    @State private var isExpanded = false
    @State private var recordedShowIDs = Set<String>()
    @State private var recordingShowIDs = Set<String>()

    private var currentShow: NowNext.Entry? {
        shows.first { now >= $0.start && now < $0.end } ?? shows.first
    }

    private var upcomingShows: [NowNext.Entry] {
        guard let current = currentShow else { return [] }
        return shows.filter { $0.start >= current.end }
    }

    private var nextShow: NowNext.Entry? {
        upcomingShows.first
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // MARK: - 1. Channel Header (Logo, Number & Name, Badges, Watch button)
            HStack(spacing: 10) {
                ChannelLogo(url: channel.logoURL, name: channel.name, size: 36)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text("\(number) • \(channel.name)")
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                        } else {
                            Text(channel.name)
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                        }
                    }

                    HStack(spacing: 6) {
                        if let bName = bouquetName, !bName.isEmpty {
                            Text(bName.uppercased())
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.accentAction.opacity(0.18), in: RoundedRectangle(cornerRadius: 4))
                        }

                        if channel.name.localizedCaseInsensitiveContains("UHD") {
                            Text("UHD")
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 4))
                        } else if channel.name.localizedCaseInsensitiveContains("HD") {
                            Text("HD")
                                .font(.system(size: 9, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 4))
                        }
                    }
                }

                Spacer()

                // Favorite Star Button
                Button {
                    Haptics.shared.impact(.light)
                    onToggleFavorite()
                } label: {
                    Image(systemName: isFavorite ? "star.fill" : "star")
                        .font(.system(size: 14))
                        .foregroundStyle(isFavorite ? .yellow : Theme.Colors.textTertiary)
                        .padding(6)
                }
                .buttonStyle(.plain)

                // Quick Watch Button (WebUI style)
                Button {
                    Haptics.shared.impact(.medium)
                    onPlay()
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 11, weight: .bold))
                        Text("Watch")
                            .font(.system(size: 12, weight: .bold))
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.accentAction, in: Capsule())
                    .foregroundStyle(.white)
                }
                .buttonStyle(.plain)
            }

            // MARK: - 2. Current Show (Live)
            if let current = currentShow {
                VStack(alignment: .leading, spacing: 5) {
                    HStack(alignment: .center, spacing: 8) {
                        // Sendezeit
                        Text(current.formattedTimeRange)
                            .font(.system(size: 12, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textSecondary)

                        // Restzeit / Live Badge
                        if let remaining = current.remainingMinutes(at: now) {
                            Text("(noch \(remaining)m)")
                                .font(.system(size: 11, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }

                        Spacer()

                        // Record Button
                        recordButton(for: current)
                    }

                    // Show Title (Tappable for details)
                    Button {
                        Haptics.shared.impact(.light)
                        onShowInfo(current)
                    } label: {
                        HStack {
                            Text(current.title)
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                            Spacer()
                        }
                    }
                    .buttonStyle(.plain)

                    // Big Progress Bar with Start / Percent / End (matches WebUI!)
                    if let fraction = current.progress(at: now) {
                        VStack(spacing: 3) {
                            GeometryReader { geo in
                                ZStack(alignment: .leading) {
                                    Capsule()
                                        .fill(Color.white.opacity(0.12))
                                        .frame(height: 5)
                                    Capsule()
                                        .fill(
                                            LinearGradient(
                                                colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                                startPoint: .leading,
                                                endPoint: .trailing
                                            )
                                        )
                                        .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(fraction))), height: 5)
                                }
                            }
                            .frame(height: 5)

                            HStack {
                                Text(current.formattedStartTime)
                                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                Spacer()
                                Text("\(Int(fraction * 100))%")
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                                Spacer()
                                Text(current.formattedEndTime)
                                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }
                        .padding(.top, 2)
                    }
                }
                .padding(10)
                .background(Theme.Colors.surfaceElevated.opacity(0.4), in: RoundedRectangle(cornerRadius: 10))
            } else {
                Text("Keine Programminformationen für diesen Sender")
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .padding(.vertical, 4)
            }

            // MARK: - 3. Collapsed "Danach" Preview (OpenWebif Style)
            if !isExpanded, let next = nextShow {
                HStack(spacing: 6) {
                    Text("DANACH:")
                        .font(.system(size: 9, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(next.formattedTimeRange)
                        .font(.system(size: 11, weight: .medium, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)
                    Text(next.title)
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)
                    Spacer()
                    Text("(\(next.durationMinutes) Min)")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
                .padding(.horizontal, 4)
            }

            // MARK: - 4. Expand / Collapse Schedule Button
            if !upcomingShows.isEmpty {
                Button {
                    Haptics.shared.impact(.light)
                    withAnimation(.spring(response: 0.3, dampingFraction: 0.85)) {
                        isExpanded.toggle()
                    }
                } label: {
                    HStack(spacing: 6) {
                        Spacer()
                        Text(isExpanded ? "Andere Sendungen ausblenden" : "Weitere Sendungen anzeigen (\(upcomingShows.count))")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(Theme.Colors.accentAction)
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(Theme.Colors.accentAction)
                        Spacer()
                    }
                    .padding(.vertical, 7)
                    .background(Theme.Colors.surfaceElevated.opacity(0.6), in: RoundedRectangle(cornerRadius: 8))
                }
                .buttonStyle(.plain)
            }

            // MARK: - 5. Expanded Day Schedule (WebUI Style)
            if isExpanded {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(upcomingShows) { show in
                        HStack(alignment: .top, spacing: 10) {
                            Text(show.formattedTimeRange)
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .frame(width: 84, alignment: .leading)

                            recordButton(for: show)

                            VStack(alignment: .leading, spacing: 2) {
                                Text(show.title)
                                    .font(.system(size: 13, weight: .bold))
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                    .lineLimit(1)

                                if let desc = show.description, !desc.isEmpty {
                                    Text(desc)
                                        .font(.system(size: 11))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .lineLimit(1)
                                }
                            }

                            Spacer()

                            Text("\(show.durationMinutes)m")
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            Haptics.shared.impact(.light)
                            onShowInfo(show)
                        }

                        if show.id != upcomingShows.last?.id {
                            Divider()
                                .background(Theme.Colors.borderSubtle.opacity(0.4))
                        }
                    }
                }
                .padding(.horizontal, 4)
                .padding(.top, 2)
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(Theme.Colors.surfaceElevated.opacity(0.55))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
        )
    }

    private func recordButton(for show: NowNext.Entry) -> some View {
        let isRec = recordedShowIDs.contains(show.id)
        let isBusy = recordingShowIDs.contains(show.id)

        return Button {
            Haptics.shared.impact(.medium)
            recordingShowIDs.insert(show.id)
            onRecord(show)
            Task {
                try? await Task.sleep(for: .milliseconds(400))
                recordingShowIDs.remove(show.id)
                recordedShowIDs.insert(show.id)
            }
        } label: {
            if isBusy {
                ProgressView()
                    .progressViewStyle(.circular)
                    .controlSize(.mini)
                    .tint(Theme.Colors.accentAction)
                    .frame(width: 20, height: 20)
            } else if isRec {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Theme.Colors.statusSuccess)
                    .frame(width: 20, height: 20)
            } else {
                Image(systemName: "record.circle")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Theme.Colors.statusError)
                    .frame(width: 20, height: 20)
            }
        }
        .buttonStyle(.plain)
    }
}

extension Double {
    var clamped01: Double { Swift.min(1, Swift.max(0, self)) }
}
