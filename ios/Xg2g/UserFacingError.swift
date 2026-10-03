// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A structured error designed for localized user presentation and diagnostic traceability.
///
/// It preserves structured properties (code, requestId, severity, retryability) until
/// presentation, and separates user-facing text from technical diagnostic logs.
public struct UserFacingError: Equatable, Sendable {

    public enum Severity: String, Equatable, Sendable {
        case info
        case warning
        case error
        case critical
    }

    /// User-facing, localized headline.
    public let title: LocalizedStringResource

    /// Optional user-facing, localized explanatory detail or recovery guidance.
    public let detail: LocalizedStringResource?

    /// Whether retrying the action could plausibly succeed.
    public let isRetryable: Bool

    /// Severity level determining icon or visual prominence.
    public let severity: Severity

    /// Optional problem code (RFC 7807 code or internal identifier).
    public let code: String?

    /// The correlation request ID from the server, when available.
    public let requestId: String?

    /// Technical diagnostic details for logs and debug views only (never user copy).
    public let diagnosticLog: String?

    public init(
        title: LocalizedStringResource,
        detail: LocalizedStringResource? = nil,
        isRetryable: Bool = true,
        severity: Severity = .error,
        code: String? = nil,
        requestId: String? = nil,
        diagnosticLog: String? = nil
    ) {
        self.title = title
        self.detail = detail
        self.isRetryable = isRetryable
        self.severity = severity
        self.code = code
        self.requestId = requestId
        self.diagnosticLog = diagnosticLog
    }

    /// Convenience formatted message for views that expect a single string (e.g. toasts, legacy banners).
    public var localizedMessage: String {
        let titleStr = String(localized: title)
        if let detail {
            let detailStr = String(localized: detail)
            return "\(titleStr): \(detailStr)"
        }
        return titleStr
    }

    /// Formatted diagnostic summary for logging.
    public var diagnosticSummary: String {
        var parts: [String] = []
        if let code { parts.append("code=\(code)") }
        if let requestId { parts.append("requestId=\(requestId)") }
        if let diagnosticLog { parts.append("diag=\(diagnosticLog)") }
        return parts.joined(separator: " ")
    }
}

/// Classifies system, network, and domain errors into structured `UserFacingError` representations.
public enum ErrorClassifier {

    /// Classifies an error into a user-facing error model.
    ///
    /// Returns `nil` when the error represents normal cancellation (user navigation, superseded tasks),
    /// ensuring no spurious error banners or toasts flash on screen.
    public static func classify(_ error: any Error, context: String? = nil) -> UserFacingError? {
        // 1. Explicit cancellation suppression
        if error is CancellationError {
            return nil
        }
        if let urlError = error as? URLError, urlError.code == .cancelled {
            return nil
        }
        let nsError = error as NSError
        if nsError.domain == NSURLErrorDomain && nsError.code == NSURLErrorCancelled {
            return nil
        }

        // 2. Structured APIError
        if let apiError = error as? APIError {
            return classifyAPIError(apiError)
        }

        // 3. PlaybackCoordinator.Failure
        if let pbFailure = error as? PlaybackCoordinator.Failure {
            return classifyPlaybackFailure(pbFailure)
        }

        // 4. SessionCoordinator.Failure
        if case SessionCoordinator.Failure.reauthenticationRequired = error {
            return UserFacingError(
                title: LocalizedStringResource("Device Must Be Paired Again"),
                detail: LocalizedStringResource("This device holds expired credentials. Please pair again."),
                isRetryable: false,
                severity: .warning,
                code: "DEVICE_REAUTH_REQUIRED"
            )
        }

        // 5. Generic fallback
        return UserFacingError(
            title: LocalizedStringResource("Request Failed"),
            detail: LocalizedStringResource("An unexpected error occurred."),
            isRetryable: true,
            severity: .error,
            diagnosticLog: error.localizedDescription
        )
    }

    /// Classifies backend zap preparation failures into structured user-facing errors.
    static func classifyZapPreparation(_ preparation: ZapPreparation) -> UserFacingError {
        let outcome = preparation.outcome ?? preparation.state
        let diagnostic = preparation.failureSummary

        switch outcome {
        case "admission_denied":
            return UserFacingError(
                title: LocalizedStringResource("All Tuners Occupied"),
                detail: LocalizedStringResource("No free receiver tuners available right now."),
                isRetryable: true,
                severity: .warning,
                code: "ADMISSION_NO_TUNERS",
                diagnosticLog: diagnostic
            )

        case "tuning_timeout":
            return UserFacingError(
                title: LocalizedStringResource("Channel Tuning Timed Out"),
                detail: LocalizedStringResource("The receiver took too long to tune the channel."),
                isRetryable: true,
                severity: .error,
                code: "TUNING_TIMEOUT",
                diagnosticLog: diagnostic
            )

        case "scrambled":
            return UserFacingError(
                title: LocalizedStringResource("Channel Scrambled"),
                detail: LocalizedStringResource("This channel is encrypted and cannot be decoded."),
                isRetryable: false,
                severity: .error,
                code: "CHANNEL_SCRAMBLED",
                diagnosticLog: diagnostic
            )

        case "no_data", "no_pat_pmt":
            return UserFacingError(
                title: LocalizedStringResource("No Broadcast Signal"),
                detail: LocalizedStringResource("The receiver received no data for this channel."),
                isRetryable: true,
                severity: .error,
                code: "NO_BROADCAST_SIGNAL",
                diagnosticLog: diagnostic
            )

        default:
            return UserFacingError(
                title: LocalizedStringResource("Playback Error"),
                detail: LocalizedStringResource("The stream could not be started."),
                isRetryable: true,
                severity: .error,
                code: outcome,
                diagnosticLog: diagnostic
            )
        }
    }

