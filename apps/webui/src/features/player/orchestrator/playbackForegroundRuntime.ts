// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { decideForegroundResume, type ForegroundResumeAction } from './foregroundResume';
import { decideOnlineRecovery, type OnlineRecoveryAction } from './onlineRecovery';
import type { PlaybackRetryResult, PlaybackRetryTarget } from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';
import { debugWarn } from '../../../utils/logging';
import type { ResumeRecoveryOutcome } from './resumePlaybackRecovery';

export type ResumeTrigger = 'foreground' | 'online';

export interface ForegroundMediaBinding {
  mediaId: string;
  onHlsReload?: () => void;
  startNudge: (callbacks: ForegroundNudgeCallbacks) => (() => void);
  onTransitionToBuffering?: () => void;
  onTransitionToPaused?: () => void;
  isUserPaused?: () => boolean;
}

export interface ForegroundNudgeCallbacks {
  onBlocked: (err: unknown) => void;
  onFailed: () => void;
  onSettled?: (outcome: ResumeRecoveryOutcome) => void;
  shouldContinue: () => boolean;
}

export interface PlaybackForegroundRuntimeOptions {
  getDomainStatus: () => PlayerStatus;
  getConnectionLost?: () => boolean;
  getPlaybackEpoch: () => number;
  isStalePlaybackEpoch: (epoch: number) => boolean;
  isStoppedEpoch: (epoch: number) => boolean;
  isDisposed: () => boolean;
  isRetryInFlight?: () => boolean;
  onRetry: (target: PlaybackRetryTarget) => Promise<PlaybackRetryResult>;
}

export interface ActiveResumeOperation {
  opId: number;
  epoch: number;
  target: PlaybackRetryTarget | null;
  mediaId: string;
  participants: Set<ResumeTrigger>;
  cancelled: boolean;
  settled: boolean;
  cancelHandle: (() => void) | null;
}

export type ActiveForegroundOperation = ActiveResumeOperation;

export interface PlaybackForegroundRuntime {
  updateEligibility(eligible: boolean): void;
  updateVisibility(visible: boolean, isPiP: boolean): void;
  updateConnectivity(online: boolean, hasActiveSession: boolean): void;
  onConnectionLostChanged(connectionLost: boolean): void;
  setTargetContext(target: PlaybackRetryTarget | null): void;
  setMediaBinding(binding: ForegroundMediaBinding | null): void;
  setUserPaused(userPaused: boolean): void;
  onPlaybackAttemptStarted(epoch: number): void;
  onPlaybackStopped(epoch: number): void;
  onTerminalAuth(epoch?: number): void;
  onRetryInitiated(): void;
  dispose(): void;

  // Inspection for tests
  getActiveOperationId(): number | null;
  getActiveParticipants(): ReadonlySet<ResumeTrigger> | null;
  getCapturedTarget(): PlaybackRetryTarget | null;
  isWasHidden(): boolean;
  isWasUnavailable(): boolean;
}

export function isSameRetryTarget(
  a: PlaybackRetryTarget | null,
  b: PlaybackRetryTarget | null,
): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  if (a.kind !== b.kind) return false;
  if (a.explicitProfile !== b.explicitProfile) return false;
  if (a.kind === 'live') return a.serviceRef === b.serviceRef;
  if (a.kind === 'vod') return a.recordingId === b.recordingId;
  if (a.kind === 'src') return a.srcUrl === b.srcUrl;
  return false;
}

export function resolveCommittedForegroundTarget(params: {
  recordingId?: string | null;
  src?: string | null;
  serviceRef?: string | null;
  activeServiceRef?: string | null;
  explicitProfile?: string | null;
}): PlaybackRetryTarget | null {
  const explicitProfile = params.explicitProfile?.trim() || undefined;

  if (params.recordingId && params.recordingId.trim()) {
    return {
      kind: 'vod',
      recordingId: params.recordingId.trim(),
      explicitProfile,
    };
  }

  if (params.src && params.src.trim()) {
    return {
      kind: 'src',
      srcUrl: params.src.trim(),
      explicitProfile,
    };
  }

  const liveRef = params.activeServiceRef?.trim() || params.serviceRef?.trim();
  if (liveRef) {
    return {
      kind: 'live',
      serviceRef: liveRef,
      explicitProfile,
    };
  }

  return null;
}

