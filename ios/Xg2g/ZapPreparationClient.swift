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
struct ZapPreparation: Decodable, Sendable {
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

/// Talks to the backend's preparation endpoints.
///
/// Four operations and no state of its own: start a preparation, ask what became of it,
/// take it, abandon it. Which of them a channel change performs, and in what order, is
/// the coordinator's business.
struct ZapPreparationClient: Sendable {
    private let api: any APIClient
    private let clientID: String

    init(api: any APIClient, clientID: String) {
        self.api = api
        self.clientID = clientID
    }

    private static let basePath = "api/v3/stream/prepare"

    /// Asks the backend to warm a channel beside the one playing.
    func start(serviceRef: String, zapID: String) async throws -> ZapPreparation {
        try await api.send(APIRequest<ZapPreparation>(
            method: .post,
            path: Self.basePath,
            query: [URLQueryItem(name: "sref", value: serviceRef)],
            headers: headers(zapID: zapID)
        ))
    }

    /// Asks what became of one.
    func status(_ preparationID: String, zapID: String) async throws -> ZapPreparation {
        try await api.send(APIRequest<ZapPreparation>(
            method: .get,
            path: "\(Self.basePath)/\(preparationID)",
            headers: headers(zapID: zapID)
        ))
    }

    /// Takes it, naming the generation that was proven ready.
    func commit(_ preparationID: String, generation: UInt64, zapID: String) async throws -> ZapPreparation {
        try await api.send(APIRequest<ZapPreparation>(
            method: .post,
            path: "\(Self.basePath)/\(preparationID)/commit",
            query: [URLQueryItem(name: "generation", value: String(generation))],
            headers: headers(zapID: zapID)
        ))
    }

    /// Abandons it. Idempotent on the server, so a client cleaning up never has to care
    /// whether it won the race.
    func cancel(_ preparationID: String, zapID: String) async {
        _ = try? await api.send(APIRequest<ZapPreparation>(
            method: .delete,
            path: "\(Self.basePath)/\(preparationID)",
            headers: headers(zapID: zapID)
        ))
    }

    private func headers(zapID: String) -> [String: String] {
        [
            "X-Xg2g-Client-Id": clientID,
            "X-Xg2g-Zap-Id": zapID,
        ]
    }
}
