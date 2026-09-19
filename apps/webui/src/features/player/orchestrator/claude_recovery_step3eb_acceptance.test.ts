// Independent review (Step 3e-B acceptance): controller-owned start continuation.
// Pins the epoch authority that 3e-B's first cut loosened: beginPlaybackAttempt never adopts an
// epoch the controller did not allocate, so a pending continuation for the current epoch is
// neither cancelled nor superseded by such a call. Epochs advance only through
// allocatePlaybackEpoch / stop / dispose, and that is where continuations are cancelled.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController, type StartContinuationOutcome } from './playbackController';
import { createInitialPlaybackDomainState } from './playbackMachine';

const controllers: PlaybackController[] = [];

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.clearAllTimers();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.useRealTimers();
});

function createController(): PlaybackController {
  const controller = createPlaybackController({
    createInitialState: () => ({ ...createInitialPlaybackDomainState(), status: 'building', playbackMode: 'VOD' }),
  });
  controllers.push(controller);
  return controller;
}

function schedule(controller: PlaybackController, epoch: number, delayMs = 10_000) {
  let outcome: StartContinuationOutcome | undefined;
  const promise = controller.scheduleStartContinuation({ epoch, delayMs, reason: 'recording_retry_after' }).then((result) => {
    outcome = result;
    return result;
  });
  return { promise, outcome: () => outcome };
}

describe('Step 3e-B — epoch authority stays with the controller', () => {
  it('beginPlaybackAttempt with an epoch the controller never allocated is ignored: no epoch change, no state change, continuation untouched', async () => {
    const controller = createController();
    const epoch = controller.getEpoch();
    const statusBefore = controller.getState().status;
    const pending = schedule(controller, epoch);
    expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch);

    controller.beginPlaybackAttempt(epoch + 5, 'LIVE', 'starting');

    expect(controller.getEpoch()).toBe(epoch);
    expect(controller.getState().status).toBe(statusBefore);
    expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch);
    await Promise.resolve();
    expect(pending.outcome()).toBeUndefined();

    await vi.advanceTimersByTimeAsync(10_000);
    expect(await pending.promise).toBe('proceed');
  });

  it('beginPlaybackAttempt with a stale (older) epoch is ignored as before', async () => {
    const controller = createController();
    const oldEpoch = controller.getEpoch();
    const newEpoch = controller.allocatePlaybackEpoch();
    expect(newEpoch).toBeGreaterThan(oldEpoch);
    const pending = schedule(controller, newEpoch);

    controller.beginPlaybackAttempt(oldEpoch, 'LIVE', 'starting');

    expect(controller.getEpoch()).toBe(newEpoch);
    expect(controller.getPendingStartContinuation()?.epoch).toBe(newEpoch);
    await Promise.resolve();
    expect(pending.outcome()).toBeUndefined();
  });

  it('the allocated epoch, not beginPlaybackAttempt, is what cancels the previous continuation', async () => {
    const controller = createController();
    const epoch = controller.getEpoch();
    const pending = schedule(controller, epoch);

    const next = controller.allocatePlaybackEpoch();
    expect(controller.getPendingStartContinuation()).toBeNull();
    await Promise.resolve();
    expect(pending.outcome()).toBe('cancelled');

    // The attempt for the allocated epoch proceeds normally and schedules its own continuation.
    controller.beginPlaybackAttempt(next, 'VOD', 'building');
    expect(controller.getEpoch()).toBe(next);
    const second = schedule(controller, next, 5_000);
    expect(controller.getPendingStartContinuation()?.epoch).toBe(next);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(await second.promise).toBe('proceed');
  });

  it('never holds two pending continuations: scheduling for the current epoch after an older one was left behind cancels the older one', async () => {
    const controller = createController();
    const first = schedule(controller, controller.getEpoch());
    controller.allocatePlaybackEpoch();
    await Promise.resolve();
    expect(first.outcome()).toBe('cancelled');
    const current = schedule(controller, controller.getEpoch());
    expect(controller.getPendingStartContinuation()?.epoch).toBe(controller.getEpoch());
    controller.cancelStartContinuation();
    await Promise.resolve();
    expect(current.outcome()).toBe('cancelled');
    expect(controller.getPendingStartContinuation()).toBeNull();
  });
});
