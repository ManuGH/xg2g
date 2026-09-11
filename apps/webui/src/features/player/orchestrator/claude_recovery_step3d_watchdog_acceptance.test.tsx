// Independent review (Step 3d acceptance): controller-owned network watchdog.
// Covers the matrix rows the implementer suites left implicit: activation parity with the
// removed React predicate (including the TV bypass and both publication orders), lifecycle
// cancellation through the real controller, start-command counts per recovery, guard
// equivalence with retry coalescing, adapter probe semantics, and the dispose/activate cycle.
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { buildPlaybackFailure, createInitialPlaybackDomainState } from './playbackMachine';
import {
  createPlaybackNetworkWatchdogRuntime,
  FIRST_PROBE_DELAY_MS,
  MAX_AUTOMATIC_NETWORK_RECOVERIES,
  shouldWatchForNetworkRecovery,
} from './playbackNetworkWatchdogRuntime';
import type { PlaybackCommand, PlaybackFailure } from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';
import { useNetworkRecoveryWatchdog } from './useNetworkRecoveryWatchdog';

const controllers: PlaybackController[] = [];

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.clearAllTimers();
  cleanup();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function networkFailure(code = 'NETWORK_TIMEOUT'): PlaybackFailure {
  return buildPlaybackFailure(
    { title: 'Network', code, status: undefined, retryable: true },
    'adapter',
    { recoverable: true },
  );
}

function authFailure(): PlaybackFailure {
  return buildPlaybackFailure(
    { title: 'Forbidden', code: 'SESSION_FORBIDDEN', status: 403, retryable: false },
    'orchestrator',
    { class: 'auth', terminal: true },
  );
}

function raiseFailure(controller: PlaybackController, failure: PlaybackFailure): void {
  controller.dispatch({
    type: 'normative.playback.failure.raised',
    epoch: controller.getEpoch(),
    failure,
    status: 'error',
  });
}

/** Real controller, fake probe port, start commands recorded through the executor. */
function createWatchedController(probe: () => Promise<boolean>) {
  const controller = createPlaybackController({
    createInitialState: () => ({
      ...createInitialPlaybackDomainState(),
      status: 'playing',
      playbackMode: 'LIVE',
    }),
  });
  controllers.push(controller);
  const startCommands: PlaybackCommand[] = [];
  controller.setCommandExecutor((command) => {
    if (command.type === 'command.playback.start') startCommands.push(command);
  });
  controller.setForegroundTarget({ kind: 'live', serviceRef: 'channel-a' });
  controller.setNetworkWatchdogContext({ platformEligible: true, intentKey: 'channel-a||', probe });
  return { controller, startCommands };
}

describe('Step 3d watchdog — activation parity with the removed React predicate', () => {
  const failures: Array<[string, PlaybackFailure | null]> = [
    ['null', null],
    ['terminal', buildPlaybackFailure({ title: 'x', code: 'X', status: undefined, retryable: false }, 'adapter', { terminal: true })],
    ['auth', authFailure()],
    ['server answered 410', buildPlaybackFailure({ title: 'x', code: 'RECORDING_GONE', status: 410, retryable: false }, 'backend', {})],
    ['request never arrived', networkFailure()],
    ['starvation code with status', buildPlaybackFailure({ title: 'x', code: 'HLS_NETWORK_RETRIES_EXHAUSTED', status: 500, retryable: true }, 'adapter', {})],
  ];
  const statuses: PlayerStatus[] = ['error', 'playing', 'buffering', 'stopped', 'idle'];

  it.each(
    [true, false].flatMap((eligible) =>
      statuses.flatMap((status) => failures.map(([label, failure]) => [eligible, status, label, failure] as const)),
    ),
  )('platformEligible=%s status=%s failure=%s', (eligible, status, _label, failure) => {
    const probe = vi.fn().mockResolvedValue(true);
    const runtime = createPlaybackNetworkWatchdogRuntime({
      getDomainStatus: () => status,
      getFailure: () => failure,
      getPlaybackEpoch: () => 1,
      isDisposed: () => false,
      onRecover: () => {},
      getTargetContext: () => ({ kind: 'live', serviceRef: 'x' }),
    });
    runtime.setContext({ platformEligible: eligible, intentKey: 'k', probe });

    // The removed predicate: !isTv && status === 'error' && shouldWatchForNetworkRecovery(failure)
    const expected = eligible && status === 'error' && shouldWatchForNetworkRecovery(failure);
    expect(runtime.getState().active).toBe(expected);
    expect(runtime.getState().timerPending).toBe(expected);
  });

  it.each(['context_then_domain', 'domain_then_context'] as const)(
    'either publication order (%s) yields one loop with one 5 s first delay',
    async (order) => {
      let status: PlayerStatus = 'playing';
      let failure: PlaybackFailure | null = null;
      const probe = vi.fn().mockResolvedValue(false);
      const runtime = createPlaybackNetworkWatchdogRuntime({
        getDomainStatus: () => status,
        getFailure: () => failure,
        getPlaybackEpoch: () => 1,
        isDisposed: () => false,
        onRecover: () => {},
        getTargetContext: () => null,
      });
      const publishContext = () => runtime.setContext({ platformEligible: true, intentKey: 'k', probe });
      const reachError = () => {
        status = 'error';
        failure = networkFailure();
        runtime.onDomainStateChanged();
      };
      if (order === 'context_then_domain') {
        publishContext();
        reachError();
      } else {
        reachError();
        publishContext();
      }

      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS - 1);
      expect(probe).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(1);
      // Exactly one loop: next probe at +10 s, not a second interleaved chain.
      await vi.advanceTimersByTimeAsync(9_999);
      expect(probe).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(2);
    },
  );
});

