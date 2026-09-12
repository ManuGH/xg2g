// Independent review characterization for Step 3e-B (copy beside playbackController.ts to run).
// Result on the 3e-A baseline (14712c05): PASSES. controller.stop() during a VOD Retry-After
// wait settles within the 75 ms teardown window because Step 2 deliberately keeps start
// promises out of the command drain (playbackController.ts handleCommand returns undefined for
// command.playback.start). The React-side sleep keeps running for the full Retry-After duration
// (vi.getTimerCount() shows it) and is only neutralised by the post-await fences.
// Step 3e-B must keep this test green and additionally prove, through the controller's own
// inspection API, that the continuation is cancelled at stop time rather than fenced afterwards.
import { useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../../types/v3-player';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';
import type { PlaybackController } from './playbackController';

const { postRecordingPlaybackInfoMock } = vi.hoisted(() => ({ postRecordingPlaybackInfoMock: vi.fn() }));

vi.mock('../../../client-ts', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  postRecordingPlaybackInfo: postRecordingPlaybackInfoMock,
}));

vi.mock('../lib/hlsRuntime', () => {
  function HlsMock(this: any) {
    this.loadSource = vi.fn();
    this.attachMedia = vi.fn();
    this.on = vi.fn();
    this.startLoad = vi.fn();
    this.destroy = vi.fn();
  }
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = { MANIFEST_PARSED: 'p', LEVEL_SWITCHED: 'l', FRAG_BUFFERED: 'f', ERROR: 'e' };
  return { default: HlsMock };
});

const RETRY_AFTER_SECONDS = 30;

describe('Step 3e-B characterization: VOD Retry-After wait vs. controller.stop()', () => {
  let exposedController!: PlaybackController;

  function Harness({ props }: { props: V3PlayerProps }) {
    const containerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<VideoElementRef>(null);
    const hlsRef = useRef<HlsInstanceRef | null>(null);
    const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
    const { controller } = usePlaybackOrchestrator(props, { containerRef, videoRef, hlsRef, resumePrimaryActionRef });
    exposedController = controller;
    return (
      <div ref={containerRef}>
        <video ref={(el) => { if (el) { el.play = vi.fn().mockResolvedValue(undefined); el.pause = vi.fn(); } videoRef.current = el; }} />
      </div>
    );
  }

  beforeEach(() => {
    vi.useFakeTimers();
    postRecordingPlaybackInfoMock.mockReset();
    postRecordingPlaybackInfoMock.mockImplementation(async () => ({
      data: undefined,
      error: { error: 'recording_building' },
      response: { status: 503, headers: new Headers({ 'Retry-After': String(RETRY_AFTER_SECONDS) }) },
    }));
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, headers: new Headers(), json: async () => ({}), text: async () => '' }));
  });

  afterEach(() => {
    vi.clearAllTimers();
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('stop() during the Retry-After wait settles promptly and no stream-info request follows', async () => {
    render(<Harness props={{ autoStart: true, recordingId: 'rec-1', apiBase: 'http://test.local' } as unknown as V3PlayerProps} />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
    expect(exposedController.getDomainStatus()).toBe('building');

    let stopped = false;
    let stopPromise!: Promise<void>;
    act(() => {
      stopPromise = exposedController.stop('user_stop').then(() => { stopped = true; });
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(stopped).toBe(true);
    expect(exposedController.getDomainStatus()).toBe('stopped');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(RETRY_AFTER_SECONDS * 1_000 + 1_000);
      await stopPromise;
    });
    expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
  });
});
