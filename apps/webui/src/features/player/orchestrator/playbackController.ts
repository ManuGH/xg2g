// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
  createPlaybackMachineRuntime,
  type PlaybackCommandExecutor,
  type PlaybackMachineRuntime,
} from './playbackMachineRuntime';
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
  PlaybackStopReason,
} from './playbackTypes';
import {
  type LiveSessionTransport,
  PlaybackHttpError,
  type SessionReadyResult,
  type StartIntentResult,
  type StreamInfoResult,
} from './liveSessionTransport';
import { normalizePlaybackInfo } from '../contracts/normalizePlaybackInfo';
import type { NormalizedPlayablePlaybackContract } from '../contracts/normalizedPlaybackTypes';
import { buildLiveIntentBody } from './startupHelpers';
import { buildContractState } from './contractErrors';
import type { PlayerStatus, V3SessionStatusResponse } from '../../../types/v3-player';
import {
  createPlaybackHeartbeatRuntime,
  type PlaybackHeartbeatRuntime,
} from './playbackHeartbeatRuntime';
import { buildPlaybackFailure } from './playbackMachine';

export { PlaybackHttpError };

export function isOkStatus(status: number): boolean {
  return status >= 200 && status < 300;
}

export interface StartLiveParams {
  serviceRef: string;
  epoch?: number;
  capabilities?: unknown;
  profileHeaders?: Record<string, string>;
  requestedDuration?: number | null;
  hasSessionIntent?: boolean;
  explicitProfilePinned?: boolean;
}

export type StartLiveResult =
  | {
      status: 'ready';
      sessionId: string;
      session: SessionReadyResult;
      contract?: NormalizedPlayablePlaybackContract;
    }
  | { status: 'cancelled'; reason: 'superseded' | 'user_stop' | 'timeout' };

export interface PlaybackControllerOptions {
  transport?: LiveSessionTransport;
  getTransport?: () => LiveSessionTransport;
  createInitialState: () => PlaybackDomainState;
  executeCommand?: PlaybackCommandExecutor;
  startSettlementTimeoutMs?: number; // default 30_000ms (overall start/retry budget)
  httpRequestTimeoutMs?: number;     // default 10_000ms (per-request HTTP timeout)
  stopRequestTimeoutMs?: number;     // default 3_000ms
  requestedDuration?: number | null;
  onAttemptStarted?: (epoch: number) => void;
  onSessionSnapshot?: (snapshot: V3SessionStatusResponse) => void;
}

interface InFlightStart {
  attemptId: string;
  epoch: number;
  transport: LiveSessionTransport;
  cancelled: boolean;
  cancelReason: 'superseded' | 'user_stop' | 'timeout' | null;
  ineligibleForAdoption: boolean;
  receivedResponse: boolean;
  settled: boolean;
  settlementTimer: ReturnType<typeof setTimeout> | null;
  resolvePublic: (result: StartLiveResult) => void;
  rejectPublic: (err: unknown) => void;
  abortController: AbortController;
}

export interface PlaybackController {
  // Runtime state & machine dispatch
  getState(): PlaybackDomainState;
  subscribe(listener: () => void): () => void;
  dispatch(event: PlaybackMachineEvent): void;
  setCommandExecutor(executor: PlaybackCommandExecutor | null): void;

  // Single epoch & attempt authority across all modes
  allocatePlaybackEpoch(): number;
  allocateSessionEpoch(playbackEpoch: number): number;
  beginPlaybackAttempt(
    epoch: number,
    nextPlaybackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
    nextStatus: PlayerStatus,
    hasSessionIntent?: boolean,
    explicitProfilePinned?: boolean,
  ): void;
  markPlaybackStopped(epoch: number): void;
  isStalePlaybackEpoch(epoch: number): boolean;
  isStaleSessionEpoch(playbackEpoch: number, sessionEpoch: number): boolean;
  getEpoch(): number;

  // Live session lifecycle
  startLive(params: StartLiveParams): Promise<StartLiveResult>;
  stop(reason?: PlaybackStopReason | string, notifyClose?: boolean): Promise<void>;
  activate(): void;
  dispose(): void;

  // Observability & inspection for tests
  getActiveSessionId(): string | null;
  getInFlightStartsCount(): number;
  getStoppingSessionIds(): ReadonlySet<string>;
  getPendingAdoptionCandidatesCount(): number;
  getHeartbeatSessionId(): string | null;
  isHeartbeatSupervising(): boolean;
}

