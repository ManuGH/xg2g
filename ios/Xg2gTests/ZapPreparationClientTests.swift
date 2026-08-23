// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// The URLs this client actually produces.
///
/// Written against the client rather than against a hand-written path, because a
/// test that repeats the path it is checking proves only that two copies agree. The
/// first version of this client spelled the API base into its own paths and asked
/// for /api/v3/api/v3/stream/prepare; every request 404'd, which from the client
/// side is indistinguishable from an endpoint that was never deployed, and cost an
/// evening of looking at the wrong side of the wire.
/// Serialised, and on its own transport.
///
/// The shared stub in APIClientTests records the last request in static state, so two
/// suites using it concurrently read each other's requests — which is how this suite
/// first "failed" with a DELETE where it had sent a POST. It records into its own.
@Suite(.serialized)
struct ZapPreparationClientTests {

    private func client() throws -> (ZapPreparationClient, () -> URLRequest?) {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PrepareStubURLProtocol.self]
        PrepareStubURLProtocol.reset()
        let api = HTTPAPIClient(
            address: try ServerAddressParser.parseTrusted("http://example.test:8089/"),
            session: URLSession(configuration: config)
        )
        return (ZapPreparationClient(api: api, clientID: "sterling-test"), { PrepareStubURLProtocol.lastRequest })
    }

    @Test func startAsksTheDeploymentsPrepareEndpoint() async throws {
        let (client, lastRequest) = try client()
        _ = try await client.start(serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:", zapID: "z1")

        let url = try #require(lastRequest()?.url)
        #expect(url.path == "/api/v3/stream/prepare", "got \(url.path)")
        #expect(url.query?.contains("sref=") == true)
        #expect(lastRequest()?.httpMethod == "POST")
        #expect(lastRequest()?.value(forHTTPHeaderField: "X-Xg2g-Client-Id") == "sterling-test")
        #expect(lastRequest()?.value(forHTTPHeaderField: "X-Xg2g-Zap-Id") == "z1")
        // Every unsafe method needs it; the backend refuses the request otherwise.
        #expect(lastRequest()?.value(forHTTPHeaderField: "Origin")?.isEmpty == false)
    }

    @Test func statusCommitAndCancelAddressOnePreparation() async throws {
        let (client, lastRequest) = try client()

        _ = try await client.status("p1", zapID: "z1")
        #expect(try #require(lastRequest()?.url).path == "/api/v3/stream/prepare/p1")
        #expect(lastRequest()?.httpMethod == "GET")

        _ = try await client.commit("p1", generation: 7, zapID: "z1")
        let commitURL = try #require(lastRequest()?.url)
        #expect(commitURL.path == "/api/v3/stream/prepare/p1/commit")
        #expect(commitURL.query?.contains("generation=7") == true)
        #expect(lastRequest()?.httpMethod == "POST")

        await client.cancel("p1", zapID: "z1")
        #expect(try #require(lastRequest()?.url).path == "/api/v3/stream/prepare/p1")
        #expect(lastRequest()?.httpMethod == "DELETE")
    }

    /// A failed preparation has to be able to say what it was waiting on.
    @Test func aFailureCarriesItsOutstandingCriteria() throws {
        let json = Data(#"""
        {"preparationId":"p1","state":"failed","outcome":"timeout",
         "pending":{"random_access":"no entry point yet","descrambled":"no clear access unit"}}
        """#.utf8)
        let decoded = try JSONDecoder().decode(ZapPreparation.self, from: json)

        #expect(decoded.isSettled)
        #expect(decoded.parsedState == .failed)
        let summary = decoded.failureSummary
        #expect(summary.contains("random_access"))
        #expect(summary.contains("no entry point yet"))
    }

    @Test func aPendingPreparationIsNotSettled() throws {
        let json = Data(#"{"preparationId":"p1","state":"pending"}"#.utf8)
        let decoded = try JSONDecoder().decode(ZapPreparation.self, from: json)
        #expect(!decoded.isSettled)
        #expect(decoded.generation == nil)
    }
}


/// A transport used by this suite alone, so it cannot read another suite's requests.
final class PrepareStubURLProtocol: URLProtocol {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var _lastRequest: URLRequest?

    static var lastRequest: URLRequest? {
        lock.lock(); defer { lock.unlock() }; return _lastRequest
    }

    static func reset() {
        lock.lock(); defer { lock.unlock() }; _lastRequest = nil
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.lock()
        Self._lastRequest = request
        Self.lock.unlock()

        let response = HTTPURLResponse(
            url: request.url ?? URL(string: "http://example.test")!,
            statusCode: 202,
            httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"]
        )!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(#"{"preparationId":"p1","state":"pending"}"#.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
