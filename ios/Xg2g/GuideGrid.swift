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
    var bottomPadding: CGFloat = 80
    let onOpen: (GuideEntry) -> Void
    let onPlay: (Channel) -> Void
    var onRecord: (NowNext.Entry, Channel) -> Void = { _, _ in }

    @AppStorage("guideShowSpotlight") private var showSpotlight: Bool = true
    @State private var selectedEntry: GuideEntry?

    @State private var horizontalOffset: CGFloat = 0
    @State private var dragAnchor: CGFloat = 0
    @State private var viewportWidth: CGFloat = 0
    @State private var hasCentredOnNow = false

    private var effectiveSelectedEntry: GuideEntry? {
        if let selected = selectedEntry,
           let schedule = projection.channels.first(where: { $0.channel.id == selected.channel.id }),
           schedule.shows.contains(where: { $0.id == selected.show.id }) {
            return selected
        }
        if let onAir = projection.onAir.first {
            return onAir
        }
        if let firstSchedule = projection.channels.first, let firstShow = firstSchedule.shows.first {
            return GuideEntry(channel: firstSchedule.channel, show: firstShow)
        }
        return nil
    }

    private var isPad: Bool {
        UIDevice.current.userInterfaceIdiom == .pad
    }

    private var pointsPerMinute: CGFloat {
        isPad ? 3.5 : 4.5
    }

    private var channelColumnWidth: CGFloat {
        isPad ? 92 : 78
    }

    private var rowHeight: CGFloat {
        isPad ? 64 : 70
    }

    private enum Metrics {
        static let rowSpacing: CGFloat = 4
        static let rulerHeight: CGFloat = 32
        static let blockSpacing: CGFloat = 2
        static let nowMarkerSize: CGFloat = 9
        static let nowLeadIn: CGFloat = 56
        static let minTitleWidth: CGFloat = 42
    }

    // MARK: - Geometry

    private var totalMinutes: CGFloat {
        CGFloat(projection.windowEnd.timeIntervalSince(projection.windowStart) / 60)
    }

    private var timelineWidth: CGFloat {
        max(totalMinutes * pointsPerMinute, 1)
    }

    private var timelineViewport: CGFloat {
        max(viewportWidth - channelColumnWidth, 1)
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
        CGFloat(index) * 30 * pointsPerMinute
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
        CGFloat(date.timeIntervalSince(projection.windowStart) / 60) * pointsPerMinute
    }

    private func width(for show: NowNext.Entry) -> CGFloat {
        let minutes = CGFloat(visibleEnd(of: show).timeIntervalSince(visibleStart(of: show)) / 60)
        return max(minutes * pointsPerMinute - Metrics.blockSpacing, 28)
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
        VStack(spacing: 0) {
            if showSpotlight, let entry = effectiveSelectedEntry {
                spotlightBanner(for: entry)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }

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
                // Same clearance the list modes give the tab bar; without it the
                // last channel row sits underneath it.
                .safeAreaPadding(.bottom, bottomPadding)
            }
            .background(Theme.Colors.bgBase)
            .overlay(alignment: .topLeading) {
                // Static divider between the pinned column and the timeline. Drawn
                // once here rather than per row so it reads as one continuous edge.
                Rectangle()
                    .fill(Theme.Colors.borderElevated)
                    .frame(width: 1)
                    .offset(x: channelColumnWidth)
                    .allowsHitTesting(false)
            }
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

    // MARK: - Spotlight Inspector

    @ViewBuilder
    private func spotlightBanner(for entry: GuideEntry) -> some View {
        let isLive = entry.show.progress(at: now) != nil

        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 10) {
                VStack(alignment: .leading, spacing: 4) {
                    // Title
                    Text(entry.show.title)
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    // Meta: LIVE badge, Time range, Channel name & logo, Progress
                    HStack(spacing: 6) {
                        if isLive {
                            HStack(spacing: 3) {
                                Circle()
                                    .fill(.white)
                                    .frame(width: 5, height: 5)
                                Text("LIVE")
                                    .font(.system(size: 9, weight: .heavy))
                            }
                            .foregroundStyle(.white)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Theme.Colors.statusError, in: Capsule())
                        }

                        Text(entry.show.formattedTimeRange)
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(isLive ? Theme.Colors.accentLive : Theme.Colors.textSecondary)

                        Text("•")
                            .font(.system(size: 10))
                            .foregroundStyle(Theme.Colors.textTertiary)

                        HStack(spacing: 4) {
                            ChannelLogo(url: entry.channel.logoURL, name: entry.channel.name, size: 14)
                            Text(entry.channel.name)
                                .font(.system(size: 11, weight: .medium))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(1)
                        }

                        if let progress = entry.show.progress(at: now) {
                            Text("• \(Int(progress * 100))%")
                                .font(.system(size: 10.5, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    }
                }

                Spacer(minLength: 8)

                // Close / dismiss button
                Button {
                    triggerHaptic(.light)
                    withAnimation(.easeInOut(duration: 0.2)) {
                        showSpotlight = false
                    }
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 18))
                        .foregroundStyle(Theme.Colors.textTertiary.opacity(0.85))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Vorschau schließen")
            }

            // Synopsis / Description
            if let desc = entry.show.description, !desc.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text(desc)
                    .font(.system(size: 11.5))
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
            }

            // Actions row: Ansehen, Aufnehmen, Details
            HStack(spacing: 8) {
                Button {
                    triggerHaptic(.light)
                    onPlay(entry.channel)
                } label: {
                    HStack(spacing: 5) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 10, weight: .bold))
                        Text(isLive ? "Live ansehen" : "Sender starten")
                            .font(.system(size: 11.5, weight: .bold))
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.accentAction, in: Capsule())
                }
                .buttonStyle(.plain)

                Button {
                    triggerHaptic(.medium)
                    onRecord(entry.show, entry.channel)
                } label: {
                    HStack(spacing: 4) {
                        Circle()
                            .fill(Theme.Colors.statusError)
                            .frame(width: 7, height: 7)
                        Text("Aufnehmen")
                            .font(.system(size: 11.5, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                Button {
                    triggerHaptic(.light)
                    onOpen(entry)
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "info.circle")
                            .font(.system(size: 11, weight: .semibold))
                        Text("Details")
                            .font(.system(size: 11.5, weight: .semibold))
                    }
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                Spacer()
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(
            Theme.Colors.surfaceElevated.opacity(0.92)
                .background(.ultraThinMaterial)
        )
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(Theme.Colors.borderElevated)
                .frame(height: 1)
        }
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
                    .frame(width: timelineWidth, height: rowHeight)

                ForEach(schedule.shows) { show in
                    let isSelected: Bool = {
                        guard showSpotlight, let selected = effectiveSelectedEntry else { return false }
                        return selected.show.id == show.id && selected.channel.id == schedule.channel.id
                    }()
                    GuideGridBlock(
                        show: show,
                        channelName: schedule.channel.name,
                        isLive: show.progress(at: now) != nil,
                        isSelected: isSelected,
                        width: width(for: show),
                        height: rowHeight,
                        titleInset: titleInset(for: show)
                    )
                    .offset(x: xPosition(for: visibleStart(of: show)))
                    .onTapGesture {
                        let entry = GuideEntry(channel: schedule.channel, show: show)
                        if showSpotlight {
                            if selectedEntry?.id == entry.id {
                                onOpen(entry)
                            } else {
                                triggerHaptic(.light)
                                withAnimation(.easeInOut(duration: 0.15)) {
                                    selectedEntry = entry
                                }
                            }
                        } else {
                            onOpen(entry)
                        }
                    }
                }

                if isNowInWindow {
                    Rectangle()
                        .fill(Theme.Colors.accentLive)
                        .frame(width: 2, height: rowHeight)
                        .offset(x: xPosition(for: now))
                        .allowsHitTesting(false)
                }
            }
            .frame(width: timelineWidth, height: rowHeight, alignment: .topLeading)
            .offset(x: horizontalOffset)
            .frame(width: timelineViewport, height: rowHeight, alignment: .leading)
            .clipped()
        }
        .frame(height: rowHeight)
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
            .frame(width: channelColumnWidth, height: rowHeight)
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
                .frame(width: channelColumnWidth, height: Metrics.rulerHeight)

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
                    // Exact time badge (like in media_1788492472556.png)
                    Text(Self.rulerFormatter.string(from: now))
                        .font(.system(size: 9.5, weight: .heavy, design: .monospaced))
                        .foregroundStyle(.white)
                        .monospacedDigit()
                        .padding(.horizontal, 5)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.accentLive, in: Capsule())
                        .shadow(color: .black.opacity(0.35), radius: 2, y: 1)
                        .offset(x: xPosition(for: now) - 20, y: 3)
                        .allowsHitTesting(false)

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
    var isSelected: Bool = false
    let width: CGFloat
    let height: CGFloat
    /// Set by the grid while the block is panned past the viewport's left edge:
    /// the text slides along inside the block rather than leaving with it.
    var titleInset: CGFloat = 0

    /// A 15-minute block is 43 pt wide, so 8 pt each side spends over a third of
    /// it on air. Narrow blocks buy the title that space back.
    private var horizontalPadding: CGFloat {
        width < 55 ? 3 : (width < 90 ? 5 : 8)
    }

    private var contentWidth: CGFloat {
        width - titleInset - horizontalPadding * 2
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(show.title)
                .font(.system(size: 11.5, weight: isSelected ? .bold : .semibold))
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(3)
                .minimumScaleFactor(0.65)
                .allowsTightening(true)
                .truncationMode(.tail)
                .multilineTextAlignment(.leading)

            // Start time
            if contentWidth > 38 && height >= 64 {
                Text(show.formattedStartTime)
                    .font(.system(size: 9.5, weight: (isSelected || isLive) ? .bold : .medium, design: .monospaced))
                    .foregroundStyle(isSelected ? Theme.Colors.statusError : (isLive ? Theme.Colors.accentLive : Theme.Colors.textTertiary))
                    .monospacedDigit()
                    .lineLimit(1)
            }

            Spacer(minLength: 0)
        }
        .padding(.leading, horizontalPadding + titleInset)
        .padding(.trailing, horizontalPadding)
        .padding(.vertical, 6)
        .frame(width: width, height: height, alignment: .topLeading)
        .background(
            isSelected
                ? Theme.Colors.statusError.opacity(0.18)
                : (isLive ? Theme.Colors.accentLive.opacity(0.14) : Theme.Colors.surfaceElevated),
            in: RoundedRectangle(cornerRadius: 7, style: .continuous)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .strokeBorder(
                    isSelected
                        ? Theme.Colors.statusError
                        : (isLive ? Theme.Colors.accentLive.opacity(0.55) : Theme.Colors.borderSubtle),
                    lineWidth: isSelected ? 2.5 : (isLive ? 1 : 0.5)
                )
        )
        .shadow(color: isSelected ? Theme.Colors.statusError.opacity(0.4) : .clear, radius: 4, x: 0, y: 0)
        .contentShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(channelName), \(show.title), \(show.formattedTimeRange)\(isSelected ? ", ausgewählt" : "")")
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

@MainActor
private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
    Haptics.shared.impact(style)
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