describe('Step 3d watchdog — lifecycle through the real controller', () => {
  it('a reachable probe issues exactly one start per recovery through retry(); a later failure opens a second loop', async () => {
    const probe = vi.fn().mockResolvedValue(true);
    const { controller, startCommands } = createWatchedController(probe);

    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: true, timerPending: true, attempt: 0 });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS);
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(probe).toHaveBeenCalledTimes(1);
    expect(startCommands).toHaveLength(1);
    expect(startCommands[0]).toMatchObject({ kind: 'live', serviceRef: 'channel-a' });
    expect(controller.getDomainStatus()).not.toBe('error');
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: false, recoveries: 1, loopEnded: true });

    // The retried start fails again: a genuine false -> true edge, second loop, second start.
    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: true, timerPending: true, attempt: 0 });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS);
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(probe).toHaveBeenCalledTimes(2);
    expect(startCommands).toHaveLength(2);
    expect(controller.getNetworkWatchdogState().recoveries).toBe(2);
  });

  it('stop() during the first delay cancels the loop: no probe, no start', async () => {
    const probe = vi.fn().mockResolvedValue(true);
    const { controller, startCommands } = createWatchedController(probe);

    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState().timerPending).toBe(true);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
      await controller.stop('user_stop');
    });
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: false, timerPending: false });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS * 2);
    });
    expect(probe).not.toHaveBeenCalled();
    expect(startCommands).toHaveLength(0);
  });

  it('terminal auth during the first delay deactivates the watchdog: no probe, no start', async () => {
    const probe = vi.fn().mockResolvedValue(true);
    const { controller, startCommands } = createWatchedController(probe);

    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState().active).toBe(true);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    act(() => raiseFailure(controller, authFailure()));
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: false, timerPending: false });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS * 2);
    });
    expect(probe).not.toHaveBeenCalled();
    expect(startCommands).toHaveLength(0);
  });

  it('stop() while a probe is in flight: the late reachable result is ignored, no start', async () => {
    let resolveProbe!: (reachable: boolean) => void;
    const probe = vi.fn(() => new Promise<boolean>((resolve) => { resolveProbe = resolve; }));
    const { controller, startCommands } = createWatchedController(probe);

    act(() => raiseFailure(controller, networkFailure()));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS);
    });
    expect(probe).toHaveBeenCalledTimes(1);
    expect(controller.getNetworkWatchdogState().probeInFlight).toBe(true);

    await act(async () => {
      await controller.stop('user_stop');
    });
    await act(async () => {
      resolveProbe(true);
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(startCommands).toHaveLength(0);
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: false, recoveries: 0, timerPending: false });
  });

  it('a same-target retry already in flight when the probe succeeds still yields exactly one start', async () => {
    let releaseStop!: () => void;
    const probe = vi.fn().mockResolvedValue(true);
    const { controller, startCommands } = createWatchedController(probe);
    // Hold the retry in its stopping phase so status stays 'error' while the probe fires.
    controller.setCommandExecutor((command) => {
      if (command.type === 'command.playback.start') startCommands.push(command);
      if (command.type === 'command.playback.stop') {
        return new Promise<void>((resolve) => { releaseStop = resolve; });
      }
    });

    act(() => raiseFailure(controller, networkFailure()));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS - 100);
    });
    let manualRetry!: Promise<unknown>;
    act(() => {
      manualRetry = controller.retry({ kind: 'live', serviceRef: 'channel-a' });
    });
    expect(controller.isRetryInFlight()).toBe(true);
    expect(controller.getDomainStatus()).toBe('error');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(probe).toHaveBeenCalledTimes(1);
    expect(controller.getNetworkWatchdogState()).toMatchObject({ recoveries: 1, loopEnded: true });

    await act(async () => {
      releaseStop();
      await vi.advanceTimersByTimeAsync(300);
      await manualRetry;
    });
    expect(startCommands).toHaveLength(1);
  });

  it('dispose() then activate(): the watchdog works again once the adapter republishes its context', async () => {
    const probe = vi.fn().mockResolvedValue(true);
    const { controller, startCommands } = createWatchedController(probe);

    controller.dispose();
    controller.activate();
    controller.setForegroundTarget({ kind: 'live', serviceRef: 'channel-a' });
    controller.setNetworkWatchdogContext({ platformEligible: true, intentKey: 'channel-a||', probe });

    // After dispose/activate the controller epoch runs ahead of the machine epoch until the next
    // start re-syncs them; a real post-activation failure carries the machine's epoch.
    act(() =>
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: controller.getState().epoch.playback,
        failure: networkFailure(),
        status: 'error',
      }),
    );
    expect(controller.getDomainStatus()).toBe('error');
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: true, timerPending: true });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS);
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(probe).toHaveBeenCalledTimes(1);
    expect(startCommands).toHaveLength(1);
  });

  it('budget: the fourth activation for one intent is refused without a probe; a new intent re-arms', async () => {
    const probe = vi.fn().mockResolvedValue(true);
    const { controller } = createWatchedController(probe);

    for (let i = 1; i <= MAX_AUTOMATIC_NETWORK_RECOVERIES; i += 1) {
      act(() => raiseFailure(controller, networkFailure()));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS);
        await vi.advanceTimersByTimeAsync(300);
      });
      expect(controller.getNetworkWatchdogState().recoveries).toBe(i);
    }
    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: true, timerPending: false });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS * 2);
    });
    expect(probe).toHaveBeenCalledTimes(MAX_AUTOMATIC_NETWORK_RECOVERIES);

    // New viewing intent returns the budget; the next activation edge probes again.
    controller.setNetworkWatchdogContext({ platformEligible: true, intentKey: 'channel-b||', probe });
    expect(controller.getNetworkWatchdogState().recoveries).toBe(0);
    await act(async () => {
      await controller.stop('user_stop');
    });
    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState().timerPending).toBe(true);
  });
});

