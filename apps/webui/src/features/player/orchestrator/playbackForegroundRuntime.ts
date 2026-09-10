// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { decideForegroundResume, type ForegroundResumeAction } from './foregroundResume';
import type { PlaybackRetryResult, PlaybackRetryTarget } from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';
import { debugWarn } from '../../../utils/logging';

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
  shouldContinue: () => boolean;
}

export interface PlaybackForegroundRuntimeOptions {
  getDomainStatus: () => PlayerStatus;
  getPlaybackEpoch: () => number;
  isStalePlaybackEpoch: (epoch: number) => boolean;
  isStoppedEpoch: (epoch: number) => boolean;
  isDisposed: () => boolean;
  onRetry: (target: PlaybackRetryTarget) => Promise<PlaybackRetryResult>;
}

export interface ActiveForegroundOperation {
  opId: number;
  epoch: number;
  target: PlaybackRetryTarget | null;
  mediaId: string;
  cancelled: boolean;
  cancelHandle: (() => void) | null;
}

export interface PlaybackForegroundRuntime {
  updateEligibility(eligible: boolean): void;
  updateVisibility(visible: boolean, isPiP: boolean): void;
  setTargetContext(target: PlaybackRetryTarget | null): void;
  setMediaBinding(binding: ForegroundMediaBinding | null): void;
  setUserPaused(userPaused: boolean): void;
  onPlaybackAttemptStarted(epoch: number): void;
  onPlaybackStopped(epoch: number): void;
  onTerminalAuth(epoch?: number): void;
  dispose(): void;

  // Inspection for tests
  getActiveOperationId(): number | null;
  getCapturedTarget(): PlaybackRetryTarget | null;
  isWasHidden(): boolean;
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
  let capturedTarget: PlaybackRetryTarget | null = null;
  let currentBinding: ForegroundMediaBinding | null = null;
  let isUserPaused = false;
  let nextOpId = 0;
  let activeOp: ActiveForegroundOperation | null = null;

  function cancelActiveOperation(_reason: string): void {
    if (activeOp) {
      const op = activeOp;
      activeOp = null;
      op.cancelled = true;
      if (op.cancelHandle) {
        try {
          op.cancelHandle();
        } catch (err) {
          debugWarn('[V3Player][Foreground] Error during cancelHandle', err);
        }
        op.cancelHandle = null;
      }
    }
  }

  function shouldNudgeContinue(op: ActiveForegroundOperation): boolean {
    if (op.cancelled || activeOp !== op) return false;
    if (options.isDisposed()) return false;
    if (options.isStalePlaybackEpoch(op.epoch)) return false;
    if (options.isStoppedEpoch(op.epoch)) return false;
    if (isUserPaused || (currentBinding?.isUserPaused?.() ?? false)) return false;
    return true;
  }

  function handleNudgeBlocked(op: ActiveForegroundOperation, err: unknown): void {
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

  function handleNudgeFailed(op: ActiveForegroundOperation): void {
    if (op.cancelled || activeOp !== op) return;
    if (options.isDisposed()) return;
    if (options.isStalePlaybackEpoch(op.epoch)) return;
    if (options.isStoppedEpoch(op.epoch)) return;
    if (isUserPaused || (currentBinding?.isUserPaused?.() ?? false)) return;

    debugWarn('[V3Player] Browser resume play failed to advance, retrying session');
    cancelActiveOperation('exhaustion');

    if (op.target) {
      void options.onRetry(op.target);
    }
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
      cancelActiveOperation('hidden');
      return;
    }

    // Document is visible. Check if this was a genuine hidden->visible transition.
    const wasHidden = wasHiddenState;
    wasHiddenState = false;

    if (!wasHidden) {
      // Not a hidden->visible edge (initial visible mount or repeated visible report)
      return;
    }

    if (!isEligible) {
      return;
    }

    // Preserve HLS startLoad() kick on qualifying reveal BEFORE play/retry decision
    if (currentBinding?.onHlsReload) {
      try {
        currentBinding.onHlsReload();
      } catch (err) {
        debugWarn('[V3Player] hls resume startLoad failed', err);
      }
    }

    const domainStatus = options.getDomainStatus();
    const hasTerminal = domainStatus === 'idle' || domainStatus === 'error' || domainStatus === 'stopped';
    const effectiveUserPaused = isUserPaused || (currentBinding?.isUserPaused?.() ?? false);

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

    // action === 'play'
    if (!currentBinding) {
      return;
    }

    cancelActiveOperation('superseded_by_new_episode');

    const opId = ++nextOpId;
    const epoch = options.getPlaybackEpoch();
    const op: ActiveForegroundOperation = {
      opId,
      epoch,
      target: capturedTarget,
      mediaId: currentBinding.mediaId,
      cancelled: false,
      cancelHandle: null,
    };
    activeOp = op;

    const targetBinding = currentBinding;
    currentBinding.onTransitionToBuffering?.();

    // Revalidate operation, lifecycle, and binding ownership after external callback before starting DOM work
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
        onFailed: () => handleNudgeFailed(op),
        shouldContinue: () => shouldNudgeContinue(op),
      });

      if (
        op.cancelled ||
        activeOp !== op ||
        options.isDisposed() ||
        options.isStalePlaybackEpoch(op.epoch) ||
        options.isStoppedEpoch(op.epoch)
      ) {
        // Synchronous cancellation occurred during startNudge!
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

  function dispose(): void {
    cancelActiveOperation('disposed');
    currentBinding = null;
    capturedTarget = null;
  }

  return {
    updateEligibility,
    updateVisibility,
    setTargetContext,
    setMediaBinding,
    setUserPaused,
    onPlaybackAttemptStarted,
    onPlaybackStopped,
    onTerminalAuth,
    dispose,
    getActiveOperationId: () => activeOp?.opId ?? null,
    getCapturedTarget: () => capturedTarget,
    isWasHidden: () => wasHiddenState,
  };
}
