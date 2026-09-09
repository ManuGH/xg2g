// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct TimeFilterPill: View {
    let filter: AppModel.TimeFilter
    let isSelected: Bool
    let onSelect: () -> Void

    var body: some View {
        Button {
            onSelect()
        } label: {
            HStack(spacing: 6) {
                if filter == .now {
                    PulsingLiveDot(size: 6)
                } else {
                    Image(systemName: filter.icon)
                        .font(.system(size: 11))
                        .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.accentAction)
                }

                Text(filter.label)
                    .font(.system(size: 13, weight: isSelected ? .bold : .medium))
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(
                isSelected ? Theme.Colors.accentLive : Theme.Colors.surfaceElevated.opacity(0.85),
                in: Capsule()
            )
            .foregroundStyle(isSelected ? Theme.Colors.bgBase : Theme.Colors.textPrimary)
            .overlay {
                if !isSelected {
                    Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                }
            }
        }
        .buttonStyle(.plain)
    }
}