describe('Step 3d watchdog — adapter probe port and platform fact', () => {
  function Host({ controller, apiBase, isTv }: { controller: PlaybackController; apiBase: string; isTv: boolean }) {
    useNetworkRecoveryWatchdog({ controller, apiBase, isTv, intentKey: 'k||' });
    return null;
  }

  function captureProbe(controller: PlaybackController) {
    const published: Array<Parameters<PlaybackController['setNetworkWatchdogContext']>[0]> = [];
    const original = controller.setNetworkWatchdogContext;
    controller.setNetworkWatchdogContext = (params) => {
      published.push(params);
      original(params);
    };
    return published;
  }

  it('publishes one memoized probe with the exact request shape and reachability semantics', async () => {
    const controller = createPlaybackController({ createInitialState: createInitialPlaybackDomainState });
    controllers.push(controller);
    const published = captureProbe(controller);
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const view = render(<Host controller={controller} apiBase="http://api.test" isTv={false} />);
    view.rerender(<Host controller={controller} apiBase="http://api.test" isTv={false} />);

    expect(published.length).toBeGreaterThan(0);
    const probes = new Set(published.map((p) => p.probe));
    expect(probes.size).toBe(1);
    expect(published.every((p) => p.platformEligible === true && p.intentKey === 'k||')).toBe(true);
    const probe = published[0]?.probe;
    if (!probe) throw new Error('adapter did not publish a probe');

    fetchMock.mockResolvedValueOnce({ ok: true, status: 200 });
    await expect(probe()).resolves.toBe(true);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://api.test/system/healthz');
    expect(init).toMatchObject({ method: 'HEAD', cache: 'no-store', credentials: 'same-origin' });
    expect(init.signal).toBeInstanceOf(AbortSignal);

    fetchMock.mockResolvedValueOnce({ ok: false, status: 204 });
    await expect(probe()).resolves.toBe(true);
    fetchMock.mockResolvedValueOnce({ ok: false, status: 503 });
    await expect(probe()).resolves.toBe(false);
    fetchMock.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    await expect(probe()).resolves.toBe(false);

    vi.unstubAllGlobals();
  });

  it('isTv publishes platformEligible=false: an error never arms the watchdog', async () => {
    const controller = createPlaybackController({
      createInitialState: () => ({ ...createInitialPlaybackDomainState(), status: 'playing', playbackMode: 'LIVE' }),
    });
    controllers.push(controller);
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal('fetch', fetchMock);

    render(<Host controller={controller} apiBase="http://api.test" isTv />);
    act(() => raiseFailure(controller, networkFailure()));
    expect(controller.getNetworkWatchdogState()).toMatchObject({ active: false, timerPending: false });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FIRST_PROBE_DELAY_MS * 2);
    });
    expect(fetchMock).not.toHaveBeenCalled();

    vi.unstubAllGlobals();
  });
});
