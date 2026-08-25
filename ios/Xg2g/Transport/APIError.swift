// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// RFC 7807 problem document, as the API actually defines it.
///
/// `type`, `title`, `status` and `requestId` are required by the schema; the
/// rest is optional. `requestId` is carried through every error because it is
/// the only thing that correlates a client-side failure with a server log line.
struct ProblemDetails: Equatable, Sendable, Decodable {
    let type: String
    let title: String
    let status: Int
    let requestId: String
    let code: String?
    let detail: String?
    let instance: String?
}

/// Why no response came back at all.
enum TransportFailure: Equatable, Sendable {
    case offline
    case timedOut
    case cannotConnect
    case tls
    case cancelled
    case other(code: Int)
}

/// A 2xx response whose body is not what the endpoint promised.
///
/// This is its own category on purpose. The Android client requested the API
/// through the Web UI path and got the SPA's HTML back **with status 200** —
/// neither a transport failure nor an HTTP error, and invisible if everything
/// collapses into a generic "network error". Naming it makes that class of bug
/// diagnosable from a single log line.
struct UnexpectedPayload: Equatable, Sendable {
    let url: URL
    let status: Int
    let contentType: String?
    /// Truncated; enough to recognise an HTML page or a proxy notice.
    let bodyPreview: String
    let expected: String
}

enum APIError: Error, Equatable, Sendable {
    /// Nothing came back: DNS, TLS, timeout, offline, cancelled.
    case transport(TransportFailure)

    /// The API answered with a well-formed problem document.
    case problem(ProblemDetails)

    /// An HTTP error status without a usable problem document — typically a
    /// proxy or gateway answering instead of the API.
    case http(status: Int, contentType: String?, bodyPreview: String)

    /// HTTP succeeded but the payload was not the promised shape.
    case unexpectedPayload(UnexpectedPayload)

    /// The request could not be built, or resolved outside the configured
    /// deployment. A programming error, surfaced rather than silently sent.
    case invalidEndpoint(path: String)

    /// The correlation id, when the server supplied one.
    var requestID: String? {
        if case .problem(let problem) = self { return problem.requestId }
        return nil
    }

    /// True when retrying the identical request could plausibly succeed.
    var isRetryable: Bool {
        switch self {
        case .transport(let failure):
            return failure != .cancelled
        case .problem(let problem):
            return problem.status == 429 || (500...599).contains(problem.status)
        case .http(let status, _, _):
            return status == 429 || (500...599).contains(status)
        case .unexpectedPayload, .invalidEndpoint:
            // A wrong body or a wrong URL will be just as wrong next time.
            return false
        }
    }
}

extension TransportFailure {
    init(urlError: URLError) {
        switch urlError.code {
        case .notConnectedToInternet, .dataNotAllowed, .internationalRoamingOff:
            self = .offline
        case .timedOut:
            self = .timedOut
        case .cannotConnectToHost, .cannotFindHost, .dnsLookupFailed, .networkConnectionLost:
            self = .cannotConnect
        case .secureConnectionFailed,
             .serverCertificateUntrusted,
             .serverCertificateHasBadDate,
             .serverCertificateNotYetValid,
             .serverCertificateHasUnknownRoot,
             .clientCertificateRejected,
             .clientCertificateRequired,
             .appTransportSecurityRequiresSecureConnection:
            self = .tls
        case .cancelled:
            self = .cancelled
        default:
            self = .other(code: urlError.errorCode)
        }
    }
}

extension APIError {
    /// What actually went wrong, in one line fit for a log.
    ///
    /// `localizedDescription` on a plain Swift error renders as "The operation
    /// couldn't be completed. (Xg2g.APIError error 1.)", which names neither the
    /// status, the reason, nor the request id the server logged it under. A channel
    /// change that fails has to say why — a failure nobody can read is the thing this
    /// player was rebuilt to stop producing.
    var diagnosticDescription: String {
        switch self {
        case .transport(let failure):
            switch failure {
            case .offline: return "no network"
            case .timedOut: return "request timed out"
            case .cannotConnect: return "cannot reach the server"
            case .tls: return "TLS failed"
            case .cancelled: return "cancelled"
            case .other(let code): return "network error \(code)"
            }
        case .problem(let p):
            var line = "HTTP \(p.status)"
            if let code = p.code, !code.isEmpty { line += " \(code)" }
            if let detail = p.detail, !detail.isEmpty { line += ": \(detail)" }
            if !p.requestId.isEmpty { line += " [request \(p.requestId)]" }
            return line
        case .http(let status, let contentType, let bodyPreview):
            return "HTTP \(status) (\(contentType ?? "no content type")): \(bodyPreview.prefix(120))"
        case .unexpectedPayload(let payload):
            return "HTTP \(payload.status) from \(payload.url.path) was not \(payload.expected): \(payload.bodyPreview.prefix(120))"
        case .invalidEndpoint(let path):
            return "invalid endpoint \(path)"
        }
    }
}
