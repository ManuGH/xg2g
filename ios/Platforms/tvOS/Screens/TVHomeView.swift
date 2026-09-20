// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// The television's home screen: one hero, a few shelves.
///
/// Same `AppModel` as the phone hub, none of its layout. A phone hub packs
/// a dozen sections into a scrolling column; a television shows what is on
/// now, what is on tonight and what was recorded, each as a row of equal
/// cards, and leaves the rest to the tabs.
struct TVHomeView: View {

    @Bindable var model: AppModel
    @State private var recordConfirmationMessage: String?

    private typealias Pick = (channel: Channel, entry: NowNext.Entry)

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            ScrollView(.vertical, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 36) {
                    if let hero = spotlight {
                        TVHeroCard(
                            channel: hero.channel,
                            entry: hero.entry,
                            onPlay: { model.playingChannel = hero.channel },
                            onRecord: { record(hero.channel, hero.entry) }
                        )
                    }

                    let recent = recentPicks
                    if !recent.isEmpty {
                        TVShelf(title: "Zuletzt gesehen") {
                            ForEach(recent, id: \.channel.id) { pick in
                                programCard(pick)
                            }
                        }
                    }

                    let now = nowPicks
                    if !now.isEmpty {
                        TVShelf(title: "Jetzt läuft") {
                            ForEach(now, id: \.channel.id) { pick in
                                programCard(pick)
                            }
                        }
                    }

                    let prime = primeTimePicks
                    if !prime.isEmpty {
                        TVShelf(title: "Heute um 20:15") {
                            ForEach(prime, id: \.channel.id) { pick in
                                programCard(pick)
                            }
                        }
                    }

                    if !model.recordings.isEmpty {
                        TVShelf(title: "Aufnahmen") {
                            ForEach(model.recordings.prefix(12)) { recording in
                                TVRecordingCard(recording: recording) {
                                    model.playbackManager.play(recording: recording, startPosition: 0)
                                }
                            }
                        }
                    }

                    if spotlight == nil && model.channels.isEmpty {
                        ContentUnavailableView("Lade Sender…", systemImage: "tv")
                            .foregroundStyle(Theme.Colors.textSecondary)
                    }
                }
                .padding(.vertical, 20)
            }

            if let message = recordConfirmationMessage {
                VStack {
                    Spacer()
                    Label(message, systemImage: "checkmark.circle.fill")
                        .font(TVDesign.Font.body)
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .padding(.horizontal, 24)
                        .padding(.vertical, 14)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                        .padding(.bottom, 24)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
        }
    }

    private func programCard(_ pick: Pick) -> some View {
        TVProgramCard(
            channel: pick.channel,
            entry: pick.entry,
            onPlay: { model.playingChannel = pick.channel },
            onRecord: { record(pick.channel, pick.entry) }
        )
    }

    // MARK: - Picks

    private var pool: [Channel] {
        model.favoriteChannels.isEmpty ? Array(model.channels.prefix(12)) : model.favoriteChannels
    }

    private var spotlight: Pick? {
        for channel in pool {
            if let show = model.show(for: channel, at: .now) {
                return (channel, show)
            }
        }
        return nil
    }

    private var recentPicks: [Pick] {
        model.recentChannels.compactMap { channel in
            model.show(for: channel, at: .now).map { (channel, $0) }
        }
    }

    private var nowPicks: [Pick] {
        let heroID = spotlight?.channel.id
        return pool.compactMap { channel in
            guard channel.id != heroID else { return nil }
            return model.show(for: channel, at: .now).map { (channel, $0) }
        }
    }

    private var primeTimePicks: [Pick] {
        pool.compactMap { channel in
            model.show(for: channel, at: .primeTimeTonight).map { (channel, $0) }
        }
    }

    private func record(_ channel: Channel, _ entry: NowNext.Entry) {
        Task {
            let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
            guard ok else { return }
            withAnimation { recordConfirmationMessage = "„\(entry.title)“ programmiert" }
            try? await Task.sleep(for: .seconds(3))
            withAnimation { recordConfirmationMessage = nil }
        }
    }
}

// MARK: - Hero

struct TVHeroCard: View {
    let channel: Channel
    let entry: NowNext.Entry
    var onPlay: () -> Void
    var onRecord: () -> Void

    var body: some View {
        let now = Date.now
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 14) {
                ChannelLogo(url: channel.logoURL, name: channel.name, size: 56)
                Text(channel.name)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                Text("Live")
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.accentLive)
            }

            Text(entry.title)
                .font(TVDesign.Font.hero)
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(2)

            if let description = entry.description, !description.isEmpty {
                Text(description)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .lineLimit(2)
                    .frame(maxWidth: 1100, alignment: .leading)
            }

            HStack(spacing: 16) {
                Text(entry.formattedStartTime)
                TVProgressLine(progress: entry.progress(at: now) ?? 0)
                    .frame(width: 320)
                if let remaining = entry.remainingMinutes(at: now) {
                    Text("noch \(remaining) Min")
                }
                Text(entry.formattedEndTime)
            }
            .font(TVDesign.Font.meta)
            .foregroundStyle(Theme.Colors.textSecondary)

            HStack(spacing: 20) {
                Button(action: onPlay) {
                    Label("Jetzt ansehen", systemImage: "play.fill")
                        .font(TVDesign.Font.body)
                }
                Button(action: onRecord) {
                    Label("Aufnehmen", systemImage: "record.circle")
                        .font(TVDesign.Font.body)
                }
            }
            .padding(.top, 6)
        }
        .padding(36)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .fill(Theme.Gradients.cardSurface)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
        )
    }
}

// MARK: - Cards

struct TVProgramCard: View {
    let channel: Channel
    let entry: NowNext.Entry
    var onPlay: () -> Void
    var onRecord: () -> Void

    var body: some View {
        let now = Date.now
        let progress = entry.progress(at: now)
        Button(action: onPlay) {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 44)
                    Text(channel.name)
                        .font(TVDesign.Font.meta)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if let remaining = entry.remainingMinutes(at: now) {
                        Text("noch \(remaining) Min")
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.accentLive)
                    } else {
                        Text(entry.formattedStartTime)
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.textSecondary)
                    }
                }

                Spacer(minLength: 0)

                Text(entry.title)
                    .font(TVDesign.Font.body)
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)

                if let progress {
                    TVProgressLine(progress: progress)
                }
            }
            .padding(20)
            .frame(width: TVDesign.Layout.cardWidth, height: TVDesign.Layout.cardHeight)
        }
        .buttonStyle(TVCardButtonStyle())
        .contextMenu {
            Button("Live schauen", systemImage: "play.fill", action: onPlay)
            Button("„\(entry.title)“ aufnehmen", systemImage: "record.circle", action: onRecord)
        }
    }
}

struct TVRecordingCard: View {
    let recording: Recording
    var onPlay: () -> Void

    var body: some View {
        Button(action: onPlay) {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Image(systemName: "play.rectangle.fill")
                        .font(TVDesign.Font.body)
                        .foregroundStyle(Theme.Colors.accentAction)
                    Spacer(minLength: 0)
                    Text(recording.formattedDuration)
                        .font(TVDesign.Font.meta)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                Spacer(minLength: 0)
                Text(recording.title)
                    .font(TVDesign.Font.body)
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(recording.formattedDate)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
            }
            .padding(20)
            .frame(width: TVDesign.Layout.cardWidth, height: TVDesign.Layout.cardHeight)
        }
        .buttonStyle(TVCardButtonStyle())
    }
}