    private static func classifyAPIError(_ apiError: APIError) -> UserFacingError? {
        switch apiError {
        case .transport(let transport):
            return classifyTransportFailure(transport)

        case .problem(let problem):
            return classifyProblemDetails(problem)

        case .http(let status, _, let bodyPreview):
            return classifyHTTPStatus(status, diagnosticPreview: bodyPreview)

        case .unexpectedPayload(let payload):
            return UserFacingError(
                title: LocalizedStringResource("Unexpected Server Response"),
                detail: LocalizedStringResource("The server response was unexpected."),
                isRetryable: false,
                severity: .error,
                diagnosticLog: "status \(payload.status), expected \(payload.expected)"
            )

        case .invalidEndpoint(let path):
            return UserFacingError(
                title: LocalizedStringResource("Invalid API Endpoint"),
                detail: LocalizedStringResource("The requested endpoint is not available."),
                isRetryable: false,
                severity: .error,
                diagnosticLog: "path \(path)"
            )
        }
    }

    private static func classifyTransportFailure(_ transport: TransportFailure) -> UserFacingError? {
        switch transport {
        case .cancelled:
            return nil

        case .offline:
            return UserFacingError(
                title: LocalizedStringResource("No Internet Connection"),
                detail: LocalizedStringResource("Please check your network connection."),
                isRetryable: true,
                severity: .warning
            )

        case .timedOut:
            return UserFacingError(
                title: LocalizedStringResource("Connection Timed Out"),
                detail: LocalizedStringResource("The server took too long to respond."),
                isRetryable: true,
                severity: .warning
            )

        case .cannotConnect:
            return UserFacingError(
                title: LocalizedStringResource("Cannot Reach Server"),
                detail: LocalizedStringResource("Please check the server address and connectivity."),
                isRetryable: true,
                severity: .error
            )

        case .tls:
            return UserFacingError(
                title: LocalizedStringResource("Secure Connection Failed"),
                detail: LocalizedStringResource("A secure TLS connection could not be established."),
                isRetryable: false,
                severity: .error
            )

        case .other(let code):
            return UserFacingError(
                title: LocalizedStringResource("Network Error"),
                detail: LocalizedStringResource("A network error occurred."),
                isRetryable: true,
                severity: .error,
                diagnosticLog: "transport code \(code)"
            )
        }
    }

