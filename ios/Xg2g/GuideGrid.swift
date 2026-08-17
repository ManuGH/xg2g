// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Channels down, time across, blocks drawn to their real duration.
///
/// The one view where a programme's *length* is visible rather than stated, which
/// is what makes an evening readable at a glance — a two-hour film and a ten
/// minute news bulletin stop looking alike.
///
/// ## Why there is no inner scroll view
///
/// A grid needs two things that nesting scroll views cannot both provide:
/// vertical laziness, and one pinned edge per axis. Nesting pins exactly one
/// edge — whichever element sits outside one scroll view is inside the other —
/// and a `LazyVStack` inside the inner scroll view is not lazy against the outer
/// one. That was measured, not assumed: 200 channels built **6000 of 6000**
/// blocks for eleven visible rows.
///
/// Culling by hand needs the scroll offset, and observing that through a
/// `PreferenceKey` did not work here either — measured twice, once as a readout
/// frozen at `0/0` while the content was visibly scrolled, once as culling that
/// stayed on the first screen after a 3000 pt jump.
///
/// So the timeline is panned by one piece of state instead. Vertical scrolling
/// stays a real `ScrollView` over a `LazyVStack`, which makes laziness and the
/// pinned ruler SwiftUI's own job; horizontal movement is a single offset shared
/// by the ruler and every row, which makes their alignment exact by
/// construction rather than by observation.
struct GuideGrid: View {

    let projection: GuideProjection
    let now: Date
    let onOpen: (GuideEntry) -> Void
    let onPlay: (Channel) -> Void

    @State private var horizontalOffset: CGFloat = 0
    @State private var dragAnchor: CGFloat = 0
    @State private var viewportWidth: CGFloat = 0
    @State private var hasCentredOnNow = false

    private enum Metrics {
        /// 4 pt per minute: a 15-minute bulletin still gets 60 pt, which is the
        /// narrowest a title can be read in. Below that every short programme
        /// truncated to an unreadable stub.
        static let pointsPerMinute: CGFloat = 4.0
        static let rowHeight: CGFloat = 62
        static let rowSpacing: CGFloat = 4
        static let channelColumnWidth: CGFloat = 92
        static let rulerHeight: CGFloat = 32
        static let blockSpacing: CGFloat = 2
        /// Ruler ticks this close to the now-marker are suppressed so the two
        /// clock readings cannot overprint each other. Sized for half a tick
        /// label plus half the marker pill.
        static let nowLabelClearance: CGFloat = 52
        /// How far left of the viewport edge "now" is parked when the grid opens.
        static let nowLeadIn: CGFloat = 56
    }

    // MARK: - Geometry

    private var totalMinutes: CGFloat {
        CGFloat(projection.windowEnd.timeIntervalSince(projection.windowStart) / 60)
    }

    private var timelineWidth: CGFloat {
        max(totalMinutes * Metrics.pointsPerMinute, 1)
    }

    private var timelineViewport: CGFloat {
        max(viewportWidth - Metrics.channelColumnWidth, 1)
    }

    private var maxPan: CGFloat {
        max(0, timelineWidth - timelineViewport)
    }

    private var halfHourCount: Int {
        Int((totalMinutes / 30).rounded(.up))
    }

    private var isNowInWindow: Bool {
        now >= projection.windowStart && now <= projection.windowEnd
    }

    private func clampPan(_ value: CGFloat) -> CGFloat {
        min(0, max(-maxPan, value))
    }

    private func xPosition(for date: Date) -> CGFloat {
        CGFloat(date.timeIntervalSince(projection.windowStart) / 60) * Metrics.pointsPerMinute
    }

    private func width(for show: NowNext.Entry) -> CGFloat {
        let start = max(show.start, projection.windowStart)
        let end = min(show.end, projection.windowEnd)
        let minutes = CGFloat(end.timeIntervalSince(start) / 60)
        return max(minutes * Metrics.pointsPerMinute - Metrics.blockSpacing, 18)
    }

    // MARK: - Body

