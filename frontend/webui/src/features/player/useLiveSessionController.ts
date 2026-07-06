import { useCallback, useEffect, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import { createSession, getSessionEvents, type IntentRequest, type PlaybackEngineErrorContext } from '../../client-ts';
import { setClientAuthToken, throwOnClientResultError } from '../../services/clientWrapper';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../lib/httpProblem';
import type {
  PlayerStatus,
  SessionCookieState,
  V3SessionHeartbeatResponse,
  V3SessionSnapshot,
  V3SessionStatusResponse,
  VideoElementRef,
} from '../../types/v3-player';
import type { AppError } from '../../types/errors';
import { debugError, debugLog, debugWarn } from '../../utils/logging';
import type { PlaybackFailureReportOptions } from './semantics/playbackFailureSemantics';
import { translatePlaybackReason } from './utils/sessionReason';
import {
  HEARTBEAT_REQUEST_TIMEOUT_MS,
  SESSION_REQUEST_TIMEOUT_MS,
  timeoutSignal,
} from './utils/requestTimeout';

const SESSION_READY_TIMEOUT_MS = 60_000;
const SESSION_READY_POLL_MS = 250;
const SESSION_READY_MAX_ATTEMPTS = Math.ceil(SESSION_READY_TIMEOUT_MS / SESSION_READY_POLL_MS);

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';
type ErrorBodyReader = (res: Response) => Promise<{ json: any | null; text: string | null }>;
type PlayerErrorFactory = (message: string, details?: unknown) => Error;

interface UseLiveSessionControllerProps {
  token?: string;
  apiBase: string;
  t: TFunction;
  videoRef: RefObject<VideoElementRef>;
  setPlaybackMode: Dispatch<SetStateAction<PlaybackMode>>;
  setDurationSeconds: Dispatch<SetStateAction<number | null>>;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  clearPlaybackFailure: () => void;
  reportPlaybackFailure: (error: AppError, options?: PlaybackFailureReportOptions) => void;
  readResponseBody: ErrorBodyReader;
  createPlayerError: PlayerErrorFactory;
  onSessionSnapshot?: (session: V3SessionSnapshot) => void;
}

interface LiveSessionController {
  sessionId: string | null;
  sessionIdRef: MutableRefObject<string | null>;
  authHeaders: (contentType?: boolean) => HeadersInit;
  reportError: (event: 'error' | 'warning' | 'info', code: number, msg?: string) => Promise<void>;
  ensureSessionCookie: () => Promise<void>;
  recoverSessionCookie: (source: string) => Promise<boolean>;
  primePlaybackAuth: (playbackUrl: string, source: string) => Promise<void>;
  setActiveSessionId: (sessionId: string | null) => void;
  clearSessionLeaseState: () => void;
  sendStopIntent: (sessionId: string | null, force?: boolean) => Promise<void>;
  refreshSessionSnapshot: (sessionId?: string | null) => Promise<V3SessionStatusResponse | null>;
  waitForSessionReady: (sessionId: string, maxAttempts?: number) => Promise<V3SessionStatusResponse>;
}

type SessionRequestResult = {
  response: Response;
  recovered: boolean;
};

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function hasValidHeartbeatInterval(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (isRecord(error) && typeof error.message === 'string') return error.message;
  return '';
}

function errorDetails(error: unknown): Record<string, unknown> | null {
  if (!isRecord(error) || !isRecord(error.details)) return null;
  return error.details;
}

export function useLiveSessionController({
  token,
  apiBase,
  t,
  videoRef,
  setPlaybackMode,
  setDurationSeconds,
  setStatus,
  clearPlaybackFailure,
  reportPlaybackFailure,
  readResponseBody,
  createPlayerError,
  onSessionSnapshot
}: UseLiveSessionControllerProps): LiveSessionController {
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [heartbeatInterval, setHeartbeatInterval] = useState<number | null>(null);
  const [, setLeaseExpiresAt] = useState<string | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const stopSentRef = useRef<string | null>(null);
  const sessionCookieRef = useRef<SessionCookieState>({ token: null, pending: null });

  const authHeaders = useCallback((contentType: boolean = false): HeadersInit => {
    const headers: Record<string, string> = {};
    if (contentType) headers['Content-Type'] = 'application/json';
    if (token) headers.Authorization = `Bearer ${token}`;
    return headers;
  }, [token]);

  const reportError = useCallback(async (
    event: 'error' | 'warning' | 'info',
    code: number,
    msg?: string,
    context?: PlaybackEngineErrorContext,
  ) => {
    if (!sessionIdRef.current) return;
    try {
      // raw-fetch-justified: session feedback reporting
      await fetch(`${apiBase}/sessions/${sessionIdRef.current}/feedback`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({
          event,
          code,
          message: msg,
          ...(context ? { context } : {}),
        })
      });
    } catch (err) {
      debugWarn('Failed to send feedback', err);
    }
  }, [apiBase, authHeaders]);

  const ensureSessionCookie = useCallback(async (): Promise<void> => {
    if (!token) return;
    if (sessionCookieRef.current.pending) return sessionCookieRef.current.pending;

    const pending = (async () => {
      try {
        setClientAuthToken(token);
        const result = await createSession();
        throwOnClientResultError(result, { source: 'POST /auth/session' });
        sessionCookieRef.current.token = token;
      } catch (err) {
        sessionCookieRef.current.token = null;
        debugWarn('Failed to create session cookie', err);
        throw err;
      } finally {
        sessionCookieRef.current.pending = null;
      }
    })();

    sessionCookieRef.current.pending = pending;
    return pending;
  }, [token]);

  const recoverSessionCookie = useCallback(async (source: string): Promise<boolean> => {
    if (!token) {
      return false;
    }

    debugWarn('[V3Player][Auth] Session cookie lost, attempting recovery', { source });
    await ensureSessionCookie();
    return true;
  }, [ensureSessionCookie, token]);

  const fetchWithRecoveredSessionCookie = useCallback(async (
    source: string,
    request: () => Promise<Response>
  ): Promise<SessionRequestResult> => {
    const response = await request();
    if (response.status !== 401) {
      return { response, recovered: false };
    }

    if (!token) {
      notifyAuthRequiredIfUnauthorizedResponse(response, source);
      return { response, recovered: false };
    }

    await recoverSessionCookie(source);

    const retriedResponse = await request();
    if (retriedResponse.status === 401) {
      notifyAuthRequiredIfUnauthorizedResponse(retriedResponse, source);
      return { response: retriedResponse, recovered: false };
    }

    return { response: retriedResponse, recovered: true };
  }, [recoverSessionCookie, token]);

  const primePlaybackAuth = useCallback(async (playbackUrl: string, source: string): Promise<void> => {
    if (typeof window === 'undefined') {
      return;
    }

    let resolvedUrl: URL;
    try {
      resolvedUrl = new URL(playbackUrl, window.location.origin);
    } catch {
      return;
    }

    if (resolvedUrl.origin !== window.location.origin || !resolvedUrl.pathname.startsWith('/api/v3/')) {
      return;
    }

    const { response } = await fetchWithRecoveredSessionCookie(source, () => fetch(resolvedUrl.toString(), {
      method: 'HEAD',
      cache: 'no-store',
      credentials: 'same-origin',
      signal: timeoutSignal(SESSION_REQUEST_TIMEOUT_MS),
    }));

    if (response.status === 401) {
      throw createPlayerError(t('player.authFailed'), {
        url: response.url || resolvedUrl.toString(),
        status: 401,
        requestId: response.headers.get('X-Request-ID') || undefined,
      });
    }
  }, [createPlayerError, fetchWithRecoveredSessionCookie, t]);

  const setActiveSessionId = useCallback((nextSessionId: string | null) => {
    sessionIdRef.current = nextSessionId;
    setSessionId(nextSessionId);
  }, []);

  const clearSessionLeaseState = useCallback(() => {
    sessionIdRef.current = null;
    stopSentRef.current = null;
    setSessionId(null);
    setHeartbeatInterval(null);
    setLeaseExpiresAt(null);
  }, []);

  const sendStopIntent = useCallback(async (idToStop: string | null, force: boolean = false): Promise<void> => {
    if (!idToStop) return;
    if (!force && stopSentRef.current === idToStop) return;
    stopSentRef.current = idToStop;
    try {
      const body: IntentRequest = {
        type: 'stream.stop',
        sessionId: idToStop
      };
      // raw-fetch-justified: stream stop intent submission
      await fetch(`${apiBase}/intents`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify(body)
      });
    } catch (err) {
      debugWarn('Failed to stop v3 session', err);
    }
  }, [apiBase, authHeaders]);

  const applySessionInfo = useCallback((session: V3SessionStatusResponse) => {
    if (session.mode) {
      setPlaybackMode(session.mode === 'LIVE' ? 'LIVE' : 'VOD');
    }
    if (typeof session.durationSeconds === 'number' && session.durationSeconds > 0) {
      setDurationSeconds(session.durationSeconds);
    }

    // Only ever tighten lease state from a snapshot, never clear it: snapshots
    // of a session that is still STARTING (e.g. during a zap) can lack
    // heartbeatIntervalSeconds, and nulling the interval here would silently
    // stop the heartbeat loop of the session that is still playing. Clearing
    // is reserved for clearSessionLeaseState().
    if (hasValidHeartbeatInterval(session.heartbeatIntervalSeconds)) {
      setHeartbeatInterval(session.heartbeatIntervalSeconds);
    }
    if (session.leaseExpiresAt) {
      setLeaseExpiresAt(session.leaseExpiresAt);
    }
    onSessionSnapshot?.(session);
  }, [onSessionSnapshot, setDurationSeconds, setPlaybackMode]);

  const refreshSessionSnapshot = useCallback(async (targetSessionId?: string | null): Promise<V3SessionStatusResponse | null> => {
    const trackedSessionId = targetSessionId ?? sessionIdRef.current;
    if (!trackedSessionId) {
      return null;
    }

    try {
      const { response: res } = await fetchWithRecoveredSessionCookie(
        'useLiveSessionController.refreshSessionSnapshot',
        () => fetch(`${apiBase}/sessions/${trackedSessionId}`, {
          headers: authHeaders()
        })
      );

      if (!res.ok) {
        debugWarn('[V3Player][Session] Snapshot refresh failed', {
          sessionId: trackedSessionId,
          status: res.status,
        });
        return null;
      }

      const session: V3SessionStatusResponse = await res.json();
      if (sessionIdRef.current !== trackedSessionId) {
        return null;
      }

      applySessionInfo(session);
      return session;
    } catch (err) {
      debugWarn('[V3Player][Session] Snapshot refresh error', err);
      return null;
    }
  }, [apiBase, applySessionInfo, authHeaders, fetchWithRecoveredSessionCookie]);

  const waitForSessionReady = useCallback(async (
    trackedSessionId: string,
    maxAttempts = SESSION_READY_MAX_ATTEMPTS
  ): Promise<V3SessionStatusResponse> => {
    let recoveredSessionAuth = false;

    // 1. Initial quick check (in case existing session is already READY or fast-started)
    for (let i = 0; i < Math.min(3, maxAttempts); i++) {
      try {
        // raw-fetch-justified: initial live session readiness lookup
        const { response: res, recovered } = await fetchWithRecoveredSessionCookie(
          'useLiveSessionController.waitForSessionReady',
          () => fetch(`${apiBase}/sessions/${trackedSessionId}`, {
            headers: authHeaders(),
            signal: timeoutSignal(SESSION_REQUEST_TIMEOUT_MS),
          })
        );
        recoveredSessionAuth = recoveredSessionAuth || recovered;

        if (res.status === 401) {
          throw createPlayerError(t('player.authFailed'), {
            url: res.url,
            status: 401,
            requestId: res.headers.get('X-Request-ID') || undefined
          });
        }

        if (res.status === 403) {
          throw createPlayerError(t('player.forbidden'), {
            url: res.url,
            status: 403,
            requestId: res.headers.get('X-Request-ID') || undefined
          });
        }

        if (res.status === 404) {
          if (recoveredSessionAuth || i === Math.min(3, maxAttempts) - 1) {
            throw createPlayerError(t('player.sessionNotFound'), {
              url: res.url,
              status: 404,
              requestId: res.headers.get('X-Request-ID') || undefined,
              sessionId: trackedSessionId,
              recoveredSessionAuth: true
            });
          }
          await sleep(SESSION_READY_POLL_MS);
          continue;
        }

        if (res.status === 410) {
          const { json, text } = await readResponseBody(res);
          const requestId =
            (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
            res.headers.get('X-Request-ID') ||
            undefined;
          const reason = (json && typeof json === 'object' ? (json.reason ?? json.state ?? json.code) : undefined) as
            | string
            | undefined;
          const reasonDetail =
            (json && typeof json === 'object' ? (json.reason_detail ?? json.reasonDetail ?? json.detail) : undefined) as
            | string
            | undefined;
          const details = {
            url: res.url,
            status: res.status,
            requestId,
            code: json?.code,
            title: json?.title,
            detail: json?.detail,
            session: json?.session,
            state: json?.state,
            reason: json?.reason,
            reason_detail: json?.reason_detail,
            body: json ?? text
          };

          onSessionSnapshot?.({
            sessionId: typeof json?.session === 'string' ? json.session : trackedSessionId,
            requestId,
            state: typeof json?.state === 'string' ? json.state : 'FAILED',
            reason: typeof json?.reason === 'string' ? json.reason : undefined,
            reasonDetail: typeof json?.reason_detail === 'string'
              ? json.reason_detail
              : typeof json?.reasonDetail === 'string'
                ? json.reasonDetail
                : undefined,
            trace: (json && typeof json === 'object' && 'trace' in json) ? (json.trace as V3SessionStatusResponse['trace']) : undefined,
          });

          if (String(reason).includes('LEASE_BUSY') || String(reasonDetail).includes('LEASE_BUSY')) {
            throw createPlayerError(t('player.leaseBusy'), details);
          }
          if (recoveredSessionAuth) {
            throw createPlayerError(t('player.sessionExpired'), details);
          }
          throw createPlayerError(`${t('player.sessionFailed')}: ${translatePlaybackReason(reason, reasonDetail, t)}`, details);
        }

        if (!res.ok) {
          const { json, text } = await readResponseBody(res);
          const requestId =
            (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
            res.headers.get('X-Request-ID') ||
            undefined;
          throw createPlayerError(`${t('player.failedToFetchSession')} (HTTP ${res.status})`, {
            url: res.url,
            status: res.status,
            requestId,
            code: json?.code,
            title: json?.title,
            detail: json?.detail,
            body: json ?? text
          });
        }

        const session: V3SessionStatusResponse = await res.json();
        applySessionInfo(session);
        const state = session.state;
        if (state === 'FAILED' || state === 'STOPPED' || state === 'CANCELLED' || state === 'STOPPING') {
          const reason = session.reason || state;
          const detail = session.reasonDetail ? `: ${session.reasonDetail}` : '';
          if (String(reason).includes('LEASE_BUSY') || String(detail).includes('LEASE_BUSY')) {
            throw new Error(t('player.leaseBusy'));
          }
          throw new Error(`${t('player.sessionFailed')}: ${translatePlaybackReason(reason, session.reasonDetail, t)}`);
        }
        if ((state === 'READY' || state === 'DRAINING') && session.playbackUrl) {
          if (!hasValidHeartbeatInterval(session.heartbeatIntervalSeconds)) {
            throw createPlayerError(t('player.sessionFailed'), {
              contractError: true,
              requestId: session.requestId,
              sessionId: trackedSessionId,
              missingField: 'heartbeatIntervalSeconds'
            });
          }
          return session;
        }
        if (state === 'PRIMING') {
          setStatus('priming');
        } else {
          setStatus('starting');
        }
        if (i < Math.min(3, maxAttempts) - 1) {
          await sleep(SESSION_READY_POLL_MS);
          continue;
        }
        break; // Initial check complete, transition to SSE stream
      } catch (err) {
        const details = errorDetails(err);
        const msg = errorMessage(err);
        const status = typeof details?.status === 'number' ? details.status : undefined;
        if (details?.contractError === true || msg === t('player.leaseBusy') || msg.startsWith(t('player.sessionFailed')) || msg === t('player.authFailed') || msg === t('player.sessionExpired') || msg === t('player.sessionNotFound')) {
          throw err;
        }
        if (typeof status === 'number' && status >= 400 && status < 500 && status !== 404 && status !== 429) {
          throw err;
        }
        if (i === Math.min(3, maxAttempts) - 1) {
          break; // Fallback to SSE stream after initial retries
        }
        await sleep(500);
      }
    }

    // 2. Real-time SSE stream subscription for 0ms latency state transitions
    const maxTimeoutMs = maxAttempts * SESSION_READY_POLL_MS;
    return new Promise<V3SessionStatusResponse>((resolve, reject) => {
      let isDone = false;
      const abortController = new AbortController();

      const timerId = window.setTimeout(() => {
        if (isDone) return;
        isDone = true;
        abortController.abort();
        reject(createPlayerError(t('player.sessionNotReadyInTime'), {
          sessionId: trackedSessionId,
          waitedMs: maxTimeoutMs,
          pollMs: SESSION_READY_POLL_MS
        }));
      }, maxTimeoutMs);

      const cleanup = () => {
        if (isDone) return false;
        isDone = true;
        window.clearTimeout(timerId);
        abortController.abort();
        return true;
      };

      void getSessionEvents({
        path: { sessionID: trackedSessionId },
        signal: abortController.signal,
        onSseEvent: (event) => {
          if (isDone || abortController.signal.aborted) return;
          if (event.event === 'session.state_changed' && event.data && typeof event.data === 'object') {
            const stateData = event.data as { state?: string; reason?: string; reasonDetail?: string };
            const state = stateData.state;
            if (state === 'PRIMING') {
              setStatus('priming');
            } else if (state === 'READY' || state === 'DRAINING') {
              if (!cleanup()) return;
              fetchWithRecoveredSessionCookie(
                'useLiveSessionController.waitForSessionReady.final',
                // raw-fetch-justified: final ready live session lookup
                () => fetch(`${apiBase}/sessions/${trackedSessionId}`, {
                  headers: authHeaders(),
                  signal: timeoutSignal(SESSION_REQUEST_TIMEOUT_MS),
                })
              ).then(async ({ response: finalRes }) => {
                if (!finalRes.ok) {
                  throw new Error(`Failed to fetch ready session (HTTP ${finalRes.status})`);
                }
                const finalSession: V3SessionStatusResponse = await finalRes.json();
                applySessionInfo(finalSession);
                if (!hasValidHeartbeatInterval(finalSession.heartbeatIntervalSeconds)) {
                  throw createPlayerError(t('player.sessionFailed'), {
                    contractError: true,
                    requestId: finalSession.requestId,
                    sessionId: trackedSessionId,
                    missingField: 'heartbeatIntervalSeconds'
                  });
                }
                resolve(finalSession);
              }).catch((err) => reject(err));
            } else if (state === 'FAILED' || state === 'STOPPED' || state === 'CANCELLED' || state === 'STOPPING') {
              if (!cleanup()) return;
              const reason = stateData.reason || state;
              const detail = stateData.reasonDetail ? `: ${stateData.reasonDetail}` : '';
              if (String(reason).includes('LEASE_BUSY') || String(detail).includes('LEASE_BUSY')) {
                reject(createPlayerError(t('player.leaseBusy')));
              } else {
                reject(createPlayerError(`${t('player.sessionFailed')}: ${translatePlaybackReason(reason, stateData.reasonDetail, t)}`));
              }
            }
          }
        },
        onSseError: (err) => {
          if (isDone || abortController.signal.aborted) return;
          debugWarn('[V3Player] SSE connection error while waiting for session ready:', err);
        }
      }).then(async ({ stream }) => {
        try {
          for await (const _ of stream) {
            if (isDone || abortController.signal.aborted) break;
          }
        } catch {
          // Stream aborted or errored, ignore
        }
      }).catch((err) => {
        if (cleanup()) {
          reject(err);
        }
      });
    });
  }, [apiBase, applySessionInfo, authHeaders, createPlayerError, fetchWithRecoveredSessionCookie, onSessionSnapshot, readResponseBody, setStatus, t]);

  useEffect(() => {
    if (!sessionId || !heartbeatInterval) {
      return;
    }

    const trackedSessionId = sessionId;
    const intervalMs = heartbeatInterval * 1000;
    debugLog('[V3Player][Heartbeat] Starting heartbeat loop:', { sessionId: trackedSessionId, intervalMs });

    // Clamp the per-request deadline to one interval so a hung beat always
    // aborts before the next one fires (no pile-up of stuck requests).
    // Guard against non-finite intervalMs (e.g. malformed heartbeatInterval)
    // so AbortSignal.timeout never receives NaN → 0 → immediate abort.
    const safeIntervalMs = Number.isFinite(intervalMs) ? intervalMs : HEARTBEAT_REQUEST_TIMEOUT_MS;
    const heartbeatRequestTimeoutMs = Math.max(1000, Math.min(safeIntervalMs, HEARTBEAT_REQUEST_TIMEOUT_MS));

    const timerId = window.setInterval(async () => {
      try {
        const { response: res } = await fetchWithRecoveredSessionCookie(
          'useLiveSessionController.heartbeat',
          () => fetch(`${apiBase}/sessions/${trackedSessionId}/heartbeat`, {
            method: 'POST',
            headers: authHeaders(true),
            signal: timeoutSignal(heartbeatRequestTimeoutMs),
          })
        );

        if (sessionIdRef.current !== trackedSessionId) {
          return;
        }

        if (res.status === 401) {
          debugWarn('[V3Player][Heartbeat] Session unauthorized (401)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          clearPlaybackFailure();
          reportPlaybackFailure({
            title: t('player.authFailed'),
            status: 401,
            retryable: false,
            code: 'SESSION_UNAUTHORIZED',
          }, {
            source: 'native-host',
            failureClass: 'auth',
            retryable: false,
            recoverable: false,
            terminal: true,
          });
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 403) {
          debugWarn('[V3Player][Heartbeat] Session forbidden (403)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          clearPlaybackFailure();
          reportPlaybackFailure({
            title: t('player.forbidden'),
            status: 403,
            retryable: false,
            code: 'SESSION_FORBIDDEN',
          }, {
            source: 'native-host',
            failureClass: 'auth',
            retryable: false,
            recoverable: false,
            terminal: true,
          });
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 200) {
          const data: V3SessionHeartbeatResponse = await res.json();
          if (sessionIdRef.current !== trackedSessionId) {
            return;
          }
          if (!data.acknowledged || !data.leaseExpiresAt || data.sessionId !== trackedSessionId) {
            debugError('[V3Player][Heartbeat] Invalid heartbeat contract response', data);
            window.clearInterval(timerId);
            clearSessionLeaseState();
            setPlaybackMode('UNKNOWN');
            setStatus('error');
            clearPlaybackFailure();
            reportPlaybackFailure({
              title: t('player.sessionFailed'),
              status: 502,
              retryable: true,
              code: 'INVALID_HEARTBEAT_CONTRACT',
            }, {
              source: 'native-host',
              failureClass: 'session',
              retryable: true,
              recoverable: true,
              terminal: false,
            });
            if (videoRef.current) {
              videoRef.current.pause();
            }
            return;
          }
          setLeaseExpiresAt(data.leaseExpiresAt);
          debugLog('[V3Player][Heartbeat] Lease extended:', data.leaseExpiresAt);
          void refreshSessionSnapshot(trackedSessionId);
        } else if (res.status === 410) {
          debugError('[V3Player][Heartbeat] Session expired (410)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          clearPlaybackFailure();
          reportPlaybackFailure({
            title: t('player.sessionExpired') || 'Session expired. Please restart.',
            status: 410,
            retryable: true,
            code: 'SESSION_EXPIRED',
          }, {
            source: 'native-host',
            failureClass: 'session',
            retryable: true,
            recoverable: true,
            terminal: false,
          });
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 404) {
          debugWarn('[V3Player][Heartbeat] Session not found (404)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          clearPlaybackFailure();
          reportPlaybackFailure({
            title: t('player.sessionNotFound') || 'Session no longer exists.',
            status: 404,
            retryable: true,
            code: 'SESSION_NOT_FOUND',
          }, {
            source: 'native-host',
            failureClass: 'session',
            retryable: true,
            recoverable: true,
            terminal: false,
          });
          if (videoRef.current) {
            videoRef.current.pause();
          }
        }
      } catch (err) {
        debugError('[V3Player][Heartbeat] Network error:', err);
      }
    }, intervalMs);

    return () => {
      debugLog('[V3Player][Heartbeat] Cleanup: Clearing heartbeat timer');
      window.clearInterval(timerId);
    };
  }, [apiBase, authHeaders, clearPlaybackFailure, clearSessionLeaseState, fetchWithRecoveredSessionCookie, heartbeatInterval, refreshSessionSnapshot, reportPlaybackFailure, sessionId, setPlaybackMode, setStatus, t, videoRef]);

  useEffect(() => {
    setClientAuthToken(token);
    sessionCookieRef.current.token = null;
    sessionCookieRef.current.pending = null;
  }, [token]);

  return {
    sessionId,
    sessionIdRef,
      authHeaders,
    reportError,
    ensureSessionCookie,
    recoverSessionCookie,
    primePlaybackAuth,
    setActiveSessionId,
    clearSessionLeaseState,
    sendStopIntent,
    refreshSessionSnapshot,
    waitForSessionReady
  };
}
