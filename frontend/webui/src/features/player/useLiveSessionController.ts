import { useCallback, useEffect, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import { createSession, type IntentRequest } from '../../client-ts';
import { setClientAuthToken, throwOnClientResultError } from '../../services/clientWrapper';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../lib/httpProblem';
import { telemetry } from '../../services/TelemetryService';
import type {
  PlayerStatus,
  SessionCookieState,
  V3SessionHeartbeatResponse,
  V3SessionSnapshot,
  V3SessionStatusResponse,
  VideoElementRef,
} from '../../types/v3-player';
import { debugError, debugLog, debugWarn } from '../../utils/logging';

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
  setError: Dispatch<SetStateAction<string | null>>;
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
  setActiveSessionId: (sessionId: string | null) => void;
  clearSessionLeaseState: () => void;
  sendStopIntent: (sessionId: string | null, force?: boolean) => Promise<void>;
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

export function useLiveSessionController({
  token,
  apiBase,
  t,
  videoRef,
  setPlaybackMode,
  setDurationSeconds,
  setStatus,
  setError,
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

  const reportError = useCallback(async (event: 'error' | 'warning' | 'info', code: number, msg?: string) => {
    if (!sessionIdRef.current) return;
    try {
      await fetch(`${apiBase}/sessions/${sessionIdRef.current}/feedback`, {
        method: 'POST',
        headers: authHeaders(true),
        body: JSON.stringify({
          event,
          code,
          message: msg
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

    setHeartbeatInterval(hasValidHeartbeatInterval(session.heartbeatIntervalSeconds) ? session.heartbeatIntervalSeconds : null);
    setLeaseExpiresAt(session.leaseExpiresAt ?? null);
    onSessionSnapshot?.(session);
  }, [onSessionSnapshot, setDurationSeconds, setPlaybackMode]);

  const waitForSessionReady = useCallback(async (
    trackedSessionId: string,
    maxAttempts = SESSION_READY_MAX_ATTEMPTS
  ): Promise<V3SessionStatusResponse> => {
    let recoveredSessionAuth = false;

    for (let i = 0; i < maxAttempts; i++) {
      try {
        const { response: res, recovered } = await fetchWithRecoveredSessionCookie(
          'useLiveSessionController.waitForSessionReady',
          () => fetch(`${apiBase}/sessions/${trackedSessionId}`, {
            headers: authHeaders()
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
          if (recoveredSessionAuth) {
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
          const combined = `${reason ?? 'GONE'}${reasonDetail ? `: ${reasonDetail}` : ''}`;
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

          telemetry.emit('ui.error', {
            status: 410,
            code: 'SESSION_GONE',
            reason: reason ?? null,
            reason_detail: reasonDetail ?? null,
            requestId
          });

          if (String(reason).includes('LEASE_BUSY') || String(reasonDetail).includes('LEASE_BUSY')) {
            throw createPlayerError(t('player.leaseBusy'), details);
          }
          if (recoveredSessionAuth) {
            throw createPlayerError(t('player.sessionExpired'), details);
          }
          throw createPlayerError(`${t('player.sessionFailed')}: ${combined}`, details);
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
          throw new Error(`${t('player.sessionFailed')}: ${reason}${detail}`);
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
        await sleep(SESSION_READY_POLL_MS);
      } catch (err) {
        const e = err as any;
        const msg = e?.message || '';
        const status = e?.details?.status as number | undefined;
        if (e?.details?.contractError) {
          throw err;
        }
        if (
          msg === t('player.leaseBusy')
          || msg.startsWith(t('player.sessionFailed'))
          || msg === t('player.authFailed')
          || msg === t('player.sessionExpired')
          || msg === t('player.sessionNotFound')
        ) {
          throw err;
        }
        if (typeof status === 'number' && status >= 400 && status < 500 && status !== 404 && status !== 429) {
          throw err;
        }
        if (i === maxAttempts - 1) {
          const details = {
            ...(e?.details && typeof e.details === 'object' ? e.details : {}),
            sessionId: trackedSessionId,
            waitedMs: maxAttempts * SESSION_READY_POLL_MS,
            pollMs: SESSION_READY_POLL_MS
          };
          throw createPlayerError(`${t('player.readinessCheckFailed')}: ${msg}`, details);
        }
        await sleep(500);
      }
    }

    throw createPlayerError(t('player.sessionNotReadyInTime'), {
      sessionId: trackedSessionId,
      waitedMs: maxAttempts * SESSION_READY_POLL_MS,
      pollMs: SESSION_READY_POLL_MS
    });
  }, [apiBase, applySessionInfo, authHeaders, createPlayerError, onSessionSnapshot, readResponseBody, setStatus, t]);

  useEffect(() => {
    if (!sessionId || !heartbeatInterval) {
      return;
    }

    const trackedSessionId = sessionId;
    const intervalMs = heartbeatInterval * 1000;
    debugLog('[V3Player][Heartbeat] Starting heartbeat loop:', { sessionId: trackedSessionId, intervalMs });

    const timerId = window.setInterval(async () => {
      try {
        const { response: res } = await fetchWithRecoveredSessionCookie(
          'useLiveSessionController.heartbeat',
          () => fetch(`${apiBase}/sessions/${trackedSessionId}/heartbeat`, {
            method: 'POST',
            headers: authHeaders(true)
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
          setError(t('player.authFailed'));
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 403) {
          debugWarn('[V3Player][Heartbeat] Session forbidden (403)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          setError(t('player.forbidden'));
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
            setError(t('player.sessionFailed'));
            if (videoRef.current) {
              videoRef.current.pause();
            }
            return;
          }
          setLeaseExpiresAt(data.leaseExpiresAt);
          debugLog('[V3Player][Heartbeat] Lease extended:', data.leaseExpiresAt);
        } else if (res.status === 410) {
          debugError('[V3Player][Heartbeat] Session expired (410)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          setError(t('player.sessionExpired') || 'Session expired. Please restart.');
          if (videoRef.current) {
            videoRef.current.pause();
          }
        } else if (res.status === 404) {
          debugWarn('[V3Player][Heartbeat] Session not found (404)');
          window.clearInterval(timerId);
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
          setStatus('error');
          setError(t('player.sessionNotFound') || 'Session no longer exists.');
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
  }, [apiBase, authHeaders, clearSessionLeaseState, heartbeatInterval, sessionId, setError, setPlaybackMode, setStatus, t, videoRef]);

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
    setActiveSessionId,
    clearSessionLeaseState,
    sendStopIntent,
    waitForSessionReady
  };
}
