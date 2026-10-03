# Architecture Specification: Bilingual Error Presentation & Playback Coordination Invariants

- **Status:** Approved Specification
- **Base Commit SHA:** `6139418afaba630e87c00b64de57977f7f22816b` (branch `feat/ios-string-catalog-foundation`, cut from `origin/main` at `2c6a4bae294b55b812b2f6f30f391365c928b978`)
- **Domain:** iOS & tvOS Client (`ios/Xg2g`), WebUI alignment (`apps/webui`), and Go Control Plane (`backend/internal/problemcode`)
- **Exclusion:** DVB and audio track normalization is explicitly out-of-scope and deferred to a dedicated downstream package.

---

## 1. Actual Error Producers & UI Consumers

### 1.1 Error Producers
1. **API Client & Transport Layer (`ios/Xg2g/Transport/APIClient.swift`, `APIError.swift`):**
   - Maps `URLError` to `TransportFailure`: `.offline`, `.timedOut`, `.cannotConnect`, `.tls`, `.cancelled`, `.other(code)`.
   - Decodes RFC 7807 problem documents into `ProblemDetails(type, title, status, requestId, code, detail, instance)`.
   - Yields structured `APIError`: `.problem`, `.http`, `.transport`, `.unexpectedPayload`, `.invalidEndpoint`.
2. **Go Backend Problem Registry (`backend/internal/problemcode/registry.go`):**
   - Single Source of Truth (SSOT) defining public and internal codes, HTTP statuses, retryability, and problem types.
   - Key categories: Authentication (`UNAUTHORIZED`, `DEVICE_REAUTH_REQUIRED`), Pairing (`PAIRING_EXPIRED`, `PAIRING_CONSUMED`, `PAIRING_REVOKED`), Admission (`ADMISSION_NO_TUNERS`, `ADMISSION_SESSIONS_FULL`), Transcode (`TRANSCODE_STALLED`, `TRANSCODE_START_TIMEOUT`), Receiver (`RECEIVER_UNREACHABLE`, `UPSTREAM_UNAVAILABLE`), and Live Media Truth (`live/scan_unavailable`, `live/partial_truth`).
3. **Zap Preparation Client (`ios/Xg2g/ZapPreparationClient.swift`):**
   - Yields `ZapPreparation` with `state`, `outcome`, `pending`, `detail`, `isSettled`, and `isAdmissionDenied` (`outcome == "admission_denied"`).
   - Generates `failureSummary` for preparation timeouts or rejections.
4. **Zap Coordinator (`ios/Xg2g/ZapCoordinator.swift`):**
   - Tracks channel change phases: `.idle`, `.warming`, `.buffering`, `.failed(serviceRef, reason)`.
   - Coordinates Make-before-Break and fallback transitions.
5. **Playback Coordinator & AVFoundation (`ios/Xg2g/PlaybackCoordinator.swift`, `PlayerScreen.swift`):**
   - `PlaybackCoordinator.Failure`: `.noSessionCreated`, `.unusableStreamURL`, `.ticketRefused`.
   - `NotificationCenter` publisher for `AVPlayerItemFailedToPlayToEndTime`.

### 1.2 UI Consumers
1. **`AppModel.swift` (`handle(_:)`, `beginPairing()`, `pairingStatus()`):**
   - Global view model managing connection state, pairing invitations, and global error banner presentation.
2. **`PlayerScreen.swift` (`failure: String?`):**
   - Fullscreen video playback error overlay with icon, message, and retry button.
3. **`RootView.swift`:**
   - Server connection form, pairing code input, expired/revoked pairing alerts, and retry action buttons.
4. **`TestTSPlayerScreen.swift` (`displayZapToast` via `coordinator.phase` observer):**
   - Native debug/zap HUD and player view observing `coordinator.phase`. On `.failed(serviceRef, reason)`, displays temporary toast HUD: `"\(channelName) konnte nicht geladen werden (\(reason))"`.

---

## 2. Preservation of Structured Errors & Diagnostic Channel Discipline

### 2.1 The Problem with Premature String Flattening
Currently, `AppModel.handle` immediately flattens rich `APIError` and `ProblemDetails` models into hardcoded German strings. This discards structured metadata (`code`, `requestId`, `isRetryable`, `severity`) before reaching views, produces hardcoded German messages on English devices, and confuses technical diagnostic codes with user messages.

### 2.2 Structured Error Presentation Model (`UserFacingError`)
We introduce a structured domain model preserving error fidelity until presentation:
```swift
public struct UserFacingError: Equatable, Sendable {
    public let title: LocalizedStringResource
    public let detail: LocalizedStringResource?
    public let isRetryable: Bool
    public let severity: Severity
    public let requestId: String?
    public let diagnosticLog: String?
    public let code: String?

    public enum Severity: String, Equatable, Sendable {
        case info, warning, error, critical
    }
}
```

