// Liveness-critical fetches (the session heartbeat, the auth prime, the
// readiness poll) must fail FAST when a connection hangs — TCP open but no
// response, which is common on flaky mobile data, wifi handoffs and captive
// portals. Without a deadline such a request wedges until the server-side lease
// silently lapses, and the user only finds out when playback suddenly stops.
//
// An AbortSignal deadline turns a hung request into a quick, retryable failure:
// the heartbeat's own catch retries on the next tick (while the lease is still
// valid), the readiness poll just counts it as one failed attempt, and the auth
// prime surfaces a normal retryable error instead of hanging startup forever.

// Heartbeat deadline. Callers additionally clamp this to one heartbeat interval
// so an aborted beat is always done before the next one fires.
export const HEARTBEAT_REQUEST_TIMEOUT_MS = 10_000;

// Deadline for session-control requests (auth prime HEAD, readiness poll).
export const SESSION_REQUEST_TIMEOUT_MS = 15_000;

// timeoutSignal returns an AbortSignal that aborts after `ms`, or undefined when
// the runtime lacks AbortSignal.timeout (very old engines). Passing
// `signal: undefined` to fetch is a no-op, so callers can use it unconditionally.
// Evaluate it per fetch call (not once and reused) so each attempt — including
// the 401 re-auth retry inside fetchWithRecoveredSessionCookie — gets a fresh,
// un-aborted signal.
export function timeoutSignal(ms: number): AbortSignal | undefined {
  if (typeof AbortSignal === 'undefined' || typeof AbortSignal.timeout !== 'function') {
    return undefined;
  }
  return AbortSignal.timeout(ms);
}
