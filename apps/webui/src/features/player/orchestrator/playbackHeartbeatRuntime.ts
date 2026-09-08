// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackFailureClass } from './playbackTypes';
import { HEARTBEAT_REQUEST_TIMEOUT_MS } from '../utils/requestTimeout';
import type { V3SessionHeartbeatResponse, V3SessionStatusResponse } from '../../../types/v3-player';

export const HEARTBEAT_RETRY_INTERVAL_MS = 5_000;
export const CONNECTION_LOST_AFTER_FAILURES = 2;
const SNAPSHOT_REQUEST_TIMEOUT_MS = 10_000;

export interface HeartbeatFailureParams {
  code: string;
  status: number;
  message: string;
  failureClass: PlaybackFailureClass;
  retryable: boolean;
  recoverable: boolean;
  terminal: boolean;
  pauseMedia: boolean;
}

export interface PlaybackHeartbeatRuntimeOptions {
  sessionId: string;
  heartbeatIntervalSeconds: number;
  initialLeaseExpiresAt?: string | null;
  transport: LiveSessionTransport;
  playbackEpoch: number;
  sessionEpoch: number;
  onLeaseUpdated: (params: { leaseExpiresAt: string | null; connectionLost: boolean }) => void;
  onFailure: (params: HeartbeatFailureParams) => void;
  onSessionSnapshot?: (snapshot: V3SessionStatusResponse) => void;
  now?: () => number;
}

export interface PlaybackHeartbeatRuntime {
  start(): void;
  stop(): void;
  isSupervising(): boolean;
  getLeaseExpiresAt(): string | null;
  isConnectionLost(): boolean;
  getConsecutiveFailures(): number;
}

