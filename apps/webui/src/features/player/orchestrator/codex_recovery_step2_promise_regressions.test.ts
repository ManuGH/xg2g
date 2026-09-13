import { expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createInitialPlaybackDomainState } from './playbackMachine';
import type { LiveSessionTransport } from './liveSessionTransport';

const transport = (): LiveSessionTransport => ({ fetchStreamInfo: vi.fn(), postStartIntent: vi.fn(), waitForReady: vi.fn(), postStopIntent: vi.fn().mockResolvedValue(undefined) });
const initial = () => ({ ...createInitialPlaybackDomainState(), epoch: { playback: 1, session: 0 } });
const target = { kind: 'live' as const, serviceRef: 'channel-A' };

it.each([false, true])('settles reentrant stop when executor returns nested promise: %s', async (returnNestedPromise) => {
  const controllerRef: { current: PlaybackController | null } = { current: null };
  let stops = 0;
  const controller = createPlaybackController({
    transport: transport(), createInitialState: initial,
    executeCommand: command => {
      if (command.type === 'command.playback.stop') {
        stops += 1;
        const nested = controllerRef.current?.stop('user_stop');
        if (returnNestedPromise) return nested;
      }
    },
  });
  controllerRef.current = controller;
  try {
    controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
    const retryResult = await controller.retry(target);
    expect(retryResult).toEqual({ status: 'cancelled', reason: 'user_stop' });
    let settled = false;
    void controller.stop('user_stop').then(() => { settled = true; });
    for (let i = 0; i < 30; i += 1) await Promise.resolve();
    expect(stops).toBe(1);
    expect(settled).toBe(true);
  } finally { controller.dispose(); }
});

it('observes rejection of the asynchronous retry start without an unhandled rejection', async () => {
  let rejectStart!: (error: Error) => void;
  const start = new Promise<void>((_resolve, reject) => { rejectStart = reject; });
  const controller = createPlaybackController({
    transport: transport(), createInitialState: initial,
    executeCommand: command => {
      if (command.type === 'command.playback.start') {
        const epoch = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting', true);
        return start;
      }
    },
  });
  try {
    expect((await controller.retry(target)).status).toBe('restarted');
    expect(controller.isRetryInFlight()).toBe(true);
    rejectStart(new Error('review: asynchronous retry preparation rejected'));
    await start.catch(() => {});
    for (let i = 0; i < 5; i += 1) await Promise.resolve();
    expect(controller.isRetryInFlight()).toBe(false);
    // Vitest must complete with zero unhandled errors, including promises
    // derived by the controller's internal completion/cleanup handlers.
  } finally { controller.dispose(); }
});
