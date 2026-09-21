// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

extension Font {

    /// The app's fixed-size font, sized for the platform.
    ///
    /// The shared views were drawn for a phone and carry their sizes as
    /// literals, most of them between 9 and 15 points. A television renders
    /// a point as a pixel at 1080p, and the same label that reads on a phone
    /// at arm's length is a smudge from the sofa, next to system chrome
    /// (search field, keyboard, tab bar) that tvOS sizes for the room.
    ///
    /// On iPhone and iPad this is `Font.system(size:weight:design:)`. On tvOS
    /// the phone size is quantised onto a television scale: a screen then
    /// shows a handful of sizes with the hierarchy carried by weight, the way
    /// TV apps do it. The steps follow what streaming apps use at 1080p
    /// (metadata 18–20, row titles 24, headings 28–34), not the tvOS system
    /// scale, whose 29-point body reads as large print next to them.
    static func app(size: CGFloat, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        .system(size: tvSize(for: size), weight: weight, design: design)
    }

    private static func tvSize(for phoneSize: CGFloat) -> CGFloat {
#if os(tvOS)
        switch phoneSize {
        case ..<10: return 18
        case ..<12: return 20
        case ..<14: return 22
        case ..<16: return 24
        case ..<20: return 28
        case ..<30: return 34
        case ..<50: return 44
        case ..<100: return 60
        default: return phoneSize
        }
#else
        return phoneSize
#endif
    }
}