export function createPlaybackHeartbeatRuntime(
  options: PlaybackHeartbeatRuntimeOptions,
): PlaybackHeartbeatRuntime {
  const {
    sessionId,
    heartbeatIntervalSeconds,
    initialLeaseExpiresAt = null,
    transport,
    onLeaseUpdated,
    onFailure,
    onSessionSnapshot,
  } = options;

  let cancelled = false;
  let timerId: ReturnType<typeof setTimeout> | null = null;
  let activeAbortController: AbortController | null = null;
  let activeSnapshotController: AbortController | null = null;
  let consecutiveFailures = 0;
  let connectionLost = false;
  let currentLeaseExpiresAt: string | null = initialLeaseExpiresAt;
  let isSupervisingActive = false;
  let generation = 0;

  const intervalMs = heartbeatIntervalSeconds * 1000;
  const isValidInterval = Number.isFinite(intervalMs) && intervalMs > 0;
  const safeIntervalMs = isValidInterval ? intervalMs : 0;
  let currentDelayMs = safeIntervalMs;

  function stopTimersAndControllers(): void {
    if (timerId !== null) {
      clearTimeout(timerId);
      timerId = null;
    }
    if (activeAbortController) {
      activeAbortController.abort();
      activeAbortController = null;
    }
    if (activeSnapshotController) {
      activeSnapshotController.abort();
      activeSnapshotController = null;
    }
  }

  function schedule(delayMs: number): void {
    if (cancelled || !isSupervisingActive || !isValidInterval) {
      return;
    }
    currentDelayMs = delayMs;
    if (timerId !== null) {
      clearTimeout(timerId);
    }
    timerId = setTimeout(() => {
      timerId = null;
      void beat(generation);
    }, delayMs);
  }

  function noteUnreachable(gen: number): void {
    if (cancelled || gen !== generation || !isSupervisingActive) {
      return;
    }
    consecutiveFailures += 1;
    if (consecutiveFailures >= CONNECTION_LOST_AFTER_FAILURES) {
      connectionLost = true;
    }
    onLeaseUpdated({
      leaseExpiresAt: currentLeaseExpiresAt,
      connectionLost,
    });
    schedule(HEARTBEAT_RETRY_INTERVAL_MS);
  }

  async function refreshSnapshot(gen: number): Promise<void> {
    if (
      cancelled ||
      gen !== generation ||
      !isSupervisingActive ||
      !transport.fetchSessionSnapshot ||
      !onSessionSnapshot
    ) {
      return;
    }

    const snapshotCtrl = new AbortController();
    activeSnapshotController = snapshotCtrl;
    const hardTimeoutTimer = setTimeout(() => {
      snapshotCtrl.abort();
    }, SNAPSHOT_REQUEST_TIMEOUT_MS);

    try {
      const res = await Promise.race([
        transport.fetchSessionSnapshot({ sessionId, signal: snapshotCtrl.signal }),
        new Promise<never>((_, reject) => {
          const onAbort = () => reject(new DOMException('Aborted', 'AbortError'));
          snapshotCtrl.signal.addEventListener('abort', onAbort, { once: true });
        }),
      ]);

      if (cancelled || gen !== generation || !isSupervisingActive) {
        return;
      }

      if (res.status === 200 && res.data && typeof res.data === 'object') {
        const snapshot = res.data as V3SessionStatusResponse;
        if (snapshot.sessionId !== sessionId) {
          return;
        }
        if (typeof snapshot.leaseExpiresAt === 'string' && snapshot.leaseExpiresAt.trim().length > 0) {
          currentLeaseExpiresAt = snapshot.leaseExpiresAt;
          onLeaseUpdated({
            leaseExpiresAt: currentLeaseExpiresAt,
            connectionLost,
          });
        }
        onSessionSnapshot(snapshot);
      }
    } catch {
      // Snapshot refresh is supplementary; reachability is driven by beat()
    } finally {
      clearTimeout(hardTimeoutTimer);
      if (activeSnapshotController === snapshotCtrl) {
        activeSnapshotController = null;
      }
    }
  }

  async function beat(gen: number): Promise<void> {
    if (cancelled || gen !== generation || !isSupervisingActive) {
      return;
    }

    if (!transport.postHeartbeat) {
      // If transport provides no postHeartbeat, no-op gracefully
      return;
    }

    const heartbeatRequestTimeoutMs = Math.max(
      1000,
      Math.min(currentDelayMs, safeIntervalMs, HEARTBEAT_REQUEST_TIMEOUT_MS),
    );

    const abortController = new AbortController();
    activeAbortController = abortController;

    const hardTimeoutTimer = setTimeout(() => {
      abortController.abort();
    }, heartbeatRequestTimeoutMs);

    try {
      const res = await Promise.race([
        transport.postHeartbeat({
          sessionId,
          signal: abortController.signal,
        }),
        new Promise<never>((_, reject) => {
          const onAbort = () => reject(new DOMException('Aborted', 'AbortError'));
          abortController.signal.addEventListener('abort', onAbort, { once: true });
        }),
      ]);

      if (cancelled || gen !== generation || !isSupervisingActive) {
        return;
      }

      if (res.status === 200) {
        const data = res.data as V3SessionHeartbeatResponse | undefined;
        const isAcknowledged = data?.acknowledged === true;
        const validLease =
          typeof data?.leaseExpiresAt === 'string' && data.leaseExpiresAt.trim().length > 0
            ? data.leaseExpiresAt
            : null;
        const matchingSession = data?.sessionId === sessionId;

        if (!isAcknowledged || !validLease || !matchingSession) {
          stop();
          currentLeaseExpiresAt = null;
          connectionLost = false;
          onLeaseUpdated({ leaseExpiresAt: null, connectionLost: false });
          onFailure({
            code: 'INVALID_HEARTBEAT_CONTRACT',
            status: 502,
            message: 'Invalid heartbeat contract response',
            failureClass: 'session',
            retryable: true,
            recoverable: true,
            terminal: false,
            pauseMedia: true,
          });
          return;
        }

        consecutiveFailures = 0;
        connectionLost = false;
        currentLeaseExpiresAt = validLease;
        onLeaseUpdated({ leaseExpiresAt: validLease, connectionLost: false });
        schedule(safeIntervalMs);
        void refreshSnapshot(gen);
      } else if (res.status === 401) {
        stop();
        currentLeaseExpiresAt = null;
        connectionLost = false;
        onLeaseUpdated({ leaseExpiresAt: null, connectionLost: false });
        onFailure({
          code: 'SESSION_UNAUTHORIZED',
          status: 401,
          message: 'Session unauthorized (401)',
          failureClass: 'auth',
          retryable: false,
          recoverable: false,
          terminal: true,
          pauseMedia: true,
        });
      } else if (res.status === 403) {
        stop();
        currentLeaseExpiresAt = null;
        connectionLost = false;
        onLeaseUpdated({ leaseExpiresAt: null, connectionLost: false });
        onFailure({
          code: 'SESSION_FORBIDDEN',
          status: 403,
          message: 'Session forbidden (403)',
          failureClass: 'auth',
          retryable: false,
          recoverable: false,
          terminal: true,
          pauseMedia: true,
        });
      } else if (res.status === 404) {
        stop();
        currentLeaseExpiresAt = null;
        connectionLost = false;
        onLeaseUpdated({ leaseExpiresAt: null, connectionLost: false });
        onFailure({
          code: 'SESSION_NOT_FOUND',
          status: 404,
          message: 'Session no longer exists.',
          failureClass: 'session',
          retryable: true,
          recoverable: true,
          terminal: false,
          pauseMedia: true,
        });
      } else if (res.status === 410) {
        stop();
        currentLeaseExpiresAt = null;
        connectionLost = false;
        onLeaseUpdated({ leaseExpiresAt: null, connectionLost: false });
        onFailure({
          code: 'SESSION_EXPIRED',
          status: 410,
          message: 'Session expired. Please restart.',
          failureClass: 'session',
          retryable: true,
          recoverable: true,
          terminal: false,
          pauseMedia: true,
        });
      } else {
        // Unexpected HTTP status (e.g. 500, 502, 503) -> reachability retry path
        noteUnreachable(gen);
      }
    } catch {
      if (cancelled || gen !== generation || !isSupervisingActive) {
        return;
      }
      noteUnreachable(gen);
    } finally {
      clearTimeout(hardTimeoutTimer);
      if (activeAbortController === abortController) {
        activeAbortController = null;
      }
    }
  }

  function start(): void {
    if (isSupervisingActive || cancelled) {
      return;
    }
    if (!isValidInterval) {
      return;
    }
    isSupervisingActive = true;
    generation += 1;
    schedule(safeIntervalMs);
  }

  function stop(): void {
    if (!isSupervisingActive && cancelled) {
      return;
    }
    isSupervisingActive = false;
    cancelled = true;
    generation += 1;
    stopTimersAndControllers();
  }

  return {
    start,
    stop,
    isSupervising: () => isSupervisingActive && !cancelled,
    getLeaseExpiresAt: () => currentLeaseExpiresAt,
    isConnectionLost: () => connectionLost,
    getConsecutiveFailures: () => consecutiveFailures,
  };
}
