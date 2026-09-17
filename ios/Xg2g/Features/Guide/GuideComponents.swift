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

extension Double {
    var clamped01: Double { Swift.min(1, Swift.max(0, self)) }
}
