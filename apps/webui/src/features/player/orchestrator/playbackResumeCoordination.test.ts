// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  createPlaybackForegroundRuntime,
  type ForegroundMediaBinding,
  type ForegroundNudgeCallbacks,
  type PlaybackForegroundRuntimeOptions,
} from './playbackForegroundRuntime';
import { startResumePlaybackRecovery } from './resumePlaybackRecovery';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackRetryResult, PlaybackRetryTarget } from './playbackTypes';

describe('Step 3c: Playback Resume Coordination Runtime (Rows 1-9)', () => {
  let status: PlayerStatus;
  let connectionLost: boolean;
  let epoch: number;
  let staleEpochs: Set<number>;
  let stoppedEpochs: Set<number>;
  let disposed: boolean;
  let onRetryCalls: PlaybackRetryTarget[];
  let onRetryMock: (target: PlaybackRetryTarget) => Promise<PlaybackRetryResult>;

  beforeEach(() => {
    vi.useFakeTimers();
    status = 'playing';
    connectionLost = false;
    epoch = 1;
    staleEpochs = new Set();
    stoppedEpochs = new Set();
    disposed = false;
    onRetryCalls = [];
    onRetryMock = vi.fn(async (target: PlaybackRetryTarget): Promise<PlaybackRetryResult> => {
      onRetryCalls.push(target);
      return { status: 'restarted', epoch: ++epoch };
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function createTestRuntime(overrides?: Partial<PlaybackForegroundRuntimeOptions>) {
    return createPlaybackForegroundRuntime({
      getDomainStatus: () => status,
      getConnectionLost: () => connectionLost,
      getPlaybackEpoch: () => epoch,
      isStalePlaybackEpoch: (ep) => staleEpochs.has(ep),
      isStoppedEpoch: (ep) => stoppedEpochs.has(ep),
      isDisposed: () => disposed,
      onRetry: onRetryMock,
      ...overrides,
    });
  }

  function createMockMedia(options?: {
    initialTime?: number;
    playMock?: () => Promise<void>;
    onHlsReload?: () => void;
    onTransitionToBuffering?: () => void;
    onTransitionToPaused?: () => void;
  }): {
    video: HTMLVideoElement & { play: ReturnType<typeof vi.fn> };
    binding: ForegroundMediaBinding;
    playSpy: ReturnType<typeof vi.fn>;
    hlsReloadSpy: ReturnType<typeof vi.fn>;
    bufferingSpy: ReturnType<typeof vi.fn>;
    pausedSpy: ReturnType<typeof vi.fn>;
  } {
    const playSpy = options?.playMock
      ? vi.fn(options.playMock)
      : vi.fn(() => Promise.resolve());
    const hlsReloadSpy = vi.fn(options?.onHlsReload ?? (() => {}));
    const bufferingSpy = vi.fn(options?.onTransitionToBuffering ?? (() => {}));
    const pausedSpy = vi.fn(options?.onTransitionToPaused ?? (() => {}));

    const video = {
      currentTime: options?.initialTime ?? 10.0,
      ended: false,
      play: playSpy,
    } as unknown as HTMLVideoElement & { play: ReturnType<typeof vi.fn> };

    const binding: ForegroundMediaBinding = {
      mediaId: 'vid-test-1',
      onHlsReload: () => {
        hlsReloadSpy();
      },
      onTransitionToBuffering: () => {
        bufferingSpy();
      },
      onTransitionToPaused: () => {
        pausedSpy();
      },
      startNudge: (callbacks: ForegroundNudgeCallbacks) => {
        return startResumePlaybackRecovery(video, {
          observeMs: 400,
          intervalMs: 250,
          maxAttempts: 8,
          onBlocked: callbacks.onBlocked,
          onFailed: callbacks.onFailed,
          onSettled: callbacks.onSettled,
          shouldContinue: callbacks.shouldContinue,
        });
      },
    };

    return { video, binding, playSpy, hlsReloadSpy, bufferingSpy, pausedSpy };
  }

  // ---------------------------------------------------------------------------
  // Row 1: Initial online/no loss, offline->online, repeated online, second outage
  // ---------------------------------------------------------------------------
  describe('Row 1: Online edges and deduplication', () => {
    it('initial online with no loss causes zero recovery', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Initial online report with active session
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
      expect(onRetryCalls).toHaveLength(0);
    });

    it('initial offline then online triggers exactly one eligible episode', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Starts offline
      runtime.updateConnectivity(false, true);
      expect(runtime.isWasUnavailable()).toBe(true);
      expect(mock.playSpy).not.toHaveBeenCalled();

      // Regains online
      runtime.updateConnectivity(true, true);
      expect(runtime.isWasUnavailable()).toBe(false);

      // Exactly 1 operation started and immediate play executed
      expect(runtime.getActiveOperationId()).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
    });

    it('repeated online reports do not produce duplicate operations', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const firstOpId = runtime.getActiveOperationId();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Repeated online report while already online
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).toBe(firstOpId);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
    });

    it('second outage and reconnect starts a fresh episode after first completes', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Outage 1
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const firstOpId = runtime.getActiveOperationId();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Stream advances and settles
      mock.video.currentTime = 12.0;
      vi.advanceTimersByTime(400); // 400ms observe -> recovered!
      expect(runtime.getActiveOperationId()).toBeNull();

      // Outage 2
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const secondOpId = runtime.getActiveOperationId();

      expect(secondOpId).not.toBeNull();
      expect(secondOpId).not.toBe(firstOpId);
      expect(mock.playSpy).toHaveBeenCalledTimes(2); // 1 from first + 1 from second
    });
  });

  // ---------------------------------------------------------------------------
  // Row 2: Availability truth table (online && !connectionLost)
  // ---------------------------------------------------------------------------
  describe('Row 2: Availability truth table', () => {
    it('online=true while connectionLost=true does not start recovery', () => {
      connectionLost = true;
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Browser reports online, but domain connectionLost is true
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
      expect(runtime.isWasUnavailable()).toBe(true);
    });

    it('clearing connectionLost while online=true completes recovery exactly once', () => {
      connectionLost = true;
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).not.toHaveBeenCalled();

      // Connection lost cleared!
      connectionLost = false;
      runtime.onConnectionLostChanged(false);

      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).not.toBeNull();
    });

    it('clearing connectionLost while offline=true does not recover until browser is online', () => {
      connectionLost = true;
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Offline + connectionLost
      runtime.updateConnectivity(false, true);
      expect(mock.playSpy).not.toHaveBeenCalled();

      // Server reachability restored, but browser still offline
      connectionLost = false;
      runtime.onConnectionLostChanged(false);
      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();

      // Finally browser comes online -> completes recovery exactly once!
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).not.toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // Row 3: Active-session gate, missing video, TV bypass, statuses, PiP & HLS kick
  // ---------------------------------------------------------------------------
  describe('Row 3: Policy gating and HLS reload kick', () => {
    it('suppresses recovery when hasActiveSession is false', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, false);
      runtime.updateConnectivity(true, false); // hasActiveSession = false

      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(mock.hlsReloadSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('bypasses recovery when isEligible is false (e.g. TV or native host active)', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });
      runtime.updateEligibility(false);

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('does not start recovery on idle or stopped status', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      status = 'stopped';
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).not.toHaveBeenCalled();

      status = 'idle';
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).not.toHaveBeenCalled();
    });

    it('does not auto-resume when user paused deliberately', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });
      runtime.setUserPaused(true);

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('triggers session retry when status is error even if userPaused was true', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });
      status = 'error';
      runtime.setUserPaused(true);

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Reaped session triggers retry before user-pause suppression
      expect(onRetryCalls).toHaveLength(1);
      expect(onRetryCalls[0]).toEqual({ kind: 'live', serviceRef: '1:0:1:TEST' });
      expect(mock.playSpy).not.toHaveBeenCalled();
    });

    it('executes HLS startLoad kick on eligible online edge with active session', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(mock.hlsReloadSpy).toHaveBeenCalledTimes(1);
    });

    it('allows online recovery even when page is hidden (no document-visible prerequisite)', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Document is hidden
      runtime.updateVisibility(false, false);

      // Online edge arrives while hidden
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Online recovery succeeds and nudges video while hidden
      expect(runtime.getActiveOperationId()).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
    });

    it('suppresses online recovery when media binding is missing under healthy status', () => {
      const runtime = createTestRuntime();
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });
      runtime.setMediaBinding(null);

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(runtime.getActiveOperationId()).toBeNull();
      expect(onRetryCalls).toHaveLength(0);
    });

    it('suppresses online retry when media binding is missing under error status', () => {
      const runtime = createTestRuntime();
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });
      runtime.setMediaBinding(null);
      status = 'error';

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(onRetryCalls).toHaveLength(0);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('handles media detachment and re-attachment around an outage correctly', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // 1. Goes offline while video attached
      runtime.updateConnectivity(false, true);

      // 2. Video detaches while offline
      runtime.setMediaBinding(null);

      // 3. Comes online while video detached -> no recovery
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();

      // 4. Video re-attached while online -> no recovery without fresh edge
      runtime.setMediaBinding(mock.binding);
      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();

      // 5. Fresh outage occurs while video attached -> recovers on online edge!
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).not.toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // Row 4: Target routes, missing identity, replacement while pending
  // ---------------------------------------------------------------------------
  describe('Row 4: Target context and replacement', () => {
    it('cancels pending operation when target context changes and does not retry old target', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:OLD' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const oldOpId = runtime.getActiveOperationId();
      expect(oldOpId).not.toBeNull();

      // Target changes to NEW
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:NEW' });
      expect(runtime.getActiveOperationId()).toBeNull();

      // Advancing timer after replacement does NOT retry OLD target
      vi.advanceTimersByTime(3000);
      expect(onRetryCalls).toHaveLength(0);
    });

    it('cancels pending operation when media binding changes', () => {
      const runtime = createTestRuntime();
      const mock1 = createMockMedia();
      runtime.setMediaBinding(mock1.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Media element replaced
      const mock2 = createMockMedia();
      mock2.binding.mediaId = 'vid-test-2';
      runtime.setMediaBinding(mock2.binding);

      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('nudges video element even when target identity is null, but does not retry on exhaustion', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext(null);

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Advance through all 8 nudges to exhaustion
      vi.advanceTimersByTime(400);
      for (let i = 0; i < 8; i++) {
        vi.advanceTimersByTime(250);
      }

      expect(mock.playSpy).toHaveBeenCalledTimes(9); // 1 free + 8 nudges
      expect(runtime.getActiveOperationId()).toBeNull();
      expect(onRetryCalls).toHaveLength(0); // No fabrication of retry without target!
    });
  });

  // ---------------------------------------------------------------------------
  // Row 5: Exact play cadence (1 free, 400ms, 8x250ms, 9 total plays)
  // ---------------------------------------------------------------------------
  describe('Row 5: Exact play cadence and attempt limits', () => {
    it('executes exactly 1 immediate free play, 400ms observe, then at most 8 nudges at 250ms', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Synchronous immediate free play
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Advance 399ms: still observing, no extra play
      vi.advanceTimersByTime(399);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Advance 1ms to 400ms: observation expired, stream stuck -> nudge 1
      vi.advanceTimersByTime(1);
      expect(mock.playSpy).toHaveBeenCalledTimes(2);

      // Advance 250ms intervals: nudges 2 through 8
      for (let attempt = 2; attempt <= 8; attempt++) {
        vi.advanceTimersByTime(250);
        expect(mock.playSpy).toHaveBeenCalledTimes(attempt + 1);
      }

      // Final interval: settled exhausted!
      vi.advanceTimersByTime(250);
      expect(mock.playSpy).toHaveBeenCalledTimes(9); // Capped at 9 total plays
      expect(runtime.getActiveOperationId()).toBeNull();
      expect(onRetryCalls).toHaveLength(1);
    });

    it('clears active operation on media progress and allows later edge to start fresh', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Progress during observation window
      mock.video.currentTime = 10.5;
      vi.advanceTimersByTime(400);

      // Explicitly cleared on progress!
      expect(runtime.getActiveOperationId()).toBeNull();

      // Subsequent hidden->visible edge is NOT swallowed by a stale slot!
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).not.toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // Row 6: Foreground then online, online then foreground, and both in one commit
  // ---------------------------------------------------------------------------
  describe('Row 6: Coalescing shared resume operation', () => {
    it('foreground then online: joins running operation, 1 immediate play, shared 400ms deadline', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // 1. Foreground reveal starts the operation at t=0
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      const opId = runtime.getActiveOperationId();
      expect(opId).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['foreground']));

      // 2. At t=100ms, online reconnect arrives
      vi.advanceTimersByTime(100);
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Joins existing operation!
      expect(runtime.getActiveOperationId()).toBe(opId);
      expect(mock.playSpy).toHaveBeenCalledTimes(1); // NO duplicate play!
      expect(runtime.getActiveParticipants()).toEqual(new Set(['foreground', 'online']));

      // 3. Deadline remains original t=400ms (so 300ms from t=100ms)
      vi.advanceTimersByTime(299);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      vi.advanceTimersByTime(1);
      expect(mock.playSpy).toHaveBeenCalledTimes(2); // nudge 1 fired at original 400ms!

      // 4. Exhaustion reaches capped 9 total plays and single retry
      for (let i = 0; i < 8; i++) {
        vi.advanceTimersByTime(250);
      }
      expect(mock.playSpy).toHaveBeenCalledTimes(9);
      expect(onRetryCalls).toHaveLength(1);
    });

    it('online then foreground: joins running operation, 1 immediate play, shared deadline', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // 1. Online reconnect starts the operation at t=0
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const opId = runtime.getActiveOperationId();
      expect(opId).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['online']));

      // 2. At t=150ms, foreground reveal arrives
      vi.advanceTimersByTime(150);
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      // Joins existing operation!
      expect(runtime.getActiveOperationId()).toBe(opId);
      expect(mock.playSpy).toHaveBeenCalledTimes(1); // NO duplicate play!
      expect(runtime.getActiveParticipants()).toEqual(new Set(['online', 'foreground']));

      // 3. Fires at original t=400ms (250ms after t=150ms)
      vi.advanceTimersByTime(250);
      expect(mock.playSpy).toHaveBeenCalledTimes(2);
    });

    it('both in one commit: one immediate play, one timer chain, both recorded as participants', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Prepare edges
      runtime.updateVisibility(false, false);
      runtime.updateConnectivity(false, true);

      // Simultaneous commit firing
      runtime.updateVisibility(true, false);
      runtime.updateConnectivity(true, true);

      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['foreground', 'online']));

      // Shared timer chain
      vi.advanceTimersByTime(400);
      expect(mock.playSpy).toHaveBeenCalledTimes(2);
    });
  });

  // ---------------------------------------------------------------------------
  // Row 7: Participant cancellation & withdrawal
  // ---------------------------------------------------------------------------
  describe('Row 7: Participant cancellation and withdrawal', () => {
    it('hide withdraws foreground; online participant keeps operation running', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Both participate
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['foreground', 'online']));

      // Document hides -> withdraws foreground
      runtime.updateVisibility(false, false);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['online']));
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Operation continues!
      vi.advanceTimersByTime(400);
      expect(mock.playSpy).toHaveBeenCalledTimes(2);
    });

    it('offline withdraws online; foreground participant keeps operation running', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Both participate
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Network lost -> withdraws online
      runtime.updateConnectivity(false, true);
      expect(runtime.getActiveParticipants()).toEqual(new Set(['foreground']));
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Operation continues!
      vi.advanceTimersByTime(400);
      expect(mock.playSpy).toHaveBeenCalledTimes(2);
    });

    it('cancels operation when last participant withdraws; fresh edge starts new operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Both participate
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Foreground withdraws
      runtime.updateVisibility(false, false);
      expect(runtime.getActiveOperationId()).not.toBeNull();

      // Online withdraws -> no participants left!
      runtime.updateConnectivity(false, true);
      expect(runtime.getActiveOperationId()).toBeNull();

      // Advance timers -> no further plays occur
      vi.advanceTimersByTime(2000);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Fresh edge after full cancellation starts fresh operation
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(2);
    });
  });

  // ---------------------------------------------------------------------------
  // Row 8: Lifecycle boundaries, stop, pause, epoch fencing, terminal auth
  // ---------------------------------------------------------------------------
  describe('Row 8: Lifecycle boundaries and terminal auth', () => {
    it('stopping playback cancels active recovery', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).not.toBeNull();

      runtime.onPlaybackStopped(epoch);
      expect(runtime.getActiveOperationId()).toBeNull();

      vi.advanceTimersByTime(2000);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
    });

    it('historical auth from older epoch does NOT cancel newer recovery operation', () => {
      epoch = 5;
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      const opId = runtime.getActiveOperationId();

      // Stale auth error from epoch 2
      runtime.onTerminalAuth(2);

      // Newer op at epoch 5 stays active!
      expect(runtime.getActiveOperationId()).toBe(opId);
    });

    it('current or forward terminal auth cancels active recovery', () => {
      epoch = 5;
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Terminal auth for current epoch
      runtime.onTerminalAuth(5);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('deferred play rejection after operation cancellation does not transition status', () => {
      let rejectPlay!: (err: unknown) => void;
      const playPromise = new Promise<void>((_, reject) => {
        rejectPlay = reject;
      });
      const mock = createMockMedia({
        playMock: vi.fn(() => playPromise),
      });
      const runtime = createTestRuntime();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Cancel operation before play resolves/rejects
      runtime.setUserPaused(true);
      expect(runtime.getActiveOperationId()).toBeNull();

      // Late play rejection arrives
      rejectPlay({ name: 'NotAllowedError' });
      // paused callback must NOT be invoked for dead operation
      expect(mock.pausedSpy).not.toHaveBeenCalled();
    });

    it('suppresses online and foreground recovery while retry preparation is in flight', () => {
      let retryInFlight = false;
      const runtime = createTestRuntime({
        isRetryInFlight: () => retryInFlight,
      });
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Start retry preparation
      retryInFlight = true;

      // Online edge arrives during preparation
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();

      // Foreground edge arrives during preparation
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(mock.playSpy).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();

      // Complete retry preparation
      retryInFlight = false;

      // Fresh online edge after preparation completes triggers recovery
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).not.toBeNull();
    });

    it('cancels active resume operation when retry is initiated', () => {
      const runtime = createTestRuntime();
      const mock = createMockMedia();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      // Active recovery running
      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);
      expect(runtime.getActiveOperationId()).not.toBeNull();
      expect(mock.playSpy).toHaveBeenCalledTimes(1);

      // Retry initiated
      runtime.onRetryInitiated();
      expect(runtime.getActiveOperationId()).toBeNull();

      // Advancing timers produces no further plays
      vi.advanceTimersByTime(2000);
      expect(mock.playSpy).toHaveBeenCalledTimes(1);
    });
  });

  // ---------------------------------------------------------------------------
  // Row 9: Reentrancy, synchronous cancellation, and single retry
  // ---------------------------------------------------------------------------
  describe('Row 9: Reentrancy and settlement safety', () => {
    it('handles synchronous cancellation inside startNudge gracefully', () => {
      const runtime = createTestRuntime();
      let cancelCalled = false;
      const binding: ForegroundMediaBinding = {
        mediaId: 'vid-sync-cancel',
        startNudge: () => {
          // Synchronously pause during startNudge
          runtime.setUserPaused(true);
          return () => {
            cancelCalled = true;
          };
        },
      };
      runtime.setMediaBinding(binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      expect(runtime.getActiveOperationId()).toBeNull();
      expect(cancelCalled).toBe(true);
    });

    it('clears activeOp before onRetry callback, ensuring zero duplicate retries on reentrancy', () => {
      const mock = createMockMedia();
      let opIdDuringRetry: number | null = -1;

      const runtime = createTestRuntime({
        onRetry: vi.fn(async (target: PlaybackRetryTarget) => {
          onRetryCalls.push(target);
          // Inspect active op ID from inside onRetry
          opIdDuringRetry = runtime.getActiveOperationId();
          return { status: 'restarted' as const, epoch: ++epoch };
        }),
      });

      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:TEST' });

      runtime.updateConnectivity(false, true);
      runtime.updateConnectivity(true, true);

      // Run until exhaustion
      vi.advanceTimersByTime(400);
      for (let i = 0; i < 8; i++) {
        vi.advanceTimersByTime(250);
      }

      expect(onRetryCalls).toHaveLength(1);
      // activeOp was already cleared to null before onRetry was invoked!
      expect(opIdDuringRetry).toBeNull();
      expect(runtime.getActiveOperationId()).toBeNull();
    });
  });
});