    var body: some View {
        ScrollView(.vertical, showsIndicators: false) {
            LazyVStack(spacing: 0, pinnedViews: [.sectionHeaders]) {
                Section {
                    ForEach(projection.channels) { schedule in
                        gridRow(schedule)
                    }
                } header: {
                    rulerRow
                }
            }
        }
        .background(Theme.Colors.bgBase)
        .overlay(alignment: .topLeading) {
            // Static divider between the pinned column and the timeline. Drawn
            // once here rather than per row so it reads as one continuous edge.
            Rectangle()
                .fill(Theme.Colors.borderElevated)
                .frame(width: 1)
                .offset(x: Metrics.channelColumnWidth)
                .allowsHitTesting(false)
        }
        .background {
            GeometryReader { proxy in
                Color.clear
                    .onAppear {
                        viewportWidth = proxy.size.width
                        centreOnNowIfNeeded()
                    }
                    .onChange(of: proxy.size.width) { _, width in
                        viewportWidth = width
                        horizontalOffset = clampPan(horizontalOffset)
                        dragAnchor = horizontalOffset
                    }
            }
        }
        .simultaneousGesture(panGesture)
    }

    /// Horizontal panning. `simultaneousGesture` so the vertical scroll view
    /// keeps its own drag; the dominance check stops a vertical flick from
    /// dragging the timeline sideways.
    private var panGesture: some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { value in
                guard abs(value.translation.width) > abs(value.translation.height) else { return }
                horizontalOffset = clampPan(dragAnchor + value.translation.width)
            }
            .onEnded { value in
                guard abs(value.translation.width) > abs(value.translation.height) else {
                    dragAnchor = horizontalOffset
                    return
                }
                let projected = clampPan(dragAnchor + value.predictedEndTranslation.width)
                withAnimation(.easeOut(duration: 0.35)) {
                    horizontalOffset = projected
                }
                dragAnchor = projected
            }
    }

    private func centreOnNowIfNeeded() {
        guard !hasCentredOnNow, isNowInWindow, viewportWidth > 0 else { return }
        hasCentredOnNow = true
        // Open on the current moment: with "Ganztägig" the window starts at
        // midnight, so the part of the day people care about would otherwise be
        // several screens to the right.
        let target = clampPan(-(xPosition(for: now) - Metrics.nowLeadIn))
        horizontalOffset = target
        dragAnchor = target
    }

    // MARK: - Rows

    private func gridRow(_ schedule: GuideChannelSchedule) -> some View {
        HStack(spacing: 0) {
            channelCell(schedule)

            ZStack(alignment: .topLeading) {
                halfHourRules
                    .frame(width: timelineWidth, height: Metrics.rowHeight)

                ForEach(schedule.shows) { show in
                    GuideGridBlock(
                        show: show,
                        channelName: schedule.channel.name,
                        isLive: show.progress(at: now) != nil,
                        width: width(for: show),
                        height: Metrics.rowHeight
                    )
                    .offset(x: xPosition(for: show.start))
                    .onTapGesture {
                        onOpen(GuideEntry(channel: schedule.channel, show: show))
                    }
                }

                if isNowInWindow {
                    Rectangle()
                        .fill(Theme.Colors.accentLive)
                        .frame(width: 2, height: Metrics.rowHeight)
                        .offset(x: xPosition(for: now))
                        .allowsHitTesting(false)
                }
            }
            .frame(width: timelineWidth, height: Metrics.rowHeight, alignment: .topLeading)
            .offset(x: horizontalOffset)
            .frame(width: timelineViewport, height: Metrics.rowHeight, alignment: .leading)
            .clipped()
        }
        .frame(height: Metrics.rowHeight)
        .padding(.bottom, Metrics.rowSpacing)
    }

    private func channelCell(_ schedule: GuideChannelSchedule) -> some View {
        Button {
            onPlay(schedule.channel)
        } label: {
            // Name rather than channel number: when a logo fails to load the
            // column otherwise reads as an anonymous badge and a digit, and no
            // row can be identified.
            VStack(spacing: 3) {
                ChannelLogo(url: schedule.channel.logoURL, name: schedule.channel.name, size: 28)

                Text(schedule.channel.name)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .padding(.horizontal, 4)
            }
            .frame(width: Metrics.channelColumnWidth, height: Metrics.rowHeight)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// Faint rules every half hour. The only decoration here, and load-bearing:
    /// without it a block's width cannot be read as a duration.
    private var halfHourRules: some View {
        ZStack(alignment: .topLeading) {
            ForEach(0..<max(halfHourCount, 1), id: \.self) { index in
                Rectangle()
                    .fill(Theme.Colors.borderSubtle)
                    .frame(width: index % 2 == 0 ? 1 : 0.5)
                    .offset(x: CGFloat(index) * 30 * Metrics.pointsPerMinute)
            }
        }
    }

    // MARK: - Ruler (pinned by the LazyVStack, not by an observed offset)

    private var rulerRow: some View {
        HStack(spacing: 0) {
            Theme.Colors.bgBase
                .frame(width: Metrics.channelColumnWidth, height: Metrics.rulerHeight)

            ZStack(alignment: .topLeading) {
                Theme.Colors.bgBase
                    .frame(width: timelineWidth, height: Metrics.rulerHeight)

                ForEach(0..<max(halfHourCount, 1), id: \.self) { index in
                    let tick = projection.windowStart.addingTimeInterval(Double(index) * 1800)
                    let tickX = CGFloat(index) * 30 * Metrics.pointsPerMinute
                    if !collidesWithNowMarker(tickX) {
                        Text(Self.rulerFormatter.string(from: tick))
                            .font(.system(size: 11, weight: index % 2 == 0 ? .bold : .medium, design: .monospaced))
                            .foregroundStyle(index % 2 == 0 ? Theme.Colors.textSecondary : Theme.Colors.textTertiary)
                            .monospacedDigit()
                            .offset(x: tickX + 6, y: 9)
                    }
                }

                if isNowInWindow {
                    Text(Self.rulerFormatter.string(from: now))
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.bgBase)
                        .monospacedDigit()
                        .padding(.horizontal, 5)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.accentLive, in: Capsule())
                        .offset(x: xPosition(for: now) - 22, y: 6)
                }
            }
            .frame(width: timelineWidth, height: Metrics.rulerHeight, alignment: .topLeading)
            .offset(x: horizontalOffset)
            .frame(width: timelineViewport, height: Metrics.rulerHeight, alignment: .leading)
            .clipped()
        }
        .frame(height: Metrics.rulerHeight)
        .background(Theme.Colors.bgBase)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(Theme.Colors.borderElevated)
                .frame(height: 1)
        }
    }

    /// The now-marker wins over a fixed tick: it is the reading people look for,
    /// and two overlapping clock values are worse than one missing gridline label.
    private func collidesWithNowMarker(_ tickX: CGFloat) -> Bool {
        guard isNowInWindow else { return false }
        return abs(tickX - xPosition(for: now)) < Metrics.nowLabelClearance
    }

    private static let rulerFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        f.timeZone = .current
        return f
    }()
}

