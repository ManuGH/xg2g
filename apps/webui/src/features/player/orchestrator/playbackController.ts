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
import type {
  LiveSessionTransport,
  SessionReadyResult,
  StartIntentResult,
} from './liveSessionTransport';
import { normalizePlaybackInfo } from '../contracts/normalizePlaybackInfo';
import { buildLiveIntentBody } from './startupHelpers';
import type { PlayerStatus } from '../../../types/v3-player';

export interface StartLiveParams {
  serviceRef: string;
  capabilities?: unknown;
  profileHeaders?: Record<string, string>;
  requestedDuration?: number | null;
  hasSessionIntent?: boolean;
  explicitProfilePinned?: boolean;
}

export type StartLiveResult =
  | { status: 'ready'; sessionId: string; session: SessionReadyResult }
  | { status: 'cancelled'; reason: 'superseded' | 'user_stop' };

export interface PlaybackControllerOptions {
  transport: LiveSessionTransport;
  createInitialState: () => PlaybackDomainState;
  executeCommand?: PlaybackCommandExecutor;
  startSettlementTimeoutMs?: number; // default 10_000ms
  stopRequestTimeoutMs?: number;     // default 3_000ms
}

interface InFlightStart {
  attemptId: string;
  epoch: number;
  cancelled: boolean;
  cancelReason: 'superseded' | 'user_stop' | null;
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
  dispose(): void;

  // Observability & inspection for tests
  getActiveSessionId(): string | null;
  getInFlightStartsCount(): number;
  getStoppingSessionIds(): ReadonlySet<string>;
  getPendingAdoptionCandidatesCount(): number;
}

export const DEFAULT_START_SETTLEMENT_TIMEOUT_MS = 10_000;
export const DEFAULT_STOP_REQUEST_TIMEOUT_MS = 3_000;
export const MAX_LEASE_CONFLICT_RETRIES = 3;
export const DEFAULT_LEASE_CONFLICT_WAIT_MS = 1_000;
export const MAX_LEASE_CONFLICT_WAIT_MS = 5_000;

