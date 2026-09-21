// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - iPadOS Sidebar

struct IPadSidebar: View {

    @Bindable var model: AppModel

    var body: some View {
        List {
            Section("Mediathek") {
                ForEach(Tab.navigationCases) { tab in
                    let isSelected = model.selectedTab == tab
                    Button {
                        triggerHaptic(.light)
                        model.selectedTab = tab
                    } label: {
                        HStack(spacing: 12) {
                            SettingsIconBadge(
                                systemName: tab.systemImage,
                                backgroundColor: isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated
                            )

                            Text(tab.rawValue)
                                .font(.app(size: 15, weight: isSelected ? .bold : .medium))
                                .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                            Spacer()

                            if isSelected {
                                Image(systemName: "chevron.right")
                                    .font(.app(size: 11, weight: .bold))
                                    .foregroundStyle(Theme.Colors.accentAction)
                            }
                        }
                        .padding(.vertical, 2)
                        .padding(.horizontal, 4)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .modifier(TabShortcutModifier(character: tab.shortcutCharacter))
                    .appHoverEffect(.highlight)
                    .listRowBackground(
                        isSelected
                            ? RoundedRectangle(cornerRadius: 10, style: .continuous)
                                .fill(Theme.Gradients.sidebarActiveSelection)
                                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.Colors.accentAction.opacity(0.3), lineWidth: 1))
                            : nil
                    )
                }
            }

            if (model.selectedTab == .liveTV || model.selectedTab == .guide) && !model.bouquets.isEmpty {
                Section("Bouquets & Sendergruppen") {
                    // Alle Sender
                    let isAllSelected = model.selectedBouquet == nil
                    Button {
                        triggerHaptic(.light)
                        Task { await model.selectBouquet(nil) }
                    } label: {
                        HStack(spacing: 10) {
                            Image(systemName: "tv.circle.fill")
                                .font(.app(size: 18))
                                .foregroundStyle(isAllSelected ? Theme.Colors.accentAction : Theme.Colors.textTertiary)

                            Text("Alle Sender")
                                .font(.app(size: 14, weight: isAllSelected ? .bold : .medium))
                                .foregroundStyle(isAllSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                            Spacer()

                            Text("\(model.channels.count)")
                                .font(.app(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 2)
                                .background(Theme.Colors.surfaceElevated, in: Capsule())
                        }
                        .padding(.vertical, 2)
                    }
                    .buttonStyle(.plain)
                    .appHoverEffect(.highlight)

                    // Favoriten
                    if !model.favoriteChannelIDs.isEmpty {
                        let isFavSelected = model.selectedBouquet?.id == AppModel.favoritesBouquetID
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(ChannelBouquet(id: AppModel.favoritesBouquetID, name: "Favoriten")) }
                        } label: {
                            HStack(spacing: 10) {
                                Image(systemName: "star.circle.fill")
                                    .font(.app(size: 18))
                                    .foregroundStyle(Theme.Colors.accentLive)

                                Text("Favoriten")
                                    .font(.app(size: 14, weight: isFavSelected ? .bold : .medium))
                                    .foregroundStyle(isFavSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)

                                Spacer()

                                Text("\(model.favoriteChannelIDs.count)")
                                    .font(.app(size: 11, weight: .semibold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                                    .padding(.horizontal, 7)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                            }
                            .padding(.vertical, 2)
                        }
                        .buttonStyle(.plain)
                        .appHoverEffect(.highlight)
                    }

                    // Bouquets
                    ForEach(model.bouquets) { bouquet in
                        let isSelected = model.selectedBouquet?.id == bouquet.id
                        Button {
                            triggerHaptic(.light)
                            Task { await model.selectBouquet(bouquet) }
                        } label: {
                            HStack(spacing: 10) {
                                Image(systemName: "folder.fill")
                                    .font(.app(size: 16))
                                    .foregroundStyle(isSelected ? Theme.Colors.accentAction : Theme.Colors.textTertiary)

                                Text(bouquet.name)
                                    .font(.app(size: 14, weight: isSelected ? .bold : .medium))
                                    .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                    .lineLimit(1)

                                Spacer()

                                if bouquet.servicesCount > 0 {
                                    Text("\(bouquet.servicesCount)")
                                        .font(.app(size: 11, weight: .semibold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                        .padding(.horizontal, 7)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                                }
                            }
                            .padding(.vertical, 2)
                        }
                        .buttonStyle(.plain)
                        .appHoverEffect(.highlight)
                    }
                }
            }

            Section {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 6) {
                        PulsingLiveDot(size: 6)
                        Text(model.serverURLString)
                            .font(.app(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    }
                    Text("xg2g Broadcast System • 2026")
                        .font(.app(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textDisabled)
                }
                .padding(10)
                .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                .padding(.vertical, 2)
            }
            .listRowBackground(Color.clear)
        }
#if !os(tvOS)
        .listStyle(.sidebar)
        .scrollContentBackground(.hidden)
#endif
        .background(Theme.Colors.bgBase.ignoresSafeArea())
        .navigationSplitViewColumnWidth(min: 270, ideal: 300, max: 360)
        .navigationTitle("xg2g TV")
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}

private struct TabShortcutModifier: ViewModifier {
    let character: Character?

    func body(content: Content) -> some View {
        #if os(iOS)
        if let character {
            content.keyboardShortcut(KeyEquivalent(character), modifiers: .command)
        } else {
            content
        }
        #else
        content
        #endif
    }
}