export const DEFAULT_HTTP_REQUEST_TIMEOUT_MS = 10_000;
export const DEFAULT_START_SETTLEMENT_TIMEOUT_MS = 30_000;
export const DEFAULT_STOP_REQUEST_TIMEOUT_MS = 3_000;
export const MAX_LEASE_CONFLICT_RETRIES = 3;
export const DEFAULT_LEASE_CONFLICT_WAIT_MS = 1_000;
export const MAX_LEASE_CONFLICT_WAIT_MS = 5_000;

export function parseSessionId(data: unknown): string | null {
  if (!data || typeof data !== 'object') {
    return null;
  }
  const raw = (data as { sessionId?: unknown }).sessionId;
  if (typeof raw !== 'string') {
    return null;
  }
  const trimmed = raw.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function raceWithSignal<T>(
  promise: Promise<T>,
  signal: AbortSignal,
  timeoutErrorMsg: string,
): Promise<T> {
  if (signal.aborted) {
    return Promise.reject(new Error(timeoutErrorMsg));
  }
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(new Error(timeoutErrorMsg));
    signal.addEventListener('abort', onAbort, { once: true });
    promise.then(
      (res) => {
        signal.removeEventListener('abort', onAbort);
        resolve(res);
      },
      (err) => {
        signal.removeEventListener('abort', onAbort);
        reject(err);
      },
    );
  });
}

