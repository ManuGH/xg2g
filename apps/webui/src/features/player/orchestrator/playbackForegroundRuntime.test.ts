import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import {
  createPlaybackForegroundRuntime,
  resolveCommittedForegroundTarget,
  isSameRetryTarget,
  type ForegroundMediaBinding,
  type ForegroundNudgeCallbacks,
  type PlaybackForegroundRuntimeOptions,
} from './playbackForegroundRuntime';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackRetryResult, PlaybackRetryTarget } from './playbackTypes';

describe('resolveCommittedForegroundTarget', () => {
  it('prefers recordingId (VOD) over src and live serviceRef', () => {
    const target = resolveCommittedForegroundTarget({
      recordingId: 'rec-123',
      src: 'http://example.com/stream.m3u8',
      serviceRef: '1:0:1:1:1:1:0:0:0:0:',
      explicitProfile: '1080p',
    });
    expect(target).toEqual({
      kind: 'vod',
      recordingId: 'rec-123',
      explicitProfile: '1080p',
    });
  });

  it('prefers src over live serviceRef when recordingId is absent', () => {
    const target = resolveCommittedForegroundTarget({
      src: 'http://example.com/stream.m3u8',
      serviceRef: '1:0:1:1:1:1:0:0:0:0:',
      explicitProfile: '720p',
    });
    expect(target).toEqual({
      kind: 'src',
      srcUrl: 'http://example.com/stream.m3u8',
      explicitProfile: '720p',
    });
  });

  it('prefers activeServiceRef over prop serviceRef for live streams', () => {
    const target = resolveCommittedForegroundTarget({
      serviceRef: '1:0:1:OLD',
      activeServiceRef: '1:0:1:ACTIVE',
      explicitProfile: 'auto',
    });
    expect(target).toEqual({
      kind: 'live',
      serviceRef: '1:0:1:ACTIVE',
      explicitProfile: 'auto',
    });
  });

  it('returns null when all source identities are empty or whitespace', () => {
    expect(resolveCommittedForegroundTarget({})).toBeNull();
    expect(resolveCommittedForegroundTarget({ recordingId: '   ', src: '', serviceRef: '  ' })).toBeNull();
  });
});

describe('isSameRetryTarget', () => {
  it('correctly compares target equality across kinds and identities', () => {
    const vodA: PlaybackRetryTarget = { kind: 'vod', recordingId: 'r1', explicitProfile: 'hd' };
    const vodB: PlaybackRetryTarget = { kind: 'vod', recordingId: 'r1', explicitProfile: 'hd' };
    const vodC: PlaybackRetryTarget = { kind: 'vod', recordingId: 'r2', explicitProfile: 'hd' };
    const vodD: PlaybackRetryTarget = { kind: 'vod', recordingId: 'r1', explicitProfile: 'sd' };
    expect(isSameRetryTarget(vodA, vodB)).toBe(true);
    expect(isSameRetryTarget(vodA, vodC)).toBe(false);
    expect(isSameRetryTarget(vodA, vodD)).toBe(false);
    expect(isSameRetryTarget(vodA, null)).toBe(false);
    expect(isSameRetryTarget(null, null)).toBe(true);
  });
});

