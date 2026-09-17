// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

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

    // Relative to the deployment's API base, which already carries /api/v3/.
    // Spelling it out here produced /api/v3/api/v3/stream/prepare and a 404 that
    // read, from the client, exactly like an endpoint that did not exist.
    private static let basePath = "stream/prepare"

    /// Asks the backend to warm a channel beside the one playing.
    func start(serviceRef: String, zapID: String) async throws -> ZapPreparation {
        let response: Xg2gContract.ZapPreparationResponse = try await api.send(APIRequest(
            method: .post,
            path: Self.basePath,
            query: [URLQueryItem(name: "sref", value: serviceRef)],
            headers: headers(zapID: zapID)
        ))
        return response.toDomain()
    }

    /// Asks what became of one.
    func status(_ preparationID: String, zapID: String) async throws -> ZapPreparation {
        let response: Xg2gContract.ZapPreparationResponse = try await api.send(APIRequest(
            method: .get,
            path: "\(Self.basePath)/\(preparationID)",
            headers: headers(zapID: zapID)
        ))
        return response.toDomain()
    }

    /// Takes it, naming the generation that was proven ready.
    func commit(_ preparationID: String, generation: UInt64, zapID: String) async throws -> ZapPreparation {
        let response: Xg2gContract.ZapPreparationResponse = try await api.send(APIRequest(
            method: .post,
            path: "\(Self.basePath)/\(preparationID)/commit",
            query: [URLQueryItem(name: "generation", value: String(generation))],
            headers: headers(zapID: zapID)
        ))
        return response.toDomain()
    }

    /// Abandons it. Idempotent on the server, so a client cleaning up never has to care
    /// whether it won the race.
    func cancel(_ preparationID: String, zapID: String) async {
        let response: Xg2gContract.ZapPreparationResponse? = try? await api.send(APIRequest(
            method: .delete,
            path: "\(Self.basePath)/\(preparationID)",
            headers: headers(zapID: zapID)
        ))
        _ = response
    }

    private func headers(zapID: String) -> [String: String] {
        [
            "X-Xg2g-Client-Id": clientID,
            "X-Xg2g-Zap-Id": zapID,
        ]
    }
}