export function createPlaybackForegroundRuntime(
  options: PlaybackForegroundRuntimeOptions,
): PlaybackForegroundRuntime {
  let isEligible = true;
  let wasHiddenState = false;
  let browserOnline = true;
  let hasActiveSession = false;
  let wasUnavailableState = options.getConnectionLost ? options.getConnectionLost() : false;
  let capturedTarget: PlaybackRetryTarget | null = null;
  let currentBinding: ForegroundMediaBinding | null = null;
  let isUserPaused = false;
  let nextOpId = 0;
  let activeOp: ActiveResumeOperation | null = null;

  function cancelActiveOperation(_reason: string): void {
    if (activeOp) {
      const op = activeOp;
      activeOp = null;
      op.cancelled = true;
      if (op.cancelHandle) {
        try {
          op.cancelHandle();
        } catch (err) {
          debugWarn('[V3Player][Resume] Error during cancelHandle', err);
        }
        op.cancelHandle = null;
      }
    }
  }

  function withdrawParticipant(trigger: ResumeTrigger): void {
    if (!activeOp) return;
    activeOp.participants.delete(trigger);
    if (activeOp.participants.size === 0) {
      cancelActiveOperation(`participant_withdrawn_${trigger}`);
    }
  }

  function shouldNudgeContinue(op: ActiveResumeOperation): boolean {
    if (op.cancelled || activeOp !== op) return false;
    if (options.isDisposed()) return false;
    if (options.isStalePlaybackEpoch(op.epoch)) return false;
    if (options.isStoppedEpoch(op.epoch)) return false;
    if (isUserPaused || (currentBinding?.isUserPaused?.() ?? false)) return false;
    return true;
  }

  function handleNudgeBlocked(op: ActiveResumeOperation, err: unknown): void {
    if (op.cancelled || activeOp !== op) return;
    if (options.isDisposed()) return;
    if (options.isStalePlaybackEpoch(op.epoch)) return;
    if (options.isStoppedEpoch(op.epoch)) return;

    if ((err as { name?: string } | null)?.name === 'NotAllowedError') {
      currentBinding?.onTransitionToPaused?.();
    } else {
      debugWarn('[V3Player] Browser resume play blocked', err);
    }
  }

  function handleOperationSettled(op: ActiveResumeOperation, outcome: ResumeRecoveryOutcome): void {
    if (op.settled || activeOp !== op) {
      return;
    }
    activeOp = null;
    op.settled = true;

    if (outcome === 'exhausted') {
      if (
        !op.cancelled &&
        !options.isDisposed() &&
        !options.isStalePlaybackEpoch(op.epoch) &&
        !options.isStoppedEpoch(op.epoch) &&
        !isUserPaused &&
        !(currentBinding?.isUserPaused?.() ?? false)
      ) {
        debugWarn('[V3Player] Browser resume play failed to advance, retrying session');
        if (op.target) {
          void options.onRetry(op.target);
        }
      }
    }
  }

  function requestPlayRecovery(trigger: ResumeTrigger): void {
    if (!currentBinding) {
      return;
    }

    if (options.isRetryInFlight?.()) {
      return;
    }

    if (
      activeOp &&
      !activeOp.cancelled &&
      !activeOp.settled &&
      activeOp.epoch === options.getPlaybackEpoch() &&
      activeOp.mediaId === currentBinding.mediaId &&
      isSameRetryTarget(activeOp.target, capturedTarget) &&
      !options.isDisposed() &&
      !options.isStalePlaybackEpoch(activeOp.epoch) &&
      !options.isStoppedEpoch(activeOp.epoch)
    ) {
      // Matching second trigger joins running operation without resetting timer, attempt count, or play call
      activeOp.participants.add(trigger);
      return;
    }

    cancelActiveOperation('superseded_by_new_episode');

    const opId = ++nextOpId;
    const epoch = options.getPlaybackEpoch();
    const op: ActiveResumeOperation = {
      opId,
      epoch,
      target: capturedTarget,
      mediaId: currentBinding.mediaId,
      participants: new Set<ResumeTrigger>([trigger]),
      cancelled: false,
      settled: false,
      cancelHandle: null,
    };
    activeOp = op;

    const targetBinding = currentBinding;
    currentBinding.onTransitionToBuffering?.();

    if (
      op.cancelled ||
      activeOp !== op ||
      options.isDisposed() ||
      options.isStalePlaybackEpoch(op.epoch) ||
      options.isStoppedEpoch(op.epoch) ||
      currentBinding !== targetBinding ||
      currentBinding.mediaId !== op.mediaId
    ) {
      if (activeOp === op) {
        activeOp = null;
      }
      op.cancelled = true;
      return;
    }

    try {
      const handle = targetBinding.startNudge({
        onBlocked: (err) => handleNudgeBlocked(op, err),
        onFailed: () => handleOperationSettled(op, 'exhausted'),
        onSettled: (outcome) => handleOperationSettled(op, outcome),
        shouldContinue: () => shouldNudgeContinue(op),
      });

      if (
        op.cancelled ||
        activeOp !== op ||
        options.isDisposed() ||
        options.isStalePlaybackEpoch(op.epoch) ||
        options.isStoppedEpoch(op.epoch)
      ) {
        if (typeof handle === 'function') {
          try {
            handle();
          } catch {
            // ignore
          }
        }
      } else {
        op.cancelHandle = handle;
      }
    } catch (_err) {
      op.cancelled = true;
      if (activeOp === op) {
        activeOp = null;
      }
    }
  }

  function checkAvailabilityEdge(): void {
    if (options.isDisposed()) return;

    const connectionLost = options.getConnectionLost ? options.getConnectionLost() : false;
    const isAvailable = browserOnline && !connectionLost;

    if (!isAvailable) {
      wasUnavailableState = true;
      withdrawParticipant('online');
      return;
    }

    const wasOffline = wasUnavailableState;
    wasUnavailableState = false;

    if (!wasOffline) {
      return;
    }

    if (!isEligible || !currentBinding) {
      return;
    }

    if (options.isRetryInFlight?.()) {
      return;
    }

    if (hasActiveSession && currentBinding.onHlsReload) {
      try {
        currentBinding.onHlsReload();
      } catch (err) {
        debugWarn('[V3Player] hls reconnect startLoad failed', err);
      }
    }

    const domainStatus = options.getDomainStatus();
    const hasTerminal = domainStatus === 'idle' || domainStatus === 'error' || domainStatus === 'stopped';
    const effectiveUserPaused = isUserPaused || (currentBinding.isUserPaused?.() ?? false);

    const action: OnlineRecoveryAction = decideOnlineRecovery({
      wasOffline: true,
      hasActiveSession,
      status: domainStatus,
      userPaused: effectiveUserPaused,
      hasTerminal,
    });

    if (action === 'none') {
      return;
    }

    if (action === 'retry') {
      cancelActiveOperation('reestablishing_session');
      if (capturedTarget) {
        void options.onRetry(capturedTarget);
      }
      return;
    }

    requestPlayRecovery('online');
  }

  function updateEligibility(eligible: boolean): void {
    if (isEligible === eligible) return;
    isEligible = eligible;
    if (!isEligible) {
      cancelActiveOperation('ineligible');
    }
  }

  function updateVisibility(visible: boolean, isPiP: boolean): void {
    if (options.isDisposed()) return;

    if (!visible) {
      wasHiddenState = true;
      withdrawParticipant('foreground');
      return;
    }

    const wasHidden = wasHiddenState;
    wasHiddenState = false;

    if (!wasHidden) {
      return;
    }

    if (!isEligible || !currentBinding) {
      return;
    }

    if (options.isRetryInFlight?.()) {
      return;
    }

    if (currentBinding.onHlsReload) {
      try {
        currentBinding.onHlsReload();
      } catch (err) {
        debugWarn('[V3Player] hls resume startLoad failed', err);
      }
    }

    const domainStatus = options.getDomainStatus();
    const hasTerminal = domainStatus === 'idle' || domainStatus === 'error' || domainStatus === 'stopped';
    const effectiveUserPaused = isUserPaused || (currentBinding.isUserPaused?.() ?? false);

    const action: ForegroundResumeAction = decideForegroundResume({
      wasHidden: true,
      isPiP,
      status: domainStatus,
      userPaused: effectiveUserPaused,
      hasTerminal,
    });

    if (action === 'none') {
      return;
    }

    if (action === 'retry') {
      cancelActiveOperation('reestablishing_session');
      if (capturedTarget) {
        void options.onRetry(capturedTarget);
      }
      return;
    }

    requestPlayRecovery('foreground');
  }

  function updateConnectivity(online: boolean, hasActive: boolean): void {
    browserOnline = online;
    hasActiveSession = hasActive;
    checkAvailabilityEdge();
  }

  function onConnectionLostChanged(_connectionLost: boolean): void {
    checkAvailabilityEdge();
  }

  function setTargetContext(target: PlaybackRetryTarget | null): void {
    if (isSameRetryTarget(capturedTarget, target)) {
      return;
    }
    cancelActiveOperation('source_replacement');
    capturedTarget = target;
  }

  function setMediaBinding(binding: ForegroundMediaBinding | null): void {
    if (currentBinding === binding) return;

    if (!binding) {
      cancelActiveOperation('media_detached');
      currentBinding = null;
      return;
    }

    if (currentBinding && currentBinding.mediaId === binding.mediaId) {
      // Same media element, updated callbacks/executor. Do not cancel the ongoing loop!
      currentBinding = binding;
      return;
    }

    // Media identity changed!
    cancelActiveOperation('media_replaced');
    currentBinding = binding;
  }

  function setUserPaused(userPaused: boolean): void {
    isUserPaused = userPaused;
    if (isUserPaused) {
      cancelActiveOperation('user_pause');
    }
  }

  function onPlaybackAttemptStarted(epoch: number): void {
    if (activeOp && activeOp.epoch < epoch) {
      cancelActiveOperation('superseded_epoch');
    }
  }

  function onPlaybackStopped(_epoch: number): void {
    cancelActiveOperation('playback_stopped');
  }

  function onTerminalAuth(authEpoch?: number): void {
    if (activeOp) {
      if (authEpoch === undefined || authEpoch >= activeOp.epoch) {
        cancelActiveOperation('terminal_auth');
      }
    }
  }

  function onRetryInitiated(): void {
    cancelActiveOperation('retry_initiated');
  }

  function dispose(): void {
    cancelActiveOperation('disposed');
    currentBinding = null;
    capturedTarget = null;
  }

  return {
    updateEligibility,
    updateVisibility,
    updateConnectivity,
    onConnectionLostChanged,
    setTargetContext,
    setMediaBinding,
    setUserPaused,
    onPlaybackAttemptStarted,
    onPlaybackStopped,
    onTerminalAuth,
    onRetryInitiated,
    dispose,
    getActiveOperationId: () => activeOp?.opId ?? null,
    getActiveParticipants: () => (activeOp ? new Set(activeOp.participants) : null),
    getCapturedTarget: () => capturedTarget,
    isWasHidden: () => wasHiddenState,
    isWasUnavailable: () => wasUnavailableState,
  };
}
