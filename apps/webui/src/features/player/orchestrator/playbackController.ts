// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
  createPlaybackMachineRuntime,
  type PlaybackCommandExecutor,
  type PlaybackMachineRuntime,
} from './playbackMachineRuntime';
import type {
  PlaybackCommand,
  PlaybackDomainState,
  PlaybackMachineEvent,
  PlaybackRetryResult,
  PlaybackRetryTarget,
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
export type { PlaybackRetryTarget, PlaybackRetryResult } from './playbackTypes';

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

export interface AutoFallbackRestartTarget {
  kind: 'live' | 'vod' | 'src';
  serviceRef?: string;
  recordingId?: string;
  srcUrl?: string;
  explicitProfile?: string;
}

export type ScheduleAutoFallbackCommand = Extract<
  PlaybackCommand,
  { type: 'command.playback.schedule_auto_fallback' }
>;

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
  params?: StartLiveParams;
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

  // Recovery fallback timers & retry sequencing
  scheduleAutoFallback(
    command: ScheduleAutoFallbackCommand,
    target?: AutoFallbackRestartTarget,
  ): void;
  cancelAutoFallback(epoch?: number): void;
  hasScheduledAutoFallback(epoch?: number): boolean;

  retry(target?: PlaybackRetryTarget): Promise<PlaybackRetryResult>;
  isRetryInFlight(): boolean;
  cancelRetry(
    reason?:
      | 'superseded'
      | 'user_stop'
      | 'disposed'
      | 'terminal_auth'
      | 'missing_target'
      | 'error',
  ): void;

  // Live session lifecycle
  startLive(params: StartLiveParams): Promise<StartLiveResult>;
  stop(reason?: PlaybackStopReason | string, notifyClose?: boolean): Promise<void>;
  updateTransport(newTransport: LiveSessionTransport): void;
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

  let isDisposed = false;
  let playbackEpoch = 0;
  let sessionEpoch = 0;
  const stoppedEpochs = new Set<number>();
  const scheduledCommands = new WeakSet<PlaybackCommand>();
  let currentlyHandlingScheduleCommand: ScheduleAutoFallbackCommand | null = null;

  interface ActiveRetryOperation {
    opId: number;
    phase: 'stopping' | 'restarting';
    initialEpoch: number;
    restartEpoch: number | null;
    target: AutoFallbackRestartTarget;
    cancelled: boolean;
    cancelReason:
      | 'superseded'
      | 'user_stop'
      | 'disposed'
      | 'terminal_auth'
      | 'missing_target'
      | 'error'
      | null;
    publicPromise: Promise<PlaybackRetryResult>;
    resolvePublic: (result: PlaybackRetryResult) => void;
    stopPromise: Promise<void> | null;
  }

  let activeRetry: ActiveRetryOperation | null = null;
  let nextRetryOpId = 1;

  function cancelActiveRetry(
    reason:
      | 'superseded'
      | 'user_stop'
      | 'disposed'
      | 'terminal_auth'
      | 'missing_target'
      | 'error',
  ): void {
    if (!activeRetry || activeRetry.cancelled) {
      return;
    }
    const op = activeRetry;
    activeRetry = null;
    op.cancelled = true;
    op.cancelReason = reason;
    op.resolvePublic({ status: 'cancelled', reason });
  }

  interface ActiveFallback {
    timer: ReturnType<typeof setTimeout>;
    target: AutoFallbackRestartTarget;
    command: ScheduleAutoFallbackCommand;
  }
  const activeFallbacks = new Map<number, ActiveFallback>();

  function cancelAutoFallback(epoch?: number): void {
    if (epoch !== undefined) {
      const fallback = activeFallbacks.get(epoch);
      if (fallback) {
        clearTimeout(fallback.timer);
        activeFallbacks.delete(epoch);
      }
    } else {
      for (const fallback of activeFallbacks.values()) {
        clearTimeout(fallback.timer);
      }
      activeFallbacks.clear();
    }
  }

  function hasScheduledAutoFallback(epoch?: number): boolean {
    if (epoch !== undefined) {
      return activeFallbacks.has(epoch);
    }
    return activeFallbacks.size > 0;
  }

  function scheduleAutoFallback(
    command: ScheduleAutoFallbackCommand,
    target?: AutoFallbackRestartTarget,
  ): void {
    scheduledCommands.add(command);
    if (
      currentlyHandlingScheduleCommand &&
      currentlyHandlingScheduleCommand.epoch === command.epoch
    ) {
      scheduledCommands.add(currentlyHandlingScheduleCommand);
    }

    if (isDisposed) {
      return;
    }
    if (isStalePlaybackEpoch(command.epoch)) {
      return;
    }

    // Duplicate replacement policy: cancel any existing pending fallback for this epoch
    const existing = activeFallbacks.get(command.epoch);
    if (existing) {
      clearTimeout(existing.timer);
      activeFallbacks.delete(command.epoch);
    }

    let resolvedTarget: AutoFallbackRestartTarget | null = null;

    if (target) {
      const explicitProfile = (command.profile ?? target.explicitProfile) || undefined;
      if (target.kind === 'live' && target.serviceRef?.trim()) {
        resolvedTarget = {
          kind: 'live',
          serviceRef: target.serviceRef.trim(),
          explicitProfile,
        };
      } else if (target.kind === 'vod' && target.recordingId?.trim()) {
        resolvedTarget = {
          kind: 'vod',
          recordingId: target.recordingId.trim(),
          explicitProfile,
        };
      } else if (target.kind === 'src' && target.srcUrl?.trim()) {
        resolvedTarget = {
          kind: 'src',
          srcUrl: target.srcUrl.trim(),
          explicitProfile,
        };
      }
    }

    if (!resolvedTarget) {
      const currentMode = runtime.getState().playbackMode;
      if (
        currentMode === 'LIVE' &&
        currentAttempt &&
        currentAttempt.epoch === command.epoch &&
        currentAttempt.params?.serviceRef?.trim()
      ) {
        resolvedTarget = {
          kind: 'live',
          serviceRef: currentAttempt.params.serviceRef.trim(),
          explicitProfile: (command.profile ?? undefined) || undefined,
        };
      }
    }

    if (!resolvedTarget) {
      // Missing required source identity: do not manufacture unexecutable restart
      return;
    }

    const finalTarget = resolvedTarget;
    const timer = setTimeout(() => {
      activeFallbacks.delete(command.epoch);

      if (isDisposed) {
        return;
      }
      if (isStalePlaybackEpoch(command.epoch)) {
        return;
      }

      runtime.dispatch({
        type: 'intent.start.requested',
        epoch: command.epoch,
        kind: finalTarget.kind,
        serviceRef: finalTarget.serviceRef,
        recordingId: finalTarget.recordingId,
        srcUrl: finalTarget.srcUrl,
        explicitProfile: finalTarget.explicitProfile,
      });
    }, command.delayMs);

    activeFallbacks.set(command.epoch, {
      timer,
      target: finalTarget,
      command,
    });
  }

  let executor: PlaybackCommandExecutor | null = executeCommand;

  const handleCommand: PlaybackCommandExecutor = (command) => {
    let result: unknown;
    if (command.type === 'command.playback.schedule_auto_fallback') {
      const prevHandling = currentlyHandlingScheduleCommand;
      currentlyHandlingScheduleCommand = command;
      try {
        if (executor) {
          result = executor(command);
        }
        if (!scheduledCommands.has(command)) {
          scheduleAutoFallback(command);
        }
      } finally {
        currentlyHandlingScheduleCommand = prevHandling;
      }
      return result;
    }

    if (executor) {
      result = executor(command);
    }
    return result;
  };

  const runtime: PlaybackMachineRuntime = createPlaybackMachineRuntime(
    createInitialState,
    handleCommand,
  );

  function dispatch(event: PlaybackMachineEvent): void {
    if (event.type === 'normative.playback.failure.raised') {
      const failure = event.failure;
      const isTerminalAuth =
        failure.class === 'auth' ||
        failure.terminal === true ||
        failure.code === 'SESSION_FORBIDDEN' ||
        failure.code === 'SESSION_UNAUTHORIZED' ||
        failure.status === 401 ||
        failure.status === 403;

      if (isTerminalAuth) {
        if (
          !isStalePlaybackEpoch(event.epoch) &&
          event.epoch === playbackEpoch
        ) {
          cancelAutoFallback(event.epoch);
          stoppedEpochs.add(event.epoch);
          stopHeartbeatSupervision();
        }

        if (activeRetry && (event.epoch === undefined || event.epoch >= activeRetry.initialEpoch)) {
          cancelActiveRetry('terminal_auth');
        }
      }
    }
    runtime.dispatch(event);
  }

  playbackEpoch = runtime.getState().epoch.playback;
  sessionEpoch = runtime.getState().epoch.session;

  let activeSessionId: string | null = null;
  let activeSessionTransport: LiveSessionTransport | null = null;
  let currentAttempt: InFlightStart | null = null;
  const inFlightStarts = new Map<string, InFlightStart>();
  const stoppingSessionIds = new Set<string>();
  const activeStopPromises = new Map<string, Promise<void>>();
  const inFlightStopPromises = new Map<number, Promise<void>>();
  let currentTransport: LiveSessionTransport =
    typeof options.transport === 'function'
      ? (options.transport as () => LiveSessionTransport)()
      : options.getTransport
        ? options.getTransport()
        : options.transport!;

  let explicitTransportUpdated = false;

  const getLatestTransport = (): LiveSessionTransport => {
    if (typeof currentTransport === 'function') {
      return (currentTransport as () => LiveSessionTransport)();
    }
    if (explicitTransportUpdated) {
      return currentTransport;
    }
    if (options.getTransport) {
      return options.getTransport();
    }
    return currentTransport;
  };

  const pendingAdoptionCandidates = new Map<
    string,
    { sessionId: string; heldAt: number; transport: LiveSessionTransport }
  >();

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

  function cancelInFlightAttempt(
    attempt: InFlightStart,
    reason: 'superseded' | 'user_stop' | 'timeout' = 'user_stop',
  ): void {
    if (attempt.settled) {
      return;
    }
    attempt.cancelled = true;
    attempt.cancelReason = reason;
    if (attempt.settlementTimer) {
      clearTimeout(attempt.settlementTimer);
      attempt.settlementTimer = null;
    }
    attempt.abortController.abort();
    attempt.settled = true;
    attempt.resolvePublic({ status: 'cancelled', reason });
  }

  function allocatePlaybackEpoch(): number {
    if (activeRetry) {
      if (activeRetry.phase === 'restarting' && activeRetry.restartEpoch === null) {
        activeRetry.restartEpoch = playbackEpoch + 1;
      } else {
        cancelActiveRetry('superseded');
      }
    }

    playbackEpoch += 1;
    sessionEpoch = 0;
    cancelAutoFallback();
    options.onAttemptStarted?.(playbackEpoch);

    // Invalidate any in-flight live start immediately and idempotently
    if (currentAttempt && !currentAttempt.settled) {
      cancelInFlightAttempt(currentAttempt, 'superseded');
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

    if (activeRetry) {
      if (activeRetry.phase === 'restarting') {
        if (epoch !== activeRetry.restartEpoch) {
          cancelActiveRetry('superseded');
        }
      } else {
        cancelActiveRetry('superseded');
      }
    }

    // Fence: a stale attempt must never mutate state or retire a newer session!
    if (isStalePlaybackEpoch(epoch)) {
      return;
    }

    cancelAutoFallback(epoch);

    // If switching to a non-browser-Live mode, retire any active Live session!
    if (nextPlaybackMode !== 'LIVE' || !hasSessionIntent) {
      if (currentAttempt && !currentAttempt.settled) {
        cancelInFlightAttempt(currentAttempt, 'superseded');
        currentAttempt = null;
      }
      for (const start of Array.from(inFlightStarts.values())) {
        cancelInFlightAttempt(start, 'superseded');
      }
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
    if (activeRetry && activeRetry.phase === 'restarting') {
      cancelActiveRetry('superseded');
    }
    stoppedEpochs.add(epoch);
    cancelAutoFallback(epoch);
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

      const pinnedBase = attempt.transport.apiBase;
      const latestTransport = getLatestTransport();
      const isSameEndpoint = !pinnedBase || !latestTransport.apiBase || pinnedBase === latestTransport.apiBase;
      const resolvedTransport = isSameEndpoint ? latestTransport : attempt.transport;

      activeSessionId = returnedSessionId;
      activeSessionTransport = resolvedTransport;
      attempt.transport = resolvedTransport;

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

      // Reconcile the latest committed same-endpoint credentials when supervision is created:
      const effectivePinnedBase = attempt.transport.apiBase;
      const latestCommittedTransport = getLatestTransport();
      const effectiveTransport =
        !effectivePinnedBase ||
        !latestCommittedTransport.apiBase ||
        effectivePinnedBase === latestCommittedTransport.apiBase
          ? latestCommittedTransport
          : attempt.transport;

      activeSessionTransport = effectiveTransport;
      attempt.transport = effectiveTransport;

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
        transport: effectiveTransport,
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
          const currentPlaybackEpoch = runtime.getState().epoch.playback;
          const isTerminalAuth =
            failure.terminal ||
            failure.code === 'SESSION_UNAUTHORIZED' ||
            failure.code === 'SESSION_FORBIDDEN' ||
            failure.status === 401 ||
            failure.status === 403;

          const hasInFlightNewAttempt = Boolean(
            (currentAttempt && currentAttempt.epoch !== attempt.epoch) ||
            Array.from(inFlightStarts.values()).some((s) => s.epoch !== attempt.epoch)
          );

          if (isTerminalAuth) {
            stoppedEpochs.add(currentPlaybackEpoch);
            stoppedEpochs.add(playbackEpoch);
            stoppedEpochs.add(attempt.epoch);
            if (currentAttempt) {
              stoppedEpochs.add(currentAttempt.epoch);
              cancelInFlightAttempt(currentAttempt, 'superseded');
              currentAttempt = null;
            }
            for (const start of Array.from(inFlightStarts.values())) {
              stoppedEpochs.add(start.epoch);
              cancelInFlightAttempt(start, 'superseded');
            }
            stopHeartbeatSupervision();
            cancelAutoFallback();
            runtime.dispatch({
              type: 'normative.playback.failure.raised',
              epoch: currentPlaybackEpoch,
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
            return;
          }

          const isConfirmedDead =
            failure.status === 404 ||
            failure.status === 410 ||
            failure.code === 'SESSION_NOT_FOUND' ||
            failure.code === 'SESSION_EXPIRED';

          if (hasInFlightNewAttempt) {
            const failedSessionId = activeSessionId;
            const failedTransport = activeSessionTransport ?? getLatestTransport();
            stopHeartbeatSupervision();
            activeSessionId = null;
            activeSessionTransport = null;
            if (!isConfirmedDead && failedSessionId) {
              handleObsoleteSession(failedSessionId, failedTransport);
            }
            if (failure.pauseMedia && executor) {
              executor({ type: 'command.media.pause' });
            }
            return;
          }

          stopHeartbeatSupervision();
          runtime.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: currentPlaybackEpoch,
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
      cancelInFlightAttempt(currentAttempt, 'superseded');
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
      params,
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
    cancelActiveRetry('user_stop');
    return executeStopInternal(reason, notifyClose);
  }

  function executeStopInternal(
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

    // 0. Synchronously stop heartbeat supervision and auto-fallback timers
    stopHeartbeatSupervision();
    cancelAutoFallback();

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
      cancelInFlightAttempt(currentAttempt, 'user_stop');
      currentAttempt = null;
    }
    for (const start of Array.from(inFlightStarts.values())) {
      cancelInFlightAttempt(start, 'user_stop');
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

  function resolveRetryTarget(
    target?: PlaybackRetryTarget,
  ): AutoFallbackRestartTarget | null {
    if (target) {
      const explicitProfile = target.explicitProfile?.trim() || undefined;
      if (target.kind === 'vod' && target.recordingId?.trim()) {
        return {
          kind: 'vod',
          recordingId: target.recordingId.trim(),
          explicitProfile,
        };
      } else if (target.kind === 'src' && target.srcUrl?.trim()) {
        return {
          kind: 'src',
          srcUrl: target.srcUrl.trim(),
          explicitProfile,
        };
      } else if (target.kind === 'live' && target.serviceRef?.trim()) {
        return {
          kind: 'live',
          serviceRef: target.serviceRef.trim(),
          explicitProfile,
        };
      }
    }

    const currentMode = runtime.getState().playbackMode;
    if (
      currentMode === 'LIVE' &&
      currentAttempt &&
      currentAttempt.params?.serviceRef?.trim()
    ) {
      return {
        kind: 'live',
        serviceRef: currentAttempt.params.serviceRef.trim(),
        explicitProfile: undefined,
      };
    }

    return null;
  }

  function isSameRetryTarget(
    a: AutoFallbackRestartTarget,
    b: AutoFallbackRestartTarget,
  ): boolean {
    if (a.kind !== b.kind) return false;
    if (a.explicitProfile !== b.explicitProfile) return false;
    if (a.kind === 'live') {
      return a.serviceRef === b.serviceRef;
    }
    if (a.kind === 'vod') {
      return a.recordingId === b.recordingId;
    }
    if (a.kind === 'src') {
      return a.srcUrl === b.srcUrl;
    }
    return false;
  }

  function isRetryInFlight(): boolean {
    return Boolean(activeRetry && !activeRetry.cancelled);
  }

  function cancelRetry(
    reason:
      | 'superseded'
      | 'user_stop'
      | 'disposed'
      | 'terminal_auth'
      | 'missing_target'
      | 'error' = 'superseded',
  ): void {
    cancelActiveRetry(reason);
  }

  function retry(target?: PlaybackRetryTarget): Promise<PlaybackRetryResult> {
    if (isDisposed) {
      return Promise.resolve({ status: 'cancelled', reason: 'disposed' });
    }

    const resolvedTarget = resolveRetryTarget(target);
    if (!resolvedTarget) {
      return Promise.resolve({ status: 'cancelled', reason: 'missing_target' });
    }

    // Coalescing check: if activeRetry matches target, coalesce
    if (activeRetry && !activeRetry.cancelled && isSameRetryTarget(activeRetry.target, resolvedTarget)) {
      return activeRetry.publicPromise;
    }

    // Cancel existing active retry if target changed
    cancelActiveRetry('superseded');

    let resolvePublic!: (result: PlaybackRetryResult) => void;
    const publicPromise = new Promise<PlaybackRetryResult>((resolve) => {
      resolvePublic = resolve;
    });

    const op: ActiveRetryOperation = {
      opId: nextRetryOpId++,
      phase: 'stopping',
      initialEpoch: playbackEpoch,
      restartEpoch: null,
      target: resolvedTarget,
      cancelled: false,
      cancelReason: null,
      publicPromise,
      resolvePublic,
      stopPromise: null,
    };

    activeRetry = op;
    void runRetryWorker(op);

    return publicPromise;
  }

  async function runRetryWorker(op: ActiveRetryOperation): Promise<void> {
    try {
      const stopPromise = executeStopInternal('auto_recovery_restart', false);
      op.stopPromise = stopPromise;
      await stopPromise;
    } catch (_err) {
      if (activeRetry === op && !op.cancelled) {
        activeRetry = null;
        op.cancelled = true;
        op.cancelReason = 'error';
        op.resolvePublic({ status: 'cancelled', reason: 'error' });
      }
      return;
    }

    // If cancelled or superseded during stop teardown
    if (op.cancelled || activeRetry !== op || isDisposed) {
      return;
    }

    op.phase = 'restarting';

    try {
      runtime.dispatch({
        type: 'intent.start.requested',
        epoch: playbackEpoch,
        kind: op.target.kind,
        serviceRef: op.target.serviceRef,
        recordingId: op.target.recordingId,
        srcUrl: op.target.srcUrl,
        explicitProfile: op.target.explicitProfile,
      });

      if (activeRetry === op && !op.cancelled) {
        activeRetry = null;
        if (op.restartEpoch !== null) {
          op.resolvePublic({ status: 'restarted', epoch: op.restartEpoch });
        } else {
          op.cancelled = true;
          op.cancelReason = 'error';
          op.resolvePublic({ status: 'cancelled', reason: 'error' });
        }
      }
    } catch (_err) {
      if (activeRetry === op && !op.cancelled) {
        activeRetry = null;
        op.cancelled = true;
        op.cancelReason = 'error';
        op.resolvePublic({ status: 'cancelled', reason: 'error' });
      }
    }
  }

  function activate(): void {
    isDisposed = false;
  }

  function dispose(): void {
    isDisposed = true;
    cancelActiveRetry('disposed');
    stopHeartbeatSupervision();
    cancelAutoFallback();
    playbackEpoch += 1;
    sessionEpoch = 0;
    stoppedEpochs.add(playbackEpoch);
    if (currentAttempt && !currentAttempt.settled) {
      currentAttempt.ineligibleForAdoption = true;
      cancelInFlightAttempt(currentAttempt, 'user_stop');
      currentAttempt = null;
    }
    for (const start of Array.from(inFlightStarts.values())) {
      start.ineligibleForAdoption = true;
      cancelInFlightAttempt(start, 'user_stop');
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
    dispatch,
    setCommandExecutor(exec) {
      executor = exec;
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

    scheduleAutoFallback,
    cancelAutoFallback,
    hasScheduledAutoFallback,

    retry,
    isRetryInFlight,
    cancelRetry,

    startLive,
    stop,
    updateTransport(newTransport: LiveSessionTransport) {
      explicitTransportUpdated = true;
      currentTransport = newTransport;
      const newBase = newTransport.apiBase;

      // 1. Active session & heartbeat runtime (evaluated independently)
      if (activeSessionId && activeSessionTransport) {
        const activeBase = activeSessionTransport.apiBase;
        if (!activeBase || !newBase || activeBase === newBase) {
          activeSessionTransport = newTransport;
          if (heartbeatRuntime) {
            heartbeatRuntime.updateTransport(newTransport);
          }
        }
      }

      // 2. Current attempt (evaluated independently)
      if (currentAttempt) {
        const attemptBase = currentAttempt.transport.apiBase;
        if (!attemptBase || !newBase || attemptBase === newBase) {
          currentAttempt.transport = newTransport;
        }
      }

      // 3. Every tracked in-flight start (evaluated independently)
      for (const start of Array.from(inFlightStarts.values())) {
        const startBase = start.transport.apiBase;
        if (!startBase || !newBase || startBase === newBase) {
          start.transport = newTransport;
        }
      }
    },
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