describe('PlaybackForegroundRuntime', () => {
  let status: PlayerStatus;
  let epoch: number;
  let staleEpochs: Set<number>;
  let stoppedEpochs: Set<number>;
  let disposed: boolean;
  let onRetryCalls: PlaybackRetryTarget[];
  let onRetryMock: (target: PlaybackRetryTarget) => Promise<PlaybackRetryResult>;

  beforeEach(() => {
    vi.useFakeTimers();
    status = 'playing';
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

  function createTestRuntime(overrides: Partial<PlaybackForegroundRuntimeOptions> = {}) {
    return createPlaybackForegroundRuntime({
      getDomainStatus: () => status,
      getPlaybackEpoch: () => epoch,
      isStalePlaybackEpoch: (ep) => staleEpochs.has(ep) || ep !== epoch,
      isStoppedEpoch: (ep) => stoppedEpochs.has(ep),
      isDisposed: () => disposed,
      onRetry: onRetryMock,
      ...overrides,
    });
  }

  function createMockBinding(mediaId: string = 'vid-1') {
    let activeCallbacks: ForegroundNudgeCallbacks | null = null;
    const cancelHandle = vi.fn();
    const onHlsReload = vi.fn();
    const onTransitionToBuffering = vi.fn();
    const onTransitionToPaused = vi.fn();
    const isUserPaused = vi.fn(() => false);

    const startNudge = vi.fn((callbacks: ForegroundNudgeCallbacks) => {
      activeCallbacks = callbacks;
      return cancelHandle;
    });

    const binding: ForegroundMediaBinding = {
      mediaId,
      onHlsReload,
      startNudge,
      onTransitionToBuffering,
      onTransitionToPaused,
      isUserPaused,
    };

    return {
      binding,
      cancelHandle,
      onHlsReload,
      startNudge,
      onTransitionToBuffering,
      onTransitionToPaused,
      isUserPaused,
      getActiveCallbacks: () => activeCallbacks,
    };
  }

  describe('1. Initial mount, repeated reports, and multi-episode hide/reveal', () => {
    it('initial visible mount does not trigger recovery (0 loads, 0 plays, 0 retries)', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(true, false);
      expect(mock.onHlsReload).not.toHaveBeenCalled();
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('hidden mount followed by reveal triggers exactly 1 reload and 1 play operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false); // hidden mount
      expect(runtime.isWasHidden()).toBe(true);
      expect(mock.startNudge).not.toHaveBeenCalled();

      runtime.updateVisibility(true, false); // reveal
      expect(runtime.isWasHidden()).toBe(false);
      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
      expect(mock.onTransitionToBuffering).toHaveBeenCalledTimes(1);
      expect(mock.startNudge).toHaveBeenCalledTimes(1);
      expect(onRetryMock).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBe(1);
    });

    it('repeated visible reports do not duplicate an episode', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(mock.startNudge).toHaveBeenCalledTimes(1);

      // Subsequent visible reports while already visible:
      runtime.updateVisibility(true, false);
      runtime.updateVisibility(true, false);
      expect(mock.startNudge).toHaveBeenCalledTimes(1);
      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
    });

    it('second hide/reveal in the same epoch initiates a second operation and cancels the first', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      // Episode 1
      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);
      const firstCallbacks = mock.getActiveCallbacks();

      // Episode 2 in same epoch
      runtime.updateVisibility(false, false); // hides again mid-recovery
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();

      runtime.updateVisibility(true, false); // reveal again
      expect(runtime.getActiveOperationId()).toBe(2);
      expect(mock.startNudge).toHaveBeenCalledTimes(2);

      // Late callbacks from episode 1 must be inert:
      firstCallbacks?.onBlocked({ name: 'NotAllowedError' });
      expect(mock.onTransitionToPaused).not.toHaveBeenCalled();
      firstCallbacks?.onFailed();
      expect(onRetryMock).not.toHaveBeenCalled();
    });
  });

  describe('2. Decision truth table and HLS reload kick', () => {
    it('runs HLS reload kick but selects none when in Picture-in-Picture', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, true); // isPiP = true

      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).not.toHaveBeenCalled();
    });

    it('runs HLS reload kick but selects none when healthy user-paused', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });
      runtime.setUserPaused(true);

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).not.toHaveBeenCalled();
    });

    it('runs HLS reload kick AND selects retry when status is error even if user-paused', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      const target: PlaybackRetryTarget = { kind: 'vod', recordingId: 'rec-99', explicitProfile: '1080p' };
      runtime.setTargetContext(target);
      status = 'error';
      runtime.setUserPaused(true); // error takes precedence over userPaused in decideForegroundResume!

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).toHaveBeenCalledWith(target);
    });

    it('runs HLS reload kick but selects none when status is stopped', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });
      status = 'stopped';

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(mock.onHlsReload).toHaveBeenCalledTimes(1);
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).not.toHaveBeenCalled();
    });

    it('ineligible state (TV or native-active bypass) skips both HLS reload kick and resume decision', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });
      runtime.updateEligibility(false); // Ineligible!

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(mock.onHlsReload).not.toHaveBeenCalled();
      expect(mock.startNudge).not.toHaveBeenCalled();
      expect(onRetryMock).not.toHaveBeenCalled();
    });
  });

  describe('3. Target capture, missing identity and source replacement', () => {
    it('missing retry identity prevents unexecutable retry on status error without crashing', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext(null); // Missing identity!
      status = 'error';

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(onRetryMock).not.toHaveBeenCalled();
    });

    it('committed source replacement (A -> B) during observation window cancels previous operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:CHANNEL_A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      // User changes channel from A to B:
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:CHANNEL_B' });
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();

      // Late exhaustion on the cancelled operation does not retry channel A:
      mock.getActiveCallbacks()?.onFailed();
      expect(onRetryMock).not.toHaveBeenCalled();
    });
  });

  describe('4. Nudge callbacks, NotAllowedError and late rejection fencing', () => {
    it('active NotAllowedError reports paused status', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      mock.getActiveCallbacks()?.onBlocked({ name: 'NotAllowedError' });
      expect(mock.onTransitionToPaused).toHaveBeenCalledTimes(1);
    });

    it('late rejection after cancellation does not update status or throw unhandled rejection', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      const callbacks = mock.getActiveCallbacks();

      // Hide before play rejection settles:
      runtime.updateVisibility(false, false);
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);

      // Play promise rejects late:
      expect(() => {
        callbacks?.onBlocked({ name: 'NotAllowedError' });
      }).not.toThrow();
      expect(mock.onTransitionToPaused).not.toHaveBeenCalled();
    });

    it('handles synchronous cancellation during startNudge by immediately disposing returned handle', () => {
      const runtime = createTestRuntime();
      let cancelCalled = false;
      const reentrantBinding: ForegroundMediaBinding = {
        mediaId: 'vid-reentrant',
        startNudge: vi.fn(() => {
          // Reentrant synchronous hide during start!
          runtime.updateVisibility(false, false);
          return () => {
            cancelCalled = true;
          };
        }),
      };
      runtime.setMediaBinding(reentrantBinding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      expect(cancelCalled).toBe(true);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('exhaustion retries with the captured target', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      const target: PlaybackRetryTarget = { kind: 'vod', recordingId: 'rec-exhaust', explicitProfile: 'auto' };
      runtime.setTargetContext(target);

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);

      mock.getActiveCallbacks()?.onFailed();
      expect(onRetryMock).toHaveBeenCalledWith(target);
      expect(runtime.getActiveOperationId()).toBeNull();
    });
  });

  describe('5. Invalidation triggers and lifecycle fencing', () => {
    it('user pause during nudging stops shouldContinue and cancels active operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      const callbacks = mock.getActiveCallbacks();
      expect(callbacks?.shouldContinue()).toBe(true);

      runtime.setUserPaused(true);
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(callbacks?.shouldContinue()).toBe(false);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('user stop invalidates active operation and suppresses late callbacks', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      const callbacks = mock.getActiveCallbacks();

      runtime.onPlaybackStopped(epoch);
      stoppedEpochs.add(epoch);
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();

      callbacks?.onFailed();
      expect(onRetryMock).not.toHaveBeenCalled();
    });

    it('terminal auth invalidates active operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      runtime.onTerminalAuth(epoch);
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('media element swap cancels active operation on old element', () => {
      const runtime = createTestRuntime();
      const mock1 = createMockBinding('vid-1');
      runtime.setMediaBinding(mock1.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      const mock2 = createMockBinding('vid-2');
      runtime.setMediaBinding(mock2.binding);
      expect(mock1.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('executor refresh on SAME mediaId preserves ongoing active operation', () => {
      const runtime = createTestRuntime();
      const mock1 = createMockBinding('vid-1');
      runtime.setMediaBinding(mock1.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      // Re-render passes refreshed callbacks but same mediaId:
      const mock2 = createMockBinding('vid-1');
      runtime.setMediaBinding(mock2.binding);

      expect(mock1.cancelHandle).not.toHaveBeenCalled();
      expect(runtime.getActiveOperationId()).toBe(1);
    });

    it('superseding playback attempt invalidates older active operation', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      runtime.onPlaybackAttemptStarted(epoch + 1);
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();
    });

    it('dispose cleanly cancels active operation and detaches bindings', () => {
      const runtime = createTestRuntime();
      const mock = createMockBinding();
      runtime.setMediaBinding(mock.binding);
      runtime.setTargetContext({ kind: 'live', serviceRef: '1:0:1:A' });

      runtime.updateVisibility(false, false);
      runtime.updateVisibility(true, false);
      expect(runtime.getActiveOperationId()).toBe(1);

      runtime.dispose();
      expect(mock.cancelHandle).toHaveBeenCalledTimes(1);
      expect(runtime.getActiveOperationId()).toBeNull();
      expect(runtime.getCapturedTarget()).toBeNull();
    });
  });
});
