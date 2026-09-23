// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createPlaybackController,
  type PlaybackController,
  type ScheduleAutoFallbackCommand,
  type AutoFallbackRestartTarget,
} from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import { buildPlaybackFailure } from './playbackMachine';
import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand, PlaybackDomainState } from './playbackTypes';

function createInitialState(): PlaybackDomainState {
  return {
    epoch: { playback: 1, session: 0 },
    traceId: '-',
    status: 'idle',
    playbackMode: 'UNKNOWN',
    vodStreamMode: null,
    activeHlsEngine: null,
    durationSeconds: null,
    canSeek: false,
    startUnix: null,
    sessionPhase: 'idle',
    mediaPhase: 'idle',
    contract: null,
    failure: null,
    lastAdvisory: null,
    explicitProfilePinned: false,
    hasSessionIntent: false,
    recovery: createRecoveryLadderState(),
    leaseExpiresAt: null,
    connectionLost: false,
  };
}

function createMockTransport(): LiveSessionTransport {
  return {
    fetchStreamInfo: vi.fn().mockResolvedValue({
      status: 200,
      data: { mode: 'direct_stream', playbackDecisionToken: 'tok-1' },
      headers: new Headers(),
    }),
    postStartIntent: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1' },
      headers: new Headers(),
    }),
    waitForReady: vi.fn().mockResolvedValue({
      sessionId: 's-mock-1',
      playbackUrl: 'http://example.test/stream.m3u8',
      heartbeatIntervalSeconds: 5,
      leaseExpiresAt: '2026-09-09T12:00:00Z',
    }),
    postStopIntent: vi.fn().mockResolvedValue(undefined),
    postHeartbeat: vi.fn().mockResolvedValue({
      status: 200,
      data: { acknowledged: true, sessionId: 's-mock-1', leaseExpiresAt: '2026-09-09T12:05:00Z' },
      headers: new Headers(),
    }),
    fetchSessionSnapshot: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1', state: 'READY' },
      headers: new Headers(),
    }),
  };
}

