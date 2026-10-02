// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Canonical target presented by the playback pipeline.
enum PlaybackTarget: Equatable, Sendable {
    case live(Channel)
    case recording(Recording, startPosition: Double?)
    case offline(OfflineRecording)

    var title: String {
        switch self {
        case .live(let channel): return channel.name
        case .recording(let rec, _): return rec.title
        case .offline(let offline): return offline.title
        }
    }

    var isLive: Bool {
        if case .live = self { return true }
        return false
    }
}