    private static func classifyProblemDetails(_ problem: ProblemDetails) -> UserFacingError {
        let code = problem.code ?? ""
        let reqId = problem.requestId.isEmpty ? nil : problem.requestId

        switch code {
        case SessionCoordinator.deviceReauthRequiredCode, "DEVICE_REAUTH_REQUIRED":
            return UserFacingError(
                title: LocalizedStringResource("Device Must Be Paired Again"),
                detail: LocalizedStringResource("This device holds expired credentials. Please pair again."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "UNAUTHORIZED":
            return UserFacingError(
                title: LocalizedStringResource("Authentication Required"),
                detail: LocalizedStringResource("Please sign in or pair again to continue."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "FORBIDDEN":
            return UserFacingError(
                title: LocalizedStringResource("Access Denied"),
                detail: LocalizedStringResource("You do not have permission for this action."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "PAIRING_EXPIRED", "pairing/expired":
            return UserFacingError(
                title: LocalizedStringResource("Pairing Code Expired"),
                detail: LocalizedStringResource("Please request a new pairing code."),
                isRetryable: false,
                severity: .info,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "PAIRING_CONSUMED", "pairing/consumed":
            return UserFacingError(
                title: LocalizedStringResource("Code Already Used"),
                detail: LocalizedStringResource("This code has already been claimed. Please request a new code."),
                isRetryable: false,
                severity: .info,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "PAIRING_REVOKED", "pairing/revoked":
            return UserFacingError(
                title: LocalizedStringResource("Pairing Denied"),
                detail: LocalizedStringResource("Pairing was denied in the admin console. Please request a new code."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "ADMISSION_NO_TUNERS":
            return UserFacingError(
                title: LocalizedStringResource("All Tuners Occupied"),
                detail: LocalizedStringResource("No free receiver tuners available right now."),
                isRetryable: true,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "ADMISSION_SESSIONS_FULL":
            return UserFacingError(
                title: LocalizedStringResource("Streaming Limit Reached"),
                detail: LocalizedStringResource("The maximum number of active streaming sessions has been reached."),
                isRetryable: true,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "ADMISSION_TRANSCODES_FULL":
            return UserFacingError(
                title: LocalizedStringResource("Transcoding Limit Reached"),
                detail: LocalizedStringResource("The server is currently processing the maximum number of transcodes."),
                isRetryable: true,
                severity: .warning,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "TRANSCODE_STALLED":
            return UserFacingError(
                title: LocalizedStringResource("Stream Stalled"),
                detail: LocalizedStringResource("Video processing stopped emitting data."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "TRANSCODE_START_TIMEOUT":
            return UserFacingError(
                title: LocalizedStringResource("Stream Start Timed Out"),
                detail: LocalizedStringResource("The stream did not start in time. Please try again."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "RECEIVER_UNREACHABLE", "dvr/receiver_unreachable":
            return UserFacingError(
                title: LocalizedStringResource("Receiver Unreachable"),
                detail: LocalizedStringResource("The TV receiver cannot be reached. Please check its connection."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "UPSTREAM_UNAVAILABLE":
            return UserFacingError(
                title: LocalizedStringResource("Receiver Unavailable"),
                detail: LocalizedStringResource("The TV receiver did not return stream data."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        case "live/scan_unavailable", "live/partial_truth", "live/missing_scan_truth":
            return UserFacingError(
                title: LocalizedStringResource("Stream Being Verified"),
                detail: LocalizedStringResource("Checking channel availability."),
                isRetryable: true,
                severity: .info,
                code: code,
                requestId: reqId,
                diagnosticLog: problem.detail
            )

        default:
            // Fallback by status code, without deriving receiver causes from generic errors
            return classifyHTTPStatus(problem.status, requestId: reqId, problemDetail: problem.detail, code: code)
        }
    }

    private static func classifyHTTPStatus(
        _ status: Int,
        requestId: String? = nil,
        diagnosticPreview: String? = nil,
        problemDetail: String? = nil,
        code: String? = nil
    ) -> UserFacingError {
        switch status {
        case 401:
            return UserFacingError(
                title: LocalizedStringResource("Authentication Required"),
                detail: LocalizedStringResource("Please sign in or pair again to continue."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )

        case 403:
            return UserFacingError(
                title: LocalizedStringResource("Access Denied"),
                detail: LocalizedStringResource("You do not have permission for this action."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )

        case 404:
            return UserFacingError(
                title: LocalizedStringResource("Not Found"),
                detail: LocalizedStringResource("The requested resource was not found."),
                isRetryable: false,
                severity: .warning,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )

        case 429:
            return UserFacingError(
                title: LocalizedStringResource("Too Many Requests"),
                detail: LocalizedStringResource("Please wait a moment before trying again."),
                isRetryable: true,
                severity: .warning,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )

        case 503:
            return UserFacingError(
                title: LocalizedStringResource("Service Unavailable"),
                detail: LocalizedStringResource("The server is temporarily unavailable."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )

        default:
            if status >= 500 {
                return UserFacingError(
                    title: LocalizedStringResource("Server Error"),
                    detail: LocalizedStringResource("An internal server error occurred."),
                    isRetryable: true,
                    severity: .error,
                    code: code,
                    requestId: requestId,
                    diagnosticLog: diagnosticPreview ?? problemDetail
                )
            }
            return UserFacingError(
                title: LocalizedStringResource("Request Failed"),
                detail: LocalizedStringResource("The request could not be completed."),
                isRetryable: true,
                severity: .error,
                code: code,
                requestId: requestId,
                diagnosticLog: diagnosticPreview ?? problemDetail
            )
        }
    }

    private static func classifyPlaybackFailure(_ failure: PlaybackCoordinator.Failure) -> UserFacingError {
        switch failure {
        case .noSessionCreated:
            return UserFacingError(
                title: LocalizedStringResource("Playback Failed"),
                detail: LocalizedStringResource("No playback session could be created."),
                isRetryable: true,
                severity: .error
            )

        case .unusableStreamURL:
            return UserFacingError(
                title: LocalizedStringResource("Playback Failed"),
                detail: LocalizedStringResource("Invalid stream URL."),
                isRetryable: false,
                severity: .error
            )

        case .ticketRefused:
            return UserFacingError(
                title: LocalizedStringResource("Playback Ticket Refused"),
                detail: LocalizedStringResource("The server refused playback authorization."),
                isRetryable: true,
                severity: .error
            )
        }
    }
}
