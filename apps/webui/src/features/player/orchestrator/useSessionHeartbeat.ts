import { useEffect, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import type { VideoElementRef, V3SessionHeartbeatResponse, V3SessionStatusResponse } from '../../../types/v3-player';
import type { AppError } from '../../../types/errors';
import type { PlaybackFailureReportOptions } from '../semantics/playbackFailureSemantics';
import { debugError, debugLog, debugWarn } from '../../../utils/logging';
import { HEARTBEAT_REQUEST_TIMEOUT_MS } from '../utils/requestTimeout';

export const HEARTBEAT_RETRY_INTERVAL_MS = 5_000;
export const CONNECTION_LOST_AFTER_FAILURES = 2;

export interface UseSessionHeartbeatProps {
  sessionId: string | null;
  sessionIdRef: MutableRefObject<string | null>;
  heartbeatInterval: number | null;
  apiBase: string;
  authHeaders: (contentType?: boolean) => HeadersInit;
  fetchWithRecoveredSessionCookie: (
    source: string,
    operation: () => Promise<Response>,
  ) => Promise<{ response: Response; recovered: boolean }>;
  refreshSessionSnapshot: (sessionId?: string | null) => Promise<V3SessionStatusResponse | null>;
  clearSessionLeaseState: () => void;
  reportPlaybackFailure: (error: AppError, options?: PlaybackFailureReportOptions) => void;
  clearPlaybackFailure: () => void;
  setPlaybackMode: Dispatch<SetStateAction<'LIVE' | 'VOD' | 'UNKNOWN'>>;
  videoRef: RefObject<VideoElementRef>;
  t: TFunction;
}

export interface UseSessionHeartbeatResult {
  connectionLost: boolean;
  leaseExpiresAt: string | null;
  setConnectionLost: Dispatch<SetStateAction<boolean>>;
}

export function useSessionHeartbeat({
  sessionId,
  sessionIdRef,
  heartbeatInterval,
  apiBase,
  authHeaders,
  fetchWithRecoveredSessionCookie,
  refreshSessionSnapshot,
  clearSessionLeaseState,
  reportPlaybackFailure,
  clearPlaybackFailure,
  setPlaybackMode,
  videoRef,
  t,
}: UseSessionHeartbeatProps): UseSessionHeartbeatResult {
  const [connectionLost, setConnectionLost] = useState<boolean>(false);
  const [leaseExpiresAt, setLeaseExpiresAt] = useState<string | null>(null);

  useEffect(() => {
    const trackedSessionId = sessionId;
    if (!trackedSessionId || !heartbeatInterval) {
      setConnectionLost(false);
      setLeaseExpiresAt(null);
      return;
    }

    const safeIntervalMs = Math.max(1000, heartbeatInterval * 1000);
    debugLog('[V3Player][Heartbeat] Setting up heartbeat loop for session:', trackedSessionId, 'interval:', safeIntervalMs);

    let timerId: number | null = null;
    let cancelled = false;
    let consecutiveFailures = 0;
    let currentDelayMs = safeIntervalMs;
    const lifecycleAbort = new AbortController();

    const stopLoop = () => {
      cancelled = true;
      if (timerId !== null) {
        window.clearTimeout(timerId);
        timerId = null;
      }
      lifecycleAbort.abort();
    };

    const schedule = (delayMs: number) => {
      if (cancelled) return;
      currentDelayMs = delayMs;
      timerId = window.setTimeout(() => {
        timerId = null;
        void beat();
      }, delayMs);
    };

    const noteUnreachable = () => {
      consecutiveFailures += 1;
      if (consecutiveFailures >= CONNECTION_LOST_AFTER_FAILURES) {
        setConnectionLost(true);
      }
      schedule(HEARTBEAT_RETRY_INTERVAL_MS);
    };

    const beat = async () => {
      if (cancelled) return;
      const heartbeatRequestTimeoutMs = Math.max(
        1000,
        Math.min(currentDelayMs, safeIntervalMs, HEARTBEAT_REQUEST_TIMEOUT_MS),
      );

      const beatAbort = new AbortController();
      const timeoutId = window.setTimeout(() => {
        beatAbort.abort(new DOMException('Timeout', 'TimeoutError'));
      }, heartbeatRequestTimeoutMs);

      const onLifecycleAbort = () => {
        beatAbort.abort(new DOMException('Aborted', 'AbortError'));
      };
      lifecycleAbort.signal.addEventListener('abort', onLifecycleAbort, { once: true });

      try {
        const { response: res } = await fetchWithRecoveredSessionCookie(
          'useSessionHeartbeat.heartbeat',
          () => fetch(`${apiBase}/sessions/${trackedSessionId}/heartbeat`, {
            method: 'POST',
            headers: authHeaders(true),
            signal: beatAbort.signal,
          })
        );

        if (cancelled || sessionIdRef.current !== trackedSessionId) {
          return;
        }

        if (res.status === 401) {
          debugWarn('[V3Player][Heartbeat] Session unauthorized (401)');
          stopLoop();
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
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
          stopLoop();
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
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
          if (cancelled || sessionIdRef.current !== trackedSessionId) {
            return;
          }
          if (!data.acknowledged || !data.leaseExpiresAt || data.sessionId !== trackedSessionId) {
            debugError('[V3Player][Heartbeat] Invalid heartbeat contract response', data);
            stopLoop();
            clearSessionLeaseState();
            setPlaybackMode('UNKNOWN');
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
          if (cancelled || sessionIdRef.current !== trackedSessionId) {
            return;
          }
          consecutiveFailures = 0;
          setConnectionLost(false);
          schedule(safeIntervalMs);
        } else if (res.status === 410) {
          debugError('[V3Player][Heartbeat] Session expired (410)');
          stopLoop();
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
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
          stopLoop();
          clearSessionLeaseState();
          setPlaybackMode('UNKNOWN');
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
        } else {
          debugWarn('[V3Player][Heartbeat] Unexpected status', res.status);
          noteUnreachable();
        }
      } catch (err) {
        if (cancelled || sessionIdRef.current !== trackedSessionId) return;
        debugError('[V3Player][Heartbeat] Network error:', err);
        noteUnreachable();
      } finally {
        window.clearTimeout(timeoutId);
        lifecycleAbort.signal.removeEventListener('abort', onLifecycleAbort);
      }
    };

    schedule(safeIntervalMs);

    return () => {
      debugLog('[V3Player][Heartbeat] Cleanup: Clearing heartbeat timer');
      stopLoop();
      setConnectionLost(false);
    };
  }, [
    apiBase,
    authHeaders,
    clearPlaybackFailure,
    clearSessionLeaseState,
    fetchWithRecoveredSessionCookie,
    heartbeatInterval,
    refreshSessionSnapshot,
    reportPlaybackFailure,
    sessionId,
    sessionIdRef,
    setPlaybackMode,
    t,
    videoRef,
  ]);

  return {
    connectionLost,
    leaseExpiresAt,
    setConnectionLost,
  };
}