export function createPlaybackController(
  options: PlaybackControllerOptions,
): PlaybackController {
  const {
    createInitialState,
    executeCommand = () => {},
    stopRequestTimeoutMs = DEFAULT_STOP_REQUEST_TIMEOUT_MS,
  } = options;

  const overallStartBudgetMs =
    options.startSettlementTimeoutMs ?? DEFAULT_START_SETTLEMENT_TIMEOUT_MS;
  const httpRequestTimeoutMs =
    options.httpRequestTimeoutMs ??
    Math.min(DEFAULT_HTTP_REQUEST_TIMEOUT_MS, overallStartBudgetMs);

  let executor: PlaybackCommandExecutor | null = executeCommand;

  const runtime: PlaybackMachineRuntime = createPlaybackMachineRuntime(
    createInitialState,
    (command) => {
      if (executor) {
        return executor(command);
      }
    },
  );

  let playbackEpoch = runtime.getState().epoch.playback;
  let sessionEpoch = runtime.getState().epoch.session;
  const stoppedEpochs = new Set<number>();

  let activeSessionId: string | null = null;
  let activeSessionTransport: LiveSessionTransport | null = null;
  let currentAttempt: InFlightStart | null = null;
  const inFlightStarts = new Map<string, InFlightStart>();
  const stoppingSessionIds = new Set<string>();
  const activeStopPromises = new Map<string, Promise<void>>();
  const inFlightStopPromises = new Map<number, Promise<void>>();
  const getLatestTransport = (): LiveSessionTransport => {
    if (typeof options.transport === 'function') {
      return (options.transport as () => LiveSessionTransport)();
    }
    if (options.getTransport) {
      return options.getTransport();
    }
    return options.transport!;
  };

  const pendingAdoptionCandidates = new Map<
    string,
    { sessionId: string; heldAt: number; transport: LiveSessionTransport }
  >();

  let isDisposed = false;
  let heartbeatRuntime: PlaybackHeartbeatRuntime | null = null;
  let heartbeatSessionId: string | null = null;
  let heartbeatGeneration = 0;

  function stopHeartbeatSupervision(): void {
    heartbeatGeneration += 1;
    if (heartbeatRuntime) {
      heartbeatRuntime.stop();
      heartbeatRuntime = null;
    }
    heartbeatSessionId = null;
  }

  function hasEligibleInFlightStarts(): boolean {
    for (const start of inFlightStarts.values()) {
      if (!start.ineligibleForAdoption && !start.cancelled && !start.receivedResponse) {
        return true;
      }
    }
    return false;
  }

  function retireAndStopSession(
    sessionId: string,
    transportToUse?: LiveSessionTransport,
  ): Promise<void> {
    if (stoppingSessionIds.has(sessionId)) {
      return activeStopPromises.get(sessionId) ?? Promise.resolve();
    }

    stoppingSessionIds.add(sessionId);
    pendingAdoptionCandidates.delete(sessionId);

    const transportForStop = transportToUse ?? getLatestTransport();

    const stopController = new AbortController();
    let timer: ReturnType<typeof setTimeout> | null = null;

    const promise = new Promise<void>((resolve) => {
      timer = setTimeout(() => {
        stopController.abort();
        resolve();
      }, stopRequestTimeoutMs);

      Promise.resolve(
        transportForStop.postStopIntent({ sessionId, signal: stopController.signal }),
      ).then(
        () => {
          if (timer) clearTimeout(timer);
          resolve();
        },
        () => {
          // Best effort: remote stop failure does not block client teardown
          if (timer) clearTimeout(timer);
          resolve();
        },
      );
    }).finally(() => {
      activeStopPromises.delete(sessionId);
    });

    activeStopPromises.set(sessionId, promise);
    return promise;
  }

  function handleObsoleteSession(
    sessionId: string | null | undefined,
    transportToUse?: LiveSessionTransport,
  ): void {
    if (!sessionId) {
      return;
    }

    const safeSessionId = sessionId.trim();
    if (!safeSessionId) return;

    if (stoppingSessionIds.has(safeSessionId)) {
      return;
    }

    if (safeSessionId === activeSessionId) {
      return;
    }

    if (hasEligibleInFlightStarts() || inFlightStopPromises.size > 0) {
      pendingAdoptionCandidates.set(safeSessionId, {
        sessionId: safeSessionId,
        heldAt: Date.now(),
        transport: transportToUse ?? getLatestTransport(),
      });
      return;
    }

    void retireAndStopSession(safeSessionId, transportToUse);
  }

  function flushPendingAdoptionCandidates(): void {
    if (hasEligibleInFlightStarts()) {
      return;
    }
    for (const candidate of Array.from(pendingAdoptionCandidates.values())) {
      if (candidate.sessionId !== activeSessionId) {
        void retireAndStopSession(candidate.sessionId, candidate.transport);
      }
    }
    pendingAdoptionCandidates.clear();
  }

  function allocatePlaybackEpoch(): number {
    playbackEpoch += 1;
    sessionEpoch = 0;
    options.onAttemptStarted?.(playbackEpoch);

    // Invalidate any in-flight live start immediately and idempotently
    if (currentAttempt && !currentAttempt.settled) {
      if (currentAttempt.settlementTimer) {
        clearTimeout(currentAttempt.settlementTimer);
        currentAttempt.settlementTimer = null;
      }
      currentAttempt.cancelled = true;
      currentAttempt.cancelReason = 'superseded';
      currentAttempt.abortController.abort();
      currentAttempt.settled = true;
      currentAttempt.resolvePublic({ status: 'cancelled', reason: 'superseded' });
      currentAttempt = null;
    }

    return playbackEpoch;
  }

  function allocateSessionEpoch(pEpoch: number): number {
    sessionEpoch += 1;
    runtime.dispatch({
      type: 'normative.session.attempt.started',
      playbackEpoch: pEpoch,
      sessionEpoch,
    });
    return sessionEpoch;
  }

  function beginPlaybackAttempt(
    epoch: number,
    nextPlaybackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
    nextStatus: PlayerStatus,
    hasSessionIntent = true,
    explicitProfilePinned = false,
  ): void {
    if (isDisposed) {
      return;
    }

    // Fence: a stale attempt must never mutate state or retire a newer session!
    if (isStalePlaybackEpoch(epoch)) {
      return;
    }

    // If switching to a non-browser-Live mode, retire any active Live session!
    if (nextPlaybackMode !== 'LIVE' || !hasSessionIntent) {
      stopHeartbeatSupervision();
      const sessionToRetire = activeSessionId;
      const transportToUse = activeSessionTransport ?? getLatestTransport();
      activeSessionId = null;
      activeSessionTransport = null;
      if (sessionToRetire) {
        void retireAndStopSession(sessionToRetire, transportToUse);
      }
      flushPendingAdoptionCandidates();
    }

    options.onAttemptStarted?.(epoch);
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch,
      playbackMode: nextPlaybackMode,
      status: nextStatus,
      requestedDuration: options.requestedDuration ?? null,
      hasSessionIntent,
      explicitProfilePinned,
    });
  }

  function markPlaybackStopped(epoch: number): void {
    runtime.dispatch({
      type: 'normative.playback.stopped',
      epoch,
    });
  }

  function isStalePlaybackEpoch(epoch: number): boolean {
    return epoch !== playbackEpoch || stoppedEpochs.has(epoch);
  }

  function isStaleSessionEpoch(pEpoch: number, sEpoch: number): boolean {
    return isStalePlaybackEpoch(pEpoch) || sEpoch !== sessionEpoch;
  }

  async function executeLiveStartup(
    attempt: InFlightStart,
    params: StartLiveParams,
  ): Promise<void> {
    const { serviceRef, capabilities, profileHeaders } = params;
    let returnedSessionId: string | null = null;

    try {
      // 1. Preflight
      const preflightController = new AbortController();
      const preflightTimer = setTimeout(() => {
        preflightController.abort();
      }, httpRequestTimeoutMs);
      const onPreflightAttemptAbort = () => preflightController.abort();
      attempt.abortController.signal.addEventListener('abort', onPreflightAttemptAbort, { once: true });

      let preflightRes: StreamInfoResult;
      try {
        preflightRes = await raceWithSignal(
          attempt.transport.fetchStreamInfo({
            serviceRef,
            capabilities,
            profileHeaders,
            signal: preflightController.signal,
          }),
          preflightController.signal,
          'Preflight request timed out or aborted',
        );
      } finally {
        clearTimeout(preflightTimer);
        attempt.abortController.signal.removeEventListener('abort', onPreflightAttemptAbort);
      }

      if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
        if (attempt.settlementTimer) clearTimeout(attempt.settlementTimer);
        inFlightStarts.delete(attempt.attemptId);
        flushPendingAdoptionCandidates();
        return;
      }

      if (!isOkStatus(preflightRes.status)) {
        throw new PlaybackHttpError(
          `Preflight failed with status ${preflightRes.status}`,
          preflightRes.status,
          preflightRes.data,
          preflightRes.headers,
        );
      }

      // 2. Normalize preflight contract & shape intent payload
      const caps = (capabilities ?? {}) as Record<string, unknown>;
      const preferredHlsEngine =
        caps.preferredHlsEngine === 'native' || caps.preferredHlsEngine === 'hlsjs'
          ? caps.preferredHlsEngine
          : 'hlsjs';

      const normalized = normalizePlaybackInfo(preflightRes.data, {
        surface: 'live',
        preferredHlsEngine,
      });

      if (normalized.kind === 'blocked') {
        throw new Error(normalized.failure.message || 'Playback blocked');
      }

      if (normalized.kind === 'playable') {
        runtime.dispatch({
          type: 'normative.playback.contract.resolved',
          epoch: attempt.epoch,
          contract: buildContractState('live', normalized, normalized.playback.outputUrl),
        });
      }

      const liveMode = normalized.playback.mode;
      const decisionToken = normalized.session.decisionToken;

      if (!decisionToken) {
        throw new Error('Playback preflight did not provide a decision token');
      }

      const intentBody = buildLiveIntentBody(
        serviceRef,
        decisionToken,
        caps as any,
        liveMode,
        typeof params.requestedDuration === 'number' ? params.requestedDuration : undefined,
      );

      const sEpoch = allocateSessionEpoch(attempt.epoch);

      runtime.dispatch({
        type: 'normative.session.phase.changed',
        playbackEpoch: attempt.epoch,
        sessionEpoch: sEpoch,
        phase: 'starting',
        requestId: normalized.kind === 'playable' ? normalized.observability.requestId : undefined,
      });

      let startRes!: StartIntentResult;
      for (let retryCount = 0; ; retryCount++) {
        let foregroundWaiting = true;
        const intentReqController = new AbortController();
        const intentReqTimer = setTimeout(() => {
          foregroundWaiting = false;
          intentReqController.abort();
        }, httpRequestTimeoutMs);

        const postPromise = attempt.transport.postStartIntent({
          body: intentBody,
          signal: intentReqController.signal,
        });

        // Retain an explicit result-processing continuation on the original submitted POST:
        // If this request returns an accepted session after executeLiveStartup timed out or
        // abandoned it, route it through handleObsoleteSession without activating playback.
        postPromise.then(
          (lateRes) => {
            clearTimeout(intentReqTimer);
            if (foregroundWaiting) {
              return;
            }
            const lateSessionId = parseSessionId(lateRes?.data);
            if (lateSessionId) {
              handleObsoleteSession(lateSessionId, attempt.transport);
              flushPendingAdoptionCandidates();
            }
          },
          () => {
            clearTimeout(intentReqTimer);
          },
        ).catch(() => {
          // Explicitly handle any unexpected error on detached continuation
        });

        try {
          startRes = await raceWithSignal(
            postPromise,
            intentReqController.signal,
            'Start intent request timed out',
          );
        } catch (err) {
          foregroundWaiting = false;
          throw err;
        } finally {
          clearTimeout(intentReqTimer);
        }

        if (startRes.status !== 409 || retryCount >= MAX_LEASE_CONFLICT_RETRIES) {
          if (!isOkStatus(startRes.status) && startRes.status !== 409) {
            throw new PlaybackHttpError(
              `Start failed with status ${startRes.status}`,
              startRes.status,
              startRes.data,
              startRes.headers,
            );
          }
          if (startRes.status === 409) {
            throw new PlaybackHttpError(
              'Lease conflict retry budget exceeded (409)',
              409,
              startRes.data,
              startRes.headers,
            );
          }
          break;
        }

        if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
          if (attempt.settlementTimer) clearTimeout(attempt.settlementTimer);
          inFlightStarts.delete(attempt.attemptId);
          flushPendingAdoptionCandidates();
          return;
        }

        const retryAfterHeader = startRes.headers?.get ? startRes.headers.get('Retry-After') : null;
        const retrySec = retryAfterHeader ? parseInt(retryAfterHeader, 10) : NaN;
        const waitMs = Number.isFinite(retrySec) && retrySec > 0
          ? Math.min(retrySec * 1_000, MAX_LEASE_CONFLICT_WAIT_MS)
          : DEFAULT_LEASE_CONFLICT_WAIT_MS;

        await new Promise<void>((resolve) => {
          if (attempt.abortController.signal.aborted) {
            resolve();
            return;
          }
          const timer = setTimeout(resolve, waitMs);
          const onAbort = () => {
            clearTimeout(timer);
            resolve();
          };
          attempt.abortController.signal.addEventListener('abort', onAbort, { once: true });
        });

        if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
          if (attempt.settlementTimer) clearTimeout(attempt.settlementTimer);
          inFlightStarts.delete(attempt.attemptId);
          flushPendingAdoptionCandidates();
          return;
        }
      }

      // Start response returned!
      attempt.receivedResponse = true;
      if (attempt.settlementTimer) {
        clearTimeout(attempt.settlementTimer);
      }

      returnedSessionId = parseSessionId(startRes.data);

      if (isDisposed) {
        inFlightStarts.delete(attempt.attemptId);
        handleObsoleteSession(returnedSessionId, attempt.transport);
        return;
      }

      if (!returnedSessionId) {
        inFlightStarts.delete(attempt.attemptId);
        if (!attempt.cancelled && !attempt.settled) {
          attempt.settled = true;
          attempt.rejectPublic(
            new PlaybackHttpError(
              `Start failed with status ${startRes.status} or missing sessionId`,
              startRes.status,
              startRes.data,
              startRes.headers,
            ),
          );
        }
        flushPendingAdoptionCandidates();
        return;
      }

      // Invariant: Never adopt a session whose stop was already issued!
      if (stoppingSessionIds.has(returnedSessionId)) {
        inFlightStarts.delete(attempt.attemptId);
        if (!attempt.cancelled && !attempt.settled) {
          attempt.settled = true;
          attempt.rejectPublic(new Error(`Session ${returnedSessionId} was already revoked or stopping`));
        }
        flushPendingAdoptionCandidates();
        return;
      }

      // If attempt is obsolete (ineligible for adoption, cancelled, or settled)
      if (attempt.ineligibleForAdoption || attempt.cancelled || attempt.settled) {
        inFlightStarts.delete(attempt.attemptId);
        handleObsoleteSession(returnedSessionId, attempt.transport);
        flushPendingAdoptionCandidates();
        return;
      }

      // Case: Active attempt returned!
      if (pendingAdoptionCandidates.has(returnedSessionId)) {
        pendingAdoptionCandidates.delete(returnedSessionId);
      }
      const previousActiveSessionId = activeSessionId;
      const previousActiveTransport = activeSessionTransport;
      activeSessionId = returnedSessionId;
      activeSessionTransport = attempt.transport;
      if (previousActiveSessionId && previousActiveSessionId !== returnedSessionId) {
        stopHeartbeatSupervision();
        void retireAndStopSession(previousActiveSessionId, previousActiveTransport ?? undefined);
      }
      flushPendingAdoptionCandidates();

      // 3. Wait for readiness
      const readySession = await attempt.transport.waitForReady({
        sessionId: returnedSessionId,
        signal: attempt.abortController.signal,
      });

      if (isDisposed) {
        if (activeSessionId === returnedSessionId) {
          stopHeartbeatSupervision();
          activeSessionId = null;
          activeSessionTransport = null;
        }
        void retireAndStopSession(returnedSessionId, attempt.transport);
        return;
      }

      if (attempt.cancelled || attempt.ineligibleForAdoption || attempt.settled) {
        handleObsoleteSession(returnedSessionId, attempt.transport);
        return;
      }

      if (readySession.mode && readySession.mode !== 'LIVE') {
        throw new Error(
          `Live session ${returnedSessionId} returned unexpected mode ${readySession.mode}`,
        );
      }

      // Validate live lease contract before adoption
      const hasValidHeartbeat =
        typeof readySession.heartbeatIntervalSeconds === 'number' &&
        Number.isFinite(readySession.heartbeatIntervalSeconds) &&
        readySession.heartbeatIntervalSeconds > 0;
      const hasValidLeaseExpiry =
        typeof readySession.leaseExpiresAt === 'string' &&
        readySession.leaseExpiresAt.trim().length > 0;

      if (!hasValidHeartbeat || !hasValidLeaseExpiry) {
        throw new Error(
          `Live session ${returnedSessionId} readiness contract violation: invalid lease metadata`,
        );
      }

      runtime.dispatch({
        type: 'normative.session.phase.changed',
        playbackEpoch: attempt.epoch,
        sessionEpoch: sEpoch,
        phase: 'ready',
        requestId: readySession.requestId ?? (normalized.kind === 'playable' ? normalized.observability.requestId : undefined),
      });

      stopHeartbeatSupervision();
      const currentGen = ++heartbeatGeneration;
      heartbeatSessionId = returnedSessionId;
      const initialLease = readySession.leaseExpiresAt ?? null;

      runtime.dispatch({
        type: 'normative.session.lease.updated',
        epoch: attempt.epoch,
        sessionEpoch: sEpoch,
        leaseExpiresAt: initialLease,
        connectionLost: false,
      });

      heartbeatRuntime = createPlaybackHeartbeatRuntime({
        sessionId: returnedSessionId,
        heartbeatIntervalSeconds: readySession.heartbeatIntervalSeconds!,
        initialLeaseExpiresAt: initialLease,
        transport: attempt.transport,
        playbackEpoch: attempt.epoch,
        sessionEpoch: sEpoch,
        onLeaseUpdated: ({ leaseExpiresAt, connectionLost }) => {
          if (currentGen !== heartbeatGeneration || activeSessionId !== returnedSessionId) {
            return;
          }
          runtime.dispatch({
            type: 'normative.session.lease.updated',
            epoch: attempt.epoch,
            sessionEpoch: sEpoch,
            leaseExpiresAt,
            connectionLost,
          });
        },
        onFailure: (failure) => {
          if (currentGen !== heartbeatGeneration || activeSessionId !== returnedSessionId) {
            return;
          }
          runtime.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: attempt.epoch,
            failure: buildPlaybackFailure(
              {
                title: failure.message,
                status: failure.status,
                code: failure.code,
                retryable: failure.retryable,
              } as any,
              'native-host',
              {
                class: failure.failureClass,
                code: failure.code,
                message: failure.message,
                retryable: failure.retryable,
                recoverable: failure.recoverable,
                terminal: failure.terminal,
              },
            ),
            status: 'error',
          });
          if (failure.pauseMedia && executor) {
            executor({ type: 'command.media.pause' });
          }
        },
        onSessionSnapshot: (snapshot) => {
          if (currentGen !== heartbeatGeneration || activeSessionId !== returnedSessionId) {
            return;
          }
          options.onSessionSnapshot?.(snapshot);
        },
      });

      heartbeatRuntime.start();

      if (!attempt.settled) {
        attempt.settled = true;
        attempt.resolvePublic({
          status: 'ready',
          sessionId: returnedSessionId,
          session: readySession,
          contract: normalized.kind === 'playable' ? normalized : undefined,
        });
      }
    } catch (err) {
      if (attempt.settlementTimer) {
        clearTimeout(attempt.settlementTimer);
      }
      inFlightStarts.delete(attempt.attemptId);
      flushPendingAdoptionCandidates();

      if (returnedSessionId) {
        if (attempt.cancelled || attempt.ineligibleForAdoption || attempt.settled) {
          handleObsoleteSession(returnedSessionId, attempt.transport);
        } else {
          if (activeSessionId === returnedSessionId) {
            stopHeartbeatSupervision();
            activeSessionId = null;
            activeSessionTransport = null;
          }
          void retireAndStopSession(returnedSessionId, attempt.transport);
        }
      }

      if (!attempt.cancelled && !attempt.settled) {
        attempt.settled = true;
        const isTimeout =
          (err instanceof Error && err.message.includes('timed out')) ||
          (err instanceof DOMException && err.name === 'AbortError') ||
          (typeof err === 'object' && err !== null && (err as { name?: unknown }).name === 'AbortError');
        if (isTimeout) {
          attempt.cancelled = true;
          attempt.cancelReason = 'timeout';
          attempt.resolvePublic({ status: 'cancelled', reason: 'timeout' });
        } else {
          attempt.rejectPublic(err);
        }
      }
    } finally {
      inFlightStarts.delete(attempt.attemptId);
      flushPendingAdoptionCandidates();
    }
  }

  async function startLive(params: StartLiveParams): Promise<StartLiveResult> {
    if (isDisposed) {
      return { status: 'cancelled', reason: 'user_stop' };
    }

    const epoch = typeof params.epoch === 'number' ? params.epoch : allocatePlaybackEpoch();
    if (isStalePlaybackEpoch(epoch)) {
      return { status: 'cancelled', reason: 'superseded' };
    }

    // Invalidate previous attempt promptly if still running
    if (currentAttempt && !currentAttempt.settled && currentAttempt.epoch !== epoch) {
      if (currentAttempt.settlementTimer) {
        clearTimeout(currentAttempt.settlementTimer);
        currentAttempt.settlementTimer = null;
      }
      currentAttempt.cancelled = true;
      currentAttempt.cancelReason = 'superseded';
      currentAttempt.abortController.abort();
      currentAttempt.settled = true;
      currentAttempt.resolvePublic({ status: 'cancelled', reason: 'superseded' });
      currentAttempt = null;
    }

    const attemptId = `att-${epoch}-${Math.random().toString(36).slice(2, 9)}`;

    let resolvePublic!: (result: StartLiveResult) => void;
    let rejectPublic!: (err: unknown) => void;
    const publicPromise = new Promise<StartLiveResult>((resolve, reject) => {
      resolvePublic = resolve;
      rejectPublic = reject;
    });

    const attemptTransport = getLatestTransport();

    const attempt: InFlightStart = {
      attemptId,
      epoch,
      transport: attemptTransport,
      cancelled: false,
      cancelReason: null,
      ineligibleForAdoption: false,
      receivedResponse: false,
      settled: false,
      settlementTimer: null,
      resolvePublic,
      rejectPublic,
      abortController: new AbortController(),
    };

    attempt.settlementTimer = setTimeout(() => {
      attempt.cancelled = true;
      attempt.cancelReason = 'timeout';
      attempt.ineligibleForAdoption = true;
      inFlightStarts.delete(attemptId);
      if (currentAttempt === attempt) {
        currentAttempt = null;
      }
      attempt.abortController.abort();
      flushPendingAdoptionCandidates();
      if (!attempt.settled) {
        attempt.settled = true;
        attempt.resolvePublic({ status: 'cancelled', reason: 'timeout' });
      }
    }, overallStartBudgetMs);

    currentAttempt = attempt;
    inFlightStarts.set(attemptId, attempt);

    beginPlaybackAttempt(
      epoch,
      'LIVE',
      'starting',
      params.hasSessionIntent ?? true,
      params.explicitProfilePinned ?? false,
    );

    void executeLiveStartup(attempt, params);

    return publicPromise;
  }

  function stop(
    reason: PlaybackStopReason | string = 'user_stop',
    notifyClose: boolean = false,
  ): Promise<void> {
    const inFlight = inFlightStopPromises.get(playbackEpoch);
    if (inFlight) {
      return inFlight;
    }

    const currentStatus = runtime.getState().status;
    if (
      currentStatus === 'stopped' &&
      !activeSessionId &&
      !currentAttempt &&
      inFlightStarts.size === 0 &&
      stoppedEpochs.has(playbackEpoch)
    ) {
      return Promise.resolve();
    }

    // 0. Synchronously stop heartbeat supervision
    stopHeartbeatSupervision();

    // 1. Synchronously advance playbackEpoch to invalidate pending preparation
    playbackEpoch += 1;
    sessionEpoch = 0;
    const stopEpoch = playbackEpoch;
    stoppedEpochs.add(stopEpoch);

    // 2. Synchronously snapshot and clear activeSessionId and its transport
    const sessionToStop = activeSessionId;
    const transportForActiveSession = activeSessionTransport;
    activeSessionId = null;
    activeSessionTransport = null;

    // 3. Synchronously attempt invalidation & prompt promise settle
    if (currentAttempt && !currentAttempt.settled) {
      if (currentAttempt.settlementTimer) {
        clearTimeout(currentAttempt.settlementTimer);
        currentAttempt.settlementTimer = null;
      }
      currentAttempt.cancelled = true;
      currentAttempt.cancelReason = 'user_stop';
      currentAttempt.abortController.abort();
      currentAttempt.settled = true;
      currentAttempt.resolvePublic({ status: 'cancelled', reason: 'user_stop' });
    }
    currentAttempt = null;

    for (const start of inFlightStarts.values()) {
      if (start.settlementTimer) {
        clearTimeout(start.settlementTimer);
        start.settlementTimer = null;
      }
      if (!start.cancelled) {
        start.cancelled = true;
        start.cancelReason = 'user_stop';
        start.abortController.abort();
        if (!start.settled) {
          start.settled = true;
          start.resolvePublic({ status: 'cancelled', reason: 'user_stop' });
        }
      }
    }

    const doStop = async () => {
      // 4. Dispatch intent.stop.requested (media teardown commands)
      runtime.dispatch({
        type: 'intent.stop.requested',
        epoch: stopEpoch,
        reason: reason as PlaybackStopReason,
        notifyClose,
      });

      await runtime.waitForCommands();

      // 5. Clean up active session and flush unadopted candidates
      flushPendingAdoptionCandidates();

      if (sessionToStop) {
        await retireAndStopSession(sessionToStop, transportForActiveSession ?? getLatestTransport());
      }

      // 6. Dispatch normative.playback.stopped
      runtime.dispatch({
        type: 'normative.playback.stopped',
        epoch: stopEpoch,
      });
    };

    const promise = doStop().finally(() => {
      inFlightStopPromises.delete(stopEpoch);
    });
    inFlightStopPromises.set(stopEpoch, promise);
    return promise;
  }

  function activate(): void {
    isDisposed = false;
  }

  function dispose(): void {
    isDisposed = true;
    stopHeartbeatSupervision();
    playbackEpoch += 1;
    sessionEpoch = 0;
    stoppedEpochs.add(playbackEpoch);
    if (currentAttempt && !currentAttempt.settled) {
      currentAttempt.cancelled = true;
      currentAttempt.ineligibleForAdoption = true;
      currentAttempt.cancelReason = 'user_stop';
      currentAttempt.abortController.abort();
      currentAttempt.settled = true;
      currentAttempt.resolvePublic({ status: 'cancelled', reason: 'user_stop' });
    }
    for (const start of inFlightStarts.values()) {
      if (start.settlementTimer) {
        clearTimeout(start.settlementTimer);
      }
      start.cancelled = true;
      start.ineligibleForAdoption = true;
      start.abortController.abort();
      if (!start.settled) {
        start.settled = true;
        start.resolvePublic({ status: 'cancelled', reason: 'user_stop' });
      }
    }
    inFlightStarts.clear();
    flushPendingAdoptionCandidates();

    if (activeSessionId) {
      void retireAndStopSession(activeSessionId, activeSessionTransport ?? undefined);
      activeSessionId = null;
      activeSessionTransport = null;
    }
  }

  return {
    getState: runtime.getState,
    subscribe: runtime.subscribe,
    dispatch: runtime.dispatch,
    setCommandExecutor(exec) {
      executor = exec;
      runtime.setCommandExecutor(exec);
    },

    allocatePlaybackEpoch,
    allocateSessionEpoch,
    beginPlaybackAttempt,
    markPlaybackStopped,
    isStalePlaybackEpoch,
    isStaleSessionEpoch,
    getEpoch() {
      return playbackEpoch;
    },

    startLive,
    stop,
    activate,
    dispose,

    getActiveSessionId() {
      return activeSessionId;
    },
    getInFlightStartsCount() {
      return inFlightStarts.size;
    },
    getStoppingSessionIds() {
      return stoppingSessionIds;
    },
    getPendingAdoptionCandidatesCount() {
      return pendingAdoptionCandidates.size;
    },
    getHeartbeatSessionId() {
      return heartbeatSessionId;
    },
    isHeartbeatSupervising() {
      return heartbeatRuntime?.isSupervising() ?? false;
    },
  };
}
