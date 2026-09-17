// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// The backend's view of one channel change in progress.
///
/// Mirrors what `/api/v3/stream/prepare` answers. Every field the server sends is kept,
/// including the ones only a failure uses: a preparation that never became ready has to
/// be able to say which condition it was still waiting on, or the client can only report
/// that something did not work.
struct ZapPreparation: Sendable {
    let preparationId: String
    let zapId: String?
    let serviceRef: String?
    let state: String
    let outcome: String?
    /// The stream proven ready. A commit has to quote it back, so the server can refuse
    /// one aimed at a stream that has since been replaced.
    let generation: UInt64?
    let readyAfterMs: Int?
    /// Which readiness criteria were still outstanding, and why.
    let pending: [String: String]?
    let detail: String?

    enum State: String {
        case pending, ready, failed, cancelled, committed
    }

    var parsedState: State? { State(rawValue: state) }

    /// Whether this preparation can still become ready.
    var isSettled: Bool {
        switch parsedState {
        case .ready, .failed, .cancelled, .committed: return true
        case .pending, nil: return false
        }
    }

    /// Whether the preparation was rejected strictly because no tuner / capacity was available.
    var isAdmissionDenied: Bool {
        outcome == "admission_denied"
    }

    /// Why it will not become ready, in a form worth showing.
    var failureSummary: String {
        if let detail, !detail.isEmpty { return detail }
        if let pending, !pending.isEmpty {
            return pending.sorted(by: { $0.key < $1.key })
                .map { "\($0.key): \($0.value)" }
                .joined(separator: "; ")
        }
        return outcome ?? state
    }
}
