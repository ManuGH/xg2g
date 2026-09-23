import { useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createInitialPlaybackDomainState } from './playbackMachine';
import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand } from './playbackTypes';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';
import * as probeModule from '../utils/playbackNetworkProbe';
import type { HlsInstanceRef, VideoElementRef } from '../../../types/v3-player';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  Object.assign(HlsMock, { isSupported: vi.fn().mockReturnValue(true) });
  return { default: HlsMock };
});
const initial = () => ({ ...createInitialPlaybackDomainState(), epoch: { playback: 1, session: 0 } });
const transport = (): LiveSessionTransport => ({ fetchStreamInfo: vi.fn(), postStartIntent: vi.fn(), waitForReady: vi.fn(), postStopIntent: vi.fn().mockResolvedValue(undefined) });
const target = { kind: 'live' as const, serviceRef: 'channel-A' };
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it('does not cancel a current retry when stale beginPlaybackAttempt arrives', async () => {
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  const execute = vi.fn((command: PlaybackCommand) => command.type === 'command.playback.stop' ? pending : undefined);
  const controller = createPlaybackController({ transport: transport(), createInitialState: initial, executeCommand: execute });
  try {
    const current = controller.allocatePlaybackEpoch();
    controller.beginPlaybackAttempt(current, 'LIVE', 'playing', true);
    const retry = controller.retry(target);
    controller.beginPlaybackAttempt(1, 'VOD', 'starting', true);
    expect(controller.isRetryInFlight()).toBe(true);
    controller.cancelRetry();
    await retry;
  } finally { controller.dispose(); release(); }
});

it('coalesces a synchronous nested public stop without repeated teardown', async () => {
  let stopCommands = 0;
  const controllerRef: { current: PlaybackController | null } = { current: null };
  const controller = createPlaybackController({
    transport: transport(), createInitialState: initial,
    executeCommand: command => {
      if (command.type === 'command.playback.stop') {
        stopCommands += 1;
        // Bound the probe: an implementation bug must not exhaust the JS stack.
        if (stopCommands < 3) void controllerRef.current?.stop('user_stop');
      }
    },
  });
  controllerRef.current = controller;
  try {
    controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
    expect(await controller.retry(target)).toEqual({ status: 'cancelled', reason: 'user_stop' });
    expect(stopCommands).toBe(1);
  } finally { controller.dispose(); }
});

it('does not derive retry source from an attempt belonging to an older epoch', async () => {
  const t = transport();
  t.fetchStreamInfo = vi.fn(() => new Promise(() => {})) as any;
  const execute = vi.fn();
  const controller = createPlaybackController({ transport: t, createInitialState: initial, executeCommand: execute });
  try {
    void controller.startLive({ serviceRef: 'old-channel-A', epoch: 1 });
    const next = controller.allocatePlaybackEpoch();
    controller.beginPlaybackAttempt(next, 'LIVE', 'starting', true);
    // The new attempt has not supplied a source identity yet.
    const result = await controller.retry();
    expect(result).toEqual({ status: 'cancelled', reason: 'missing_target' });
    expect(execute.mock.calls.filter(([command]) => command.type === 'command.playback.start')).toEqual([]);
  } finally { controller.dispose(); }
});

it('keeps facade duplicate suppression until the retry preparation has finished', async () => {
  let releaseProbe!: (value: any) => void;
  const pendingProbe = new Promise<any>(resolve => { releaseProbe = resolve; });
  const probe = vi.spyOn(probeModule, 'measurePlaybackNetwork').mockResolvedValueOnce(undefined as any).mockReturnValue(pendingProbe);
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const data = String(url).includes('/live/stream-info')
      ? { mode: 'direct_stream', playbackDecisionToken: 'token', decision: { mode: 'direct_stream', playbackDecisionToken: 'token' } }
      : String(url).includes('/intents') ? { sessionId: 'test-session' } : {};
    return new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
  let exposed!: ReturnType<typeof usePlaybackOrchestrator>;
  function Host() {
    const containerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<VideoElementRef>(null);
    const hlsRef = useRef<HlsInstanceRef>(null);
    const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
    exposed = usePlaybackOrchestrator({ autoStart: false } as any, { containerRef, videoRef, hlsRef, resumePrimaryActionRef });
    return <div />;
  }
  const view = render(<Host />);
  try {
    await act(async () => { await exposed.actions.startStream('channel-A'); });
    expect(probe).toHaveBeenCalledTimes(1);
    await act(async () => { void exposed.actions.retry(); });
    expect(probe).toHaveBeenCalledTimes(2);
    await act(async () => { void exposed.actions.retry(); });
    // The first retry's preparation is still waiting on pendingProbe.
    expect(probe).toHaveBeenCalledTimes(2);
  } finally {
    view.unmount();
    await act(async () => { releaseProbe(null); });
  }
});
