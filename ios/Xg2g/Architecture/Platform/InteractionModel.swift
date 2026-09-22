// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Defines how the user physically interacts with the app.
///
/// Rather than scattering `#if os(...)` throughout views, components consult the
/// interaction model to adapt their layout, hit targets, and navigation flow.
enum InteractionModel: String, Sendable, Equatable, CaseIterable {
    /// Compact touch display (iPhone, iPod touch).
    case touchCompact

    /// Regular touch display (iPad primary interaction model; may support hover and hardware-keyboard capabilities).
    case touchRegular

    /// Directional focus engine and remote control (Apple TV, Siri Remote).
    case focusRemote

    /// Pointer (mouse/trackpad) and hardware keyboard (Mac Catalyst, Mac-hosted Apple client).
    case pointerKeyboard
}