### 2.3 Diagnostic Channel Discipline
- **User-Facing UI:** Renders only localized `title` and `detail`, plus contextual action buttons (e.g. "Retry", "Request New Code").
- **Diagnostic Payloads:**
  - Technical diagnostic strings (`requestId`, HTTP status, raw server details) are **never** dumped into normal user-facing text.
  - Diagnostics route exclusively to existing channels: `Logger(category: "playback")` and `TelemetryServer.shared.log(...)`.
  - Unrestricted forwarding of arbitrary raw payloads to telemetry sinks is strictly prohibited to avoid leaking private credentials or breaking data boundaries.

---

## 3. Localized Problem Codes, Transport, and Cancellation Classification

### 3.1 Strict Classification Rules
1. **Do Not Derive Receiver-Specific Causes from Generic Codes:**
   - Generic HTTP `500 INTERNAL_SERVER_ERROR` or generic `503 SERVICE_UNAVAILABLE` must be presented as a general service issue ("Service Unavailable" / "Dienst nicht verfügbar"), **never** guessing receiver-specific faults (e.g. "Receiver is offline") unless the explicit RFC 7807 code (`RECEIVER_UNREACHABLE`, `dvr/receiver_unreachable`) is present.
2. **Cancellation Suppression:**
   - `CancellationError`, `Task.isCancelled`, or `TransportFailure.cancelled` must return `nil` from classification.
   - Normal user transitions (closing a player, switching tabs, rapid zapping) must never flash transient error banners.
3. **Unknown or Missing Codes:**
   - Map gracefully via HTTP status fallback (401 -> Auth, 403 -> Access denied, 404 -> Not found, 5xx -> Server unavailable).
   - Preserve `requestId` in diagnostic logs to correlate with backend logs.

### 3.2 Canonical Classification Matrix

| Error Type / Code | HTTP | Retryable | Title (EN / DE) | Detail (EN / DE) |
| :--- | :--- | :--- | :--- | :--- |
| `DEVICE_REAUTH_REQUIRED` | 403 | No | Device Must Be Paired Again / Gerät muss erneut gekoppelt werden | This device holds expired credentials. / Dieses Gerät besitzt abgelaufene Zugangsdaten. |
| `UNAUTHORIZED` | 401 | No | Authentication Required / Authentifizierung erforderlich | Please sign in or pair again to continue. / Bitte erneut anmelden oder koppeln. |
| `FORBIDDEN` | 403 | No | Access Denied / Zugriff verweigert | You do not have permission for this action. / Keine Berechtigung für diese Aktion. |
| `PAIRING_EXPIRED` | 410 | No | Pairing Code Expired / Kopplungscode abgelaufen | Please request a new pairing code. / Bitte einen neuen Code anfordern. |
| `PAIRING_CONSUMED` | 410 | No | Code Already Used / Code bereits verwendet | This code has already been claimed. / Dieser Code wurde bereits verwendet. |
| `PAIRING_REVOKED` | 410 | No | Pairing Denied / Kopplung abgelehnt | Pairing was denied in the admin console. / Die Kopplung wurde abgelehnt. |
| `ADMISSION_NO_TUNERS` | 503 | Yes | All Tuners Occupied / Alle Tuner belegt | No free receiver tuners available. / Aktuell sind keine freien Tuner verfügbar. |
| `TRANSCODE_STALLED` | 503 | Yes | Stream Stalled / Stream stockt | Video processing stopped emitting data. / Die Videoverarbeitung liefert keine Daten mehr. |
| `RECEIVER_UNREACHABLE` | 502 | Yes | Receiver Unreachable / Receiver nicht erreichbar | The TV receiver cannot be reached. / Der TV-Receiver ist nicht erreichbar. |
| `UPSTREAM_UNAVAILABLE` | 502 | Yes | Receiver Unavailable / Receiver nicht verfügbar | The TV receiver did not return stream data. / Der TV-Receiver lieferte keine Daten. |
| Transport: `.offline` | — | Yes | No Internet Connection / Keine Internetverbindung | Check your network connection. / Bitte Netzwerkverbindung prüfen. |
| Transport: `.timedOut`| — | Yes | Connection Timed Out / Zeitüberschreitung | The server did not respond in time. / Der Server hat nicht rechtzeitig geantwortet. |
| Transport: `.cannotConnect`| — | Yes | Cannot Reach Server / Server nicht erreichbar | Check the server address and connectivity. / Server-Adresse und Verbindung prüfen. |
| Transport: `.tls` | — | No | Secure Connection Failed / TLS-Verbindung fehlgeschlagen | Certificate validation or TLS handshake failed. / Zertifikatsprüfung fehlgeschlagen. |
| Transport: `.cancelled` | — | — | *Suppressed (nil)* | *Suppressed (nil)* |

---