export function createPlaybackController(
  options: PlaybackControllerOptions,
): PlaybackController {
  const {
    transport,
    createInitialState,
    executeCommand = () => {},
    startSettlementTimeoutMs = DEFAULT_START_SETTLEMENT_TIMEOUT_MS,
    stopRequestTimeoutMs = DEFAULT_STOP_REQUEST_TIMEOUT_MS,
  } = options;

  let executor: PlaybackCommandExecutor | null = executeCommand;

  const runtime: PlaybackMachineRuntime = createPlaybackMachineRuntime(
    createInitialState,
    (command) => {
      if (executor) {
        executor(command);
      }
    },
  );

  let playbackEpoch = runtime.getState().epoch.playback;
  let sessionEpoch = runtime.getState().epoch.session;

  let activeSessionId: string | null = null;
  let currentAttempt: InFlightStart | null = null;
  const inFlightStarts = new Map<string, InFlightStart>();
  const stoppingSessionIds = new Set<string>();
  const activeStopPromises = new Map<string, Promise<void>>();
  const inFlightStopPromises = new Map<number, Promise<void>>();
  const pendingAdoptionCandidates = new Map<string, { sessionId: string; heldAt: number }>();

  let isDisposed = false;

  function hasEligibleInFlightStarts(): boolean {
    for (const start of inFlightStarts.values()) {
      if (!start.ineligibleForAdoption && !start.cancelled && !start.receivedResponse) {
        return true;
      }
    }
    return false;
  }

  function retireAndStopSession(sessionId: string): Promise<void> {
    if (stoppingSessionIds.has(sessionId)) {
      return activeStopPromises.get(sessionId) ?? Promise.resolve();
    }

    stoppingSessionIds.add(sessionId);
    pendingAdoptionCandidates.delete(sessionId);

    const stopController = new AbortController();
    let timer: ReturnType<typeof setTimeout> | null = null;

    const promise = new Promise<void>((resolve) => {
      timer = setTimeout(() => {
        stopController.abort();
        resolve();
      }, stopRequestTimeoutMs);

      Promise.resolve(
        transport.postStopIntent({ sessionId, signal: stopController.signal }),
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

  function handleObsoleteSession(sessionId: string | null | undefined): void {
    if (!sessionId) {
      return;
    }

    if (stoppingSessionIds.has(sessionId)) {
      return;
    }

    if (sessionId === activeSessionId) {
      return;
    }

    if (hasEligibleInFlightStarts()) {
      pendingAdoptionCandidates.set(sessionId, {
        sessionId,
        heldAt: Date.now(),
      });
      return;
    }

    void retireAndStopSession(sessionId);
  }

  function flushPendingAdoptionCandidates(): void {
    if (hasEligibleInFlightStarts()) {
      return;
    }
    for (const sessionId of Array.from(pendingAdoptionCandidates.keys())) {
      if (sessionId !== activeSessionId) {
        void retireAndStopSession(sessionId);
      }
    }
    pendingAdoptionCandidates.clear();
  }

  function allocatePlaybackEpoch(): number {
    playbackEpoch += 1;
    sessionEpoch = 0;
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
    runtime.dispatch({
      type: 'normative.playback.attempt.started',
      epoch,
      playbackMode: nextPlaybackMode,
      status: nextStatus,
      requestedDuration: null,
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
    return epoch !== playbackEpoch;
  }

  function isStaleSessionEpoch(pEpoch: number, sEpoch: number): boolean {
    return pEpoch !== playbackEpoch || sEpoch !== sessionEpoch;
  }

  async function executeLiveStartup(
    attempt: InFlightStart,
    params: StartLiveParams,
  ): Promise<void> {
    const { serviceRef, capabilities, profileHeaders } = params;
    let returnedSessionId: string | null = null;

    try {
      // 1. Preflight
      const preflightRes = await transport.fetchStreamInfo({
        serviceRef,
        capabilities,
        profileHeaders,
        signal: attempt.abortController.signal,
      });

      if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
        if (attempt.settlementTimer) clearTimeout(attempt.settlementTimer);
        inFlightStarts.delete(attempt.attemptId);
        flushPendingAdoptionCandidates();
        return;
      }

      if (preflightRes.status !== 200) {
        throw new Error(`Preflight failed with status ${preflightRes.status}`);
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

      const intentReqController = new AbortController();
      const intentReqTimer = setTimeout(() => {
        intentReqController.abort();
      }, startSettlementTimeoutMs);

      let startRes: StartIntentResult;
      try {
        startRes = await transport.postStartIntent({
          body: intentBody,
          signal: intentReqController.signal,
        });
      } finally {
        clearTimeout(intentReqTimer);
      }

      // Handle 409 Conflict with one retry after 2s backoff
      if (startRes.status === 409) {
        if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
          if (attempt.settlementTimer) clearTimeout(attempt.settlementTimer);
          inFlightStarts.delete(attempt.attemptId);
          flushPendingAdoptionCandidates();
          return;
        }

        const retryTimer = setTimeout(() => {}, 2000);
        try {
          await new Promise<void>((resolve, reject) => {
            const t = setTimeout(resolve, 2000);
            attempt.abortController.signal.addEventListener(
              'abort',
              () => {
                clearTimeout(t);
                reject(new DOMException('Aborted', 'AbortError'));
              },
              { once: true },
            );
          });

          if (attempt.cancelled || attempt.ineligibleForAdoption || isDisposed) {
            inFlightStarts.delete(attempt.attemptId);
            flushPendingAdoptionCandidates();
            return;
          }

          startRes = await transport.postStartIntent({
            body: intentBody,
            signal: attempt.abortController.signal,
          });
        } finally {
          clearTimeout(retryTimer);
        }
      }

      // Start response returned!
      attempt.receivedResponse = true;
      if (attempt.settlementTimer) {
        clearTimeout(attempt.settlementTimer);
      }

      returnedSessionId = (startRes.data as any)?.sessionId?.trim() || null;

      if (isDisposed) {
        inFlightStarts.delete(attempt.attemptId);
        handleObsoleteSession(returnedSessionId);
        return;
      }

      if (!returnedSessionId) {
        inFlightStarts.delete(attempt.attemptId);
        if (!attempt.cancelled && !attempt.settled) {
          attempt.settled = true;
          attempt.rejectPublic(new Error(`Start failed with status ${startRes.status} or missing sessionId`));
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
        handleObsoleteSession(returnedSessionId);
        flushPendingAdoptionCandidates();
        return;
      }

      // Case: Active attempt returned!
      if (pendingAdoptionCandidates.has(returnedSessionId)) {
        pendingAdoptionCandidates.delete(returnedSessionId);
      }
      const previousActiveSessionId = activeSessionId;
      activeSessionId = returnedSessionId;
      if (previousActiveSessionId && previousActiveSessionId !== returnedSessionId) {
        void retireAndStopSession(previousActiveSessionId);
      }
      flushPendingAdoptionCandidates();

      // 3. Wait for readiness
      const readySession = await transport.waitForReady({
        sessionId: returnedSessionId,
        signal: attempt.abortController.signal,
      });

      if (isDisposed) {
        if (activeSessionId === returnedSessionId) {
          activeSessionId = null;
        }
        void retireAndStopSession(returnedSessionId);
        return;
      }

      if (attempt.cancelled || attempt.ineligibleForAdoption || attempt.settled) {
        handleObsoleteSession(returnedSessionId);
        return;
      }

      if (!attempt.settled) {
        attempt.settled = true;
        attempt.resolvePublic({
          status: 'ready',
          sessionId: returnedSessionId,
          session: readySession,
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
          handleObsoleteSession(returnedSessionId);
        } else {
          if (activeSessionId === returnedSessionId) {
            activeSessionId = null;
          }
          void retireAndStopSession(returnedSessionId);
        }
      }

      if (!attempt.cancelled && !attempt.settled) {
        attempt.settled = true;
        attempt.rejectPublic(err);
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

    const epoch = allocatePlaybackEpoch();

    // Invalidate previous attempt promptly
    if (currentAttempt && !currentAttempt.settled) {
      currentAttempt.cancelled = true;
      currentAttempt.cancelReason = 'superseded';
      currentAttempt.abortController.abort();
      currentAttempt.settled = true;
      currentAttempt.resolvePublic({ status: 'cancelled', reason: 'superseded' });
    }

    const attemptId = `att-${epoch}-${Math.random().toString(36).slice(2, 9)}`;

    let resolvePublic!: (result: StartLiveResult) => void;
    let rejectPublic!: (err: unknown) => void;
    const publicPromise = new Promise<StartLiveResult>((resolve, reject) => {
      resolvePublic = resolve;
      rejectPublic = reject;
    });

    const attempt: InFlightStart = {
      attemptId,
      epoch,
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
      attempt.ineligibleForAdoption = true;
      inFlightStarts.delete(attemptId);
      if (currentAttempt === attempt) {
        currentAttempt = null;
      }
      flushPendingAdoptionCandidates();
      if (!attempt.settled) {
        attempt.settled = true;
        attempt.resolvePublic({ status: 'cancelled', reason: 'superseded' });
      }
    }, startSettlementTimeoutMs);

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

  async function stop(
    reason: PlaybackStopReason | string = 'user_stop',
    notifyClose: boolean = false,
  ): Promise<void> {
    const stopEpoch = playbackEpoch;
    const existing = inFlightStopPromises.get(stopEpoch);
    if (existing) {
      return existing;
    }

    const currentStatus = runtime.getState().status;
    if ((currentStatus === 'stopped' || currentStatus === 'idle') && !activeSessionId && !currentAttempt) {
      return Promise.resolve();
    }

    const doStop = async () => {
      // 1. Synchronous attempt invalidation & prompt promise settle
      if (currentAttempt && !currentAttempt.settled) {
        currentAttempt.cancelled = true;
        currentAttempt.cancelReason = 'user_stop';
        currentAttempt.abortController.abort();
        currentAttempt.settled = true;
        currentAttempt.resolvePublic({ status: 'cancelled', reason: 'user_stop' });
      }
      currentAttempt = null;

      for (const start of inFlightStarts.values()) {
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

      // 2. Dispatch intent.stop.requested (media teardown commands)
      runtime.dispatch({
        type: 'intent.stop.requested',
        epoch: stopEpoch,
        reason: reason as PlaybackStopReason,
        notifyClose,
      });

      // 3. Clean up active session and flush unadopted candidates
      const sessionToStop = activeSessionId;
      activeSessionId = null;

      flushPendingAdoptionCandidates();

      if (sessionToStop) {
        await retireAndStopSession(sessionToStop);
      }

      // 4. Dispatch normative.playback.stopped
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

  function dispose(): void {
    isDisposed = true;
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
      void retireAndStopSession(activeSessionId);
      activeSessionId = null;
    }
    executor = null;
    runtime.setCommandExecutor(null);
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
  };
}
