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
        /// 3 pt per minute: about 100 minutes of a phone screen, which is what
        /// makes this read as a schedule — at 4 pt only a single hour fitted and
        /// two ruler marks were the most that could ever be on screen at once,
        /// so the eye had nothing to measure a block against.
        ///
        /// The cost is paid by the shortest programmes: a 15-minute bulletin is
        /// 43 pt rather than 58, which is one shrunken line of title and no room
        /// for a clock reading. `horizontalPadding` and the title's scale factor
        /// below are sized for exactly that block.
        static let pointsPerMinute: CGFloat = 3.0
        static let rowHeight: CGFloat = 62
        static let rowSpacing: CGFloat = 4
        static let channelColumnWidth: CGFloat = 92
        static let rulerHeight: CGFloat = 32
        static let blockSpacing: CGFloat = 2
        /// The caret that marks "now" on the ruler. A shape rather than a
        /// second clock reading, so no fixed tick has to be suppressed to make
        /// room for it.
        static let nowMarkerSize: CGFloat = 9
        /// How far left of the viewport edge "now" is parked when the grid opens.
        static let nowLeadIn: CGFloat = 56
        /// A pinned title stops sliding here so it cannot leave its own block:
        /// enough for a two-word stub, no more.
        static let minTitleWidth: CGFloat = 58
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

    private func tickDate(_ index: Int) -> Date {
        projection.windowStart.addingTimeInterval(Double(index) * 1800)
    }

    private func tickX(_ index: Int) -> CGFloat {
        CGFloat(index) * 30 * Metrics.pointsPerMinute
    }

    /// Emphasis has to come from the clock, not from the tick's position in the
    /// sequence. The window opens wherever "now" rounds down to, so with a 07:30
    /// start every even index was a *half* hour: the grid drew its bold rules
    /// and bold labels on :30 and left the full hours faint, and which way round
    /// that fell depended on the minute the screen happened to be opened.
    private func isHourTick(_ index: Int) -> Bool {
        Calendar.current.component(.minute, from: tickDate(index)) == 0
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
        let minutes = CGFloat(visibleEnd(of: show).timeIntervalSince(visibleStart(of: show)) / 60)
        return max(minutes * Metrics.pointsPerMinute - Metrics.blockSpacing, 18)
    }

    /// Where a block is *drawn* from, which is not where the programme started.
    ///
    /// A show already running when the window opens occupies the grid from the
    /// window's first minute. Drawing it from its real start while sizing it to
    /// the clipped remainder — which is what this did — shifted the block left
    /// by the clipped part: a morning show that began at 06:00 in a window
    /// starting 07:30 was drawn 360 pt left of the origin and 360 pt too short,
    /// so it landed entirely off-screen and its channel row read as empty.
    private func visibleStart(of show: NowNext.Entry) -> Date {
        max(show.start, projection.windowStart)
    }

    private func visibleEnd(of show: NowNext.Entry) -> Date {
        min(show.end, projection.windowEnd)
    }

    /// How far a block's text is pushed right so it survives being panned past
    /// the viewport's left edge.
    ///
    /// Without it the widest blocks — exactly the ones a reader is most likely
    /// to be looking at — become anonymous coloured slabs, because their title
    /// sits at a leading edge that is hours off-screen. Clamped so the text
    /// never slides out of its own block's trailing edge.
    private func titleInset(for show: NowNext.Entry) -> CGFloat {
        let overshoot = -horizontalOffset - xPosition(for: visibleStart(of: show))
        guard overshoot > 0 else { return 0 }
        return min(overshoot, max(0, width(for: show) - Metrics.minTitleWidth))
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
            // Same clearance the two list modes give the tab bar; without it the
            // last channel row sits underneath it.
            .safeAreaPadding(.bottom, 80)
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
                        height: Metrics.rowHeight,
                        titleInset: titleInset(for: show)
                    )
                    .offset(x: xPosition(for: visibleStart(of: show)))
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
                let isHour = isHourTick(index)
                Rectangle()
                    .fill(isHour ? Theme.Colors.borderElevated : Theme.Colors.borderSubtle)
                    .frame(width: isHour ? 1 : 0.5)
                    .offset(x: tickX(index))
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
                    let isHour = isHourTick(index)
                    Text(Self.rulerFormatter.string(from: tickDate(index)))
                        .font(.system(size: 11, weight: isHour ? .bold : .medium, design: .monospaced))
                        .foregroundStyle(isHour ? Theme.Colors.textSecondary : Theme.Colors.textTertiary)
                        .monospacedDigit()
                        .offset(x: tickX(index) + 6, y: 9)
                }

                if isNowInWindow {
                    NowCaret(size: Metrics.nowMarkerSize)
                        .offset(x: xPosition(for: now) - Metrics.nowMarkerSize / 2,
                                y: Metrics.rulerHeight - Metrics.nowMarkerSize)
                        .allowsHitTesting(false)
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
    /// Set by the grid while the block is panned past the viewport's left edge:
    /// the text slides along inside the block rather than leaving with it.
    var titleInset: CGFloat = 0

    /// A 15-minute block is 43 pt wide, so 8 pt each side spends over a third of
    /// it on air. Narrow blocks buy the title that space back.
    private var horizontalPadding: CGFloat {
        width < 72 ? 4 : 8
    }

    private var contentWidth: CGFloat {
        width - titleInset - horizontalPadding * 2
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(show.title)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(2)
                // Shrink before breaking: in a narrow block SwiftUI splits the
                // one word that does not fit across both lines, and "Garfie/ld"
                // reads as a typo rather than as a title. 0.70 is what a
                // single-word title needs to survive the narrowest block the
                // grid draws: "Garfield" at 12 pt is ~50 pt, the block leaves 35.
                .minimumScaleFactor(0.70)
                .allowsTightening(true)
                .truncationMode(.tail)
                .multilineTextAlignment(.leading)

            // Start only. The block's right edge already states the end, and a
            // full range in every block put eleven digits into each one — twenty
            // blocks of scattered clock readings competing with the ruler that is
            // supposed to be doing this job.
            //
            // Measured against what is left after the inset, not the block: a
            // pinned title must not push the clock reading out of view.
            if contentWidth > 46 {
                Text(show.formattedStartTime)
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(isLive ? Theme.Colors.accentLive : Theme.Colors.textTertiary)
                    .monospacedDigit()
                    .lineLimit(1)
            }

            Spacer(minLength: 0)
        }
        .padding(.leading, horizontalPadding + titleInset)
        .padding(.trailing, horizontalPadding)
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

/// The head of the now-line, where the ruler hands over to the rows.
///
/// A shape rather than a clock reading: the pill it replaces stated a time the
/// status bar already shows, and cost the ruler a fixed label everywhere it
/// landed — at 07:57 that was 08:00, leaving one fixed reading on the screen.
private struct NowCaret: View {
    let size: CGFloat

    var body: some View {
        Path { path in
            path.move(to: CGPoint(x: 0, y: 0))
            path.addLine(to: CGPoint(x: size, y: 0))
            path.addLine(to: CGPoint(x: size / 2, y: size))
            path.closeSubpath()
        }
        .fill(Theme.Colors.accentLive)
        .frame(width: size, height: size)
        .accessibilityHidden(true)
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