## 4. Playback Coordinator Regression Invariants

The `ZapCoordinator` enforces five explicit invariants:

### 4.1 Invariant 1: Initial Playback (Cold Start)
- **Condition:** No channel is currently playing (`playing == nil`).
- **Behavior:**
  - If backend preparation succeeds: warms -> presentable -> commits to surface.
  - If preparation fails: because there is no running session to protect, `failOrStartOutright` triggers immediate direct stream fallback (`play(unprepared:)`).
  - If fallback also fails: phase transitions to `.failed(serviceRef, reason)`, surface remains clean, and structured error is presented with retry action.

### 4.2 Invariant 2: Ordinary Failed Zap with Existing Playback (Make-before-Break)
- **Condition:** Channel A is actively playing on screen. User requests Channel B.
- **Behavior:**
  - Channel B fails backend preparation (e.g. timeout, unpresentable, no PMT).
  - **Core Guarantee:** Channel A continues playing completely undisturbed. Audio output rate remains 1.0, video frames continue rendering on surface, and presentation ownership does not change.
  - `requestedServiceRef` resets to `nil`.
  - Phase transitions to `.failed(B, reason)`. A non-blocking toast/notice informs the user while Channel A remains playing.

### 4.3 Invariant 3: Admission-Denied Break-before-Make Exception
- **Condition:** Channel A is playing. User requests Channel B on a receiver with hardware tuner contention (single tuner receiver, or all tuners active).
- **Behavior:**
  - Preparation returns `isAdmissionDenied == true` (`outcome == "admission_denied"`).
  - **Exception Contract:** In this scenario, Make-before-Break cannot succeed because Channel A holds the sole tuner lease needed by Channel B.
  - Therefore, `ZapCoordinator` explicitly performs an **admission-denied break-before-make fallback**: it calls `startOutright(url:)` for Channel B, releasing the tuner lease and tuning Channel B directly.

### 4.4 Invariant 4: Rapid Supersession with Late Completion
- **Condition:** User rapidly zaps A -> B -> C.
- **Behavior:**
  - When C is requested while B is in-flight, B is immediately abandoned and cancelled on backend (`preparations.cancel`).
  - If B's preparation or network request resolves late (after C has become the active zap ID):
    - Late B results are discarded (`guard isCurrent(zapID) else { return }`).
    - B **never** builds a session, binds to the surface, alters `phase`, or publishes an error.
    - Only C is committed to presentation.

### 4.5 Invariant 5: Stop & Dismissal
- **Condition:** User dismisses the player or navigates away while a zap or stream is preparing.
- **Behavior:**
  - `coordinator.stop()` tears down in-flight preparation, stops video/audio pipelines, and resets state.
  - Any caught cancellation errors are completely suppressed; no error is published to `lastError` or toasts.

### 4.6 Invariant 6: Cancellation During Buffering
- **Condition:** Task cancellation occurs while `awaitPresentable` is polling the session's readiness.
- **Behavior:**
  - `awaitPresentable` returns `.cancelled`.
  - The coordinator silently abandons in-flight backend preparation and sets `phase = .idle` without publishing a `NOT_PRESENTABLE` failure or showing an error toast.

### 4.7 Invariant 7: Delayed Cleanup Isolation
- **Condition:** An earlier cancelled zap undergoes asynchronous cleanup (e.g. DELETE preparation over network) that finishes after a newer zap has already become active.
- **Behavior:**
  - `abandonInFlight` re-checks that `requestedServiceRef` matches the departing preparation before resetting it.
  - The completion handler revalidates `isCurrent(zapID)` after awaiting cleanup before modifying `phase` or clearing state.
  - Newer zap ownership (`phase` and `requestedServiceRef`) remains strictly intact.

### 4.8 `awaitPresentable` Polling & Lifecycle Contract
- **No Timer Timeout:** `awaitPresentable` does **not** implement an internal timer-based timeout.
- It loops cooperatively while `isCurrent(zapID)`, checking `Task.isCancelled` and `session.isPresentable` on each iteration of `Task.sleep(for: pollInterval)`.
- Outcomes:
  - `.ready`: session reported `isPresentable == true`.
  - `.cancelled`: task was cancelled or sleep threw `CancellationError`.
  - `.notPresentable`: loop terminated while still current (e.g. if the condition fails without cancellation).

---

## 5. EPG Language Boundary

- **Source Integrity:** Broadcast EPG content (programme titles, event descriptions, episode summaries, genres) from Enigma2/OpenWebIF represents live external media metadata and must be **preserved verbatim in its original broadcast language** (e.g. German on German channels).
- **Application Chrome:** Only application-owned controls, buttons, placeholders, section titles, and status messages are localized into the user's active UI locale (EN / DE).
- A regression test confirms that German EPG programme content renders cleanly inside an English-configured UI without truncation, double translation, or placeholder corruption.