/// A single programme block, sized to its runtime.
private struct GuideGridBlock: View {
    let show: NowNext.Entry
    let channelName: String
    let isLive: Bool
    let width: CGFloat
    let height: CGFloat

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(show.title)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(2)
                .multilineTextAlignment(.leading)

            if width > 90 {
                Text(show.formattedTimeRange)
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(isLive ? Theme.Colors.accentLive : Theme.Colors.textTertiary)
                    .monospacedDigit()
            }

            Spacer(minLength: 0)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 7)
        .frame(width: width, height: height, alignment: .topLeading)
        .background(
            isLive ? Theme.Colors.accentLive.opacity(0.14) : Theme.Colors.surfaceElevated,
            in: RoundedRectangle(cornerRadius: 7, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .strokeBorder(
                    isLive ? Theme.Colors.accentLive.opacity(0.55) : Theme.Colors.borderSubtle,
                    lineWidth: isLive ? 1 : 0.5
                )
        )
        .contentShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(channelName), \(show.title), \(show.formattedTimeRange)")
    }
}

#if DEBUG
#Preview("Raster") {
    GuideGrid(
        projection: GuidePreviewData.projection,
        now: .now,
        onOpen: { _ in },
        onPlay: { _ in }
    )
    .background(Theme.Colors.bgBase)
}
#endif