describe('PlaybackController: Automatic Recovery Fallback Timers (Step 1)', () => {
  let transport: LiveSessionTransport;
  let executedCommands: PlaybackCommand[];
  let controller: PlaybackController;

  beforeEach(() => {
    vi.useFakeTimers();
    transport = createMockTransport();
    executedCommands = [];
    controller = createPlaybackController({
      transport,
      createInitialState,
      executeCommand: (cmd) => {
        executedCommands.push(cmd);
      },
    });
  });

  afterEach(() => {
    controller?.dispose();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  describe('Scenario 1: Automatic fallback delay', () => {
    it('does not restart before the deadline and fires exactly one restart at the deadline', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const scheduleCmd: ScheduleAutoFallbackCommand = {
        type: 'command.playback.schedule_auto_fallback',
        epoch: 1,
        delayMs: 1500,
        profile: null,
        failureCode: 'MEDIA_ERR_DECODE',
        failureClass: 'decode',
      };
      const target: AutoFallbackRestartTarget = {
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        explicitProfile: undefined,
      };

      controller.scheduleAutoFallback(scheduleCmd, target);
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // Advance to 1499ms: no start command yet
      vi.advanceTimersByTime(1499);
      const startCommandsBefore = executedCommands.filter(
        (c) => c.type === 'command.playback.start',
      );
      expect(startCommandsBefore).toHaveLength(0);

      // Advance 1ms to reach 1500ms deadline: exactly one start command dispatched
      vi.advanceTimersByTime(1);
      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      const startCommandsAtDeadline = executedCommands.filter(
        (c) => c.type === 'command.playback.start',
      );
      expect(startCommandsAtDeadline).toHaveLength(1);
      expect(startCommandsAtDeadline[0]).toMatchObject({
        type: 'command.playback.start',
        epoch: 1,
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
      });

      // Advance further 5000ms: no duplicate restart occurs
      vi.advanceTimersByTime(5000);
      const startCommandsAfter = executedCommands.filter(
        (c) => c.type === 'command.playback.start',
      );
      expect(startCommandsAfter).toHaveLength(1);
    });
  });

  describe('Scenario 2: Session vs profile fallback delays and budgets', () => {
    it('preserves 1500ms session re-establishment delay and increments sessionRestarts count', () => {
      controller.setCommandExecutor((cmd) => {
        executedCommands.push(cmd);
        if (cmd.type === 'command.playback.schedule_auto_fallback') {
          controller.scheduleAutoFallback(cmd, {
            kind: 'live',
            serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
          });
        }
      });
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
      // Transition session phase to ready
      controller.dispatch({
        type: 'normative.session.phase.changed',
        playbackEpoch: 1,
        sessionEpoch: 0,
        phase: 'ready',
      });

      // Raise session lapse / reaped failure
      const failure = buildPlaybackFailure(
        {
          code: 'SESSION_REAPED',
          status: 409,
          retryable: true,
        } as any,
        'native-host',
        {
          class: 'session',
          recoverable: true,
          terminal: false,
        },
      );

      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 1,
        failure,
      });

      // Machine state transitions to 'recovering' with sessionRestarts = 1
      expect(controller.getState().status).toBe('recovering');
      expect(controller.getState().recovery.sessionRestarts).toBe(1);
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // Verify delay is 1500ms
      vi.advanceTimersByTime(1499);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);

      vi.advanceTimersByTime(1);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(1);
    });

    it('preserves 250ms profile/bandwidth fallback delay and marks autoFallbackUsed', () => {
      controller.setCommandExecutor((cmd) => {
        executedCommands.push(cmd);
        if (cmd.type === 'command.playback.schedule_auto_fallback') {
          controller.scheduleAutoFallback(cmd, {
            kind: 'live',
            serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
          });
        }
      });
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true, false);

      const failure = buildPlaybackFailure(
        {
          title: 'Decoder exhausted',
          code: 'DECODE_EXHAUSTED',
          retryable: true,
        },
        'media-element',
        {
          recoverable: true,
        },
      );

      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 1,
        failure,
      });

      expect(controller.getState().status).toBe('recovering');
      expect(controller.getState().recovery.autoFallbackUsed).toBe(true);
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // Verify delay is 250ms
      vi.advanceTimersByTime(249);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);

      vi.advanceTimersByTime(1);
      const startCmds = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCmds).toHaveLength(1);
      expect((startCmds[0] as any).explicitProfile).toBe('repair');
    });
  });

  describe('Scenario 3: Stop during delay', () => {
    it('settles stop immediately without waiting for delay and cancels pending restart', async () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1500,
          profile: null,
          failureCode: 'MEDIA_ERR_DECODE',
          failureClass: 'decode',
        },
        {
          kind: 'live',
          serviceRef: 'channel-test',
        },
      );
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // Call stop during the delay
      const stopPromise = controller.stop('user_stop');
      // Verify stop settles promptly (does not wait 1500ms)
      await stopPromise;

      expect(controller.hasScheduledAutoFallback(1)).toBe(false);
      expect(controller.getState().status).toBe('stopped');

      // Advance timers by 2000ms: no start command should ever be emitted
      vi.advanceTimersByTime(2000);
      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands).toHaveLength(0);
    });
  });

  describe('Scenario 4: Channel / source replacement', () => {
    it('cancels scheduled retry when new epoch/channel supersedes the failing one', () => {
      // Channel A running on epoch 1
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1500,
          profile: null,
          failureCode: 'MEDIA_ERR_NETWORK',
          failureClass: 'network',
        },
        {
          kind: 'live',
          serviceRef: 'channel-A',
        },
      );
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // At T+500ms, user tunes to Channel B (allocating epoch 2)
      vi.advanceTimersByTime(500);
      const epoch2 = controller.allocatePlaybackEpoch();
      expect(epoch2).toBe(2);
      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      controller.beginPlaybackAttempt(2, 'LIVE', 'starting', true);

      // Advance past T+1500ms
      vi.advanceTimersByTime(1500);

      // No start command should have been dispatched for channel A or channel B
      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands).toHaveLength(0);
    });
  });

  describe('Scenario 5: Terminal auth failure during delay', () => {
    it('cancels pending retry upon terminal auth error', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1500,
          profile: null,
          failureCode: 'MEDIA_ERR_NETWORK',
          failureClass: 'network',
        },
        {
          kind: 'live',
          serviceRef: 'channel-test',
        },
      );
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // At T+300ms, terminal auth failure is raised
      vi.advanceTimersByTime(300);
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 1,
        failure: buildPlaybackFailure({ code: 'SESSION_FORBIDDEN', status: 403 } as any, 'native-host', {
          class: 'auth',
          terminal: true,
        }),
      });

      // Auth failures are terminal; recovery ladder decides 'terminal'
      expect(controller.getState().status).toBe('error');
      // Real terminal auth event cancelled the pending fallback
      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      // Advance timers past original deadline: no start command is emitted
      vi.advanceTimersByTime(2000);
      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands).toHaveLength(0);

      // A fresh explicit start remains possible on next epoch
      const epoch2 = controller.allocatePlaybackEpoch();
      controller.beginPlaybackAttempt(epoch2, 'LIVE', 'starting', true);
      expect(controller.getState().status).toBe('starting');
      expect(controller.getState().epoch.playback).toBe(2);
    });
  });

  describe('Scenario 6: Repeated failure/schedule (Duplicate replacement policy)', () => {
    it('replaces existing pending timer for the same epoch without retry explosion', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      // First schedule: delay 1500ms at T=0
      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1500,
          profile: null,
          failureCode: 'ERR_1',
          failureClass: 'network',
        },
        {
          kind: 'live',
          serviceRef: 'channel-1',
          explicitProfile: '1080p',
        },
      );
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      // At T=500ms, a second schedule command arrives for epoch 1 with delay 250ms and profile 'repair'
      vi.advanceTimersByTime(500);
      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 250,
          profile: 'repair',
          failureCode: 'ERR_2',
          failureClass: 'decode',
        },
        {
          kind: 'live',
          serviceRef: 'channel-1',
          explicitProfile: 'repair',
        },
      );

      // At T=749ms (500 + 249): no start command
      vi.advanceTimersByTime(249);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);

      // At T=750ms (500 + 250): the replacement timer fires!
      vi.advanceTimersByTime(1);
      const startCommandsAt750 = executedCommands.filter(
        (c) => c.type === 'command.playback.start',
      );
      expect(startCommandsAt750).toHaveLength(1);
      expect((startCommandsAt750[0] as any).explicitProfile).toBe('repair');

      // At T=1500ms (original deadline): verify the old timer does NOT fire
      vi.advanceTimersByTime(750);
      const startCommandsAt1500 = executedCommands.filter(
        (c) => c.type === 'command.playback.start',
      );
      expect(startCommandsAt1500).toHaveLength(1); // Still exactly 1!
    });
  });

  describe('Scenario 7: Executor updates and synchronous dispatch', () => {
    it('dispatches restart through the current committed executor across updates', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
      const initialCommandCount = executedCommands.length;

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1000,
          profile: null,
          failureCode: 'ERR_1',
          failureClass: 'network',
        },
        {
          kind: 'live',
          serviceRef: 'channel-1',
        },
      );

      // Replace executor mid-flight (simulating React rerender with fresh callback)
      const lateExecutorCommands: PlaybackCommand[] = [];
      controller.setCommandExecutor((cmd) => {
        lateExecutorCommands.push(cmd);
      });

      // Advance to deadline
      vi.advanceTimersByTime(1000);

      // New executor received the restart command
      expect(lateExecutorCommands).toHaveLength(1);
      expect(lateExecutorCommands[0]?.type).toBe('command.playback.start');
      // Old executor did not receive additional commands after being replaced
      expect(executedCommands).toHaveLength(initialCommandCount);
    });

    it('ensures normal command dispatch remains synchronous and does not block on timers', () => {
      let syncDispatched = false;
      controller.setCommandExecutor((cmd) => {
        if (cmd.type === 'command.playback.stop') {
          syncDispatched = true;
        }
      });

      // Dispatching intent.stop.requested completes synchronously
      controller.dispatch({
        type: 'intent.stop.requested',
        epoch: 1,
        reason: 'user_stop',
        notifyClose: false,
      });

      expect(syncDispatched).toBe(true);
    });
  });

  describe('Scenario 8: Lifecycle and disposal', () => {
    it('cancels pending timers on dispose and prevents post-disposal restart', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 1500,
          profile: null,
          failureCode: 'MEDIA_ERR_DECODE',
          failureClass: 'decode',
        },
        {
          kind: 'live',
          serviceRef: 'channel-test',
        },
      );
      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      controller.dispose();
      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(3000);
      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands).toHaveLength(0);
    });
  });

  describe('Scenario 9: Live / VOD / src compatibility', () => {
    it('faithfully routes VOD target on fallback restart', () => {
      controller.beginPlaybackAttempt(1, 'VOD', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 500,
          profile: null,
          failureCode: 'MEDIA_ERR_SRC_NOT_SUPPORTED',
          failureClass: 'fatal',
        },
        {
          kind: 'vod',
          recordingId: 'rec-xyz-123',
        },
      );

      vi.advanceTimersByTime(500);

      const startCmds = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCmds).toHaveLength(1);
      expect(startCmds[0]).toMatchObject({
        type: 'command.playback.start',
        epoch: 1,
        kind: 'vod',
        recordingId: 'rec-xyz-123',
      });
    });

    it('faithfully routes src target on fallback restart', () => {
      controller.beginPlaybackAttempt(1, 'UNKNOWN', 'buffering', false);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 500,
          profile: null,
          failureCode: 'MEDIA_ERR_NETWORK',
          failureClass: 'network',
        },
        {
          kind: 'src',
          srcUrl: 'https://example.test/direct.mp4',
        },
      );

      vi.advanceTimersByTime(500);

      const startCmds = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCmds).toHaveLength(1);
      expect(startCmds[0]).toMatchObject({
        type: 'command.playback.start',
        epoch: 1,
        kind: 'src',
        srcUrl: 'https://example.test/direct.mp4',
      });
    });
  });

  describe('Scenario 10: Missing source identity and valid supplied targets', () => {
    it('does not schedule or emit a restart when VOD identity is missing', () => {
      controller.beginPlaybackAttempt(1, 'VOD', 'playing', true);

      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 1,
        failure: buildPlaybackFailure(
          { title: 'Decoder exhausted', code: 'DECODE_EXHAUSTED', retryable: true },
          'media-element',
          { recoverable: true },
        ),
      });

      expect(controller.getState().status).toBe('recovering');
      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(2000);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toEqual([]);
    });

    it('does not schedule or emit a restart when VOD target has empty or whitespace recordingId', () => {
      controller.beginPlaybackAttempt(1, 'VOD', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 250,
          profile: null,
          failureCode: 'DECODE_EXHAUSTED',
          failureClass: 'decode',
        },
        {
          kind: 'vod',
          recordingId: '   ',
        },
      );

      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(2000);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toEqual([]);
    });

    it('does not schedule or emit a restart when Live identity is missing and no in-flight attempt exists', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback({
        type: 'command.playback.schedule_auto_fallback',
        epoch: 1,
        delayMs: 250,
        profile: null,
        failureCode: 'MEDIA_ERR_DECODE',
        failureClass: 'decode',
      });

      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(2000);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toEqual([]);
    });

    it('does not schedule or emit a restart when Live target has empty or whitespace serviceRef', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 250,
          profile: null,
          failureCode: 'MEDIA_ERR_DECODE',
          failureClass: 'decode',
        },
        {
          kind: 'live',
          serviceRef: '   ',
        },
      );

      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(2000);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toEqual([]);
    });

    it('does not schedule or emit a restart when src target has empty or whitespace srcUrl', () => {
      controller.beginPlaybackAttempt(1, 'UNKNOWN', 'buffering', false);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 250,
          profile: null,
          failureCode: 'MEDIA_ERR_NETWORK',
          failureClass: 'network',
        },
        {
          kind: 'src',
          srcUrl: '   ',
        },
      );

      expect(controller.hasScheduledAutoFallback(1)).toBe(false);

      vi.advanceTimersByTime(2000);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toEqual([]);
    });

    it('does not derive live identity from an earlier epoch attempt (no cross-epoch pollution)', async () => {
      // Epoch 1: Live attempt started via startLive
      const startPromise = controller.startLive({
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        epoch: 1,
      });

      // Advance time slightly to let startup commence
      vi.advanceTimersByTime(10);

      // Now tune away / allocate epoch 2 for VOD
      const epoch2 = controller.allocatePlaybackEpoch();
      expect(epoch2).toBe(2);
      controller.beginPlaybackAttempt(epoch2, 'VOD', 'playing', true);

      // Settle epoch 1 promise (superseded)
      await startPromise;

      // Epoch 2 receives fallback command without target
      controller.scheduleAutoFallback({
        type: 'command.playback.schedule_auto_fallback',
        epoch: 2,
        delayMs: 250,
        profile: null,
        failureCode: 'DECODE_EXHAUSTED',
        failureClass: 'decode',
      });

      // No fallback should be scheduled using epoch 1's live service reference
      expect(controller.hasScheduledAutoFallback(2)).toBe(false);

      vi.advanceTimersByTime(2000);
      const starts = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(starts).toEqual([]);
    });

    it('schedules and emits restart when authoritative live attempt matches current epoch', () => {
      // Live attempt started with authoritative serviceRef in currentAttempt
      void controller.startLive({
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        epoch: 1,
      });

      // Headless schedule command without target for matching epoch 1
      controller.scheduleAutoFallback({
        type: 'command.playback.schedule_auto_fallback',
        epoch: 1,
        delayMs: 500,
        profile: 'repair',
        failureCode: 'MEDIA_ERR_DECODE',
        failureClass: 'decode',
      });

      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      vi.advanceTimersByTime(500);

      const starts = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(starts).toHaveLength(1);
      expect(starts[0]).toMatchObject({
        type: 'command.playback.start',
        epoch: 1,
        kind: 'live',
        serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
        explicitProfile: 'repair',
      });
    });

    it('faithfully schedules and emits restart when explicit valid target is supplied', () => {
      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      controller.scheduleAutoFallback(
        {
          type: 'command.playback.schedule_auto_fallback',
          epoch: 1,
          delayMs: 500,
          profile: 'low',
          failureCode: 'MEDIA_ERR_DECODE',
          failureClass: 'decode',
        },
        {
          kind: 'live',
          serviceRef: 'channel-explicit',
          explicitProfile: 'high',
        },
      );

      expect(controller.hasScheduledAutoFallback(1)).toBe(true);

      vi.advanceTimersByTime(500);

      const starts = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(starts).toHaveLength(1);
      // Command profile ('low') takes precedence over target explicitProfile ('high')
      expect(starts[0]).toMatchObject({
        type: 'command.playback.start',
        epoch: 1,
        kind: 'live',
        serviceRef: 'channel-explicit',
        explicitProfile: 'low',
      });
    });
  });
});
