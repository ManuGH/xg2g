import { createRef } from 'react';
import { renderHook, act } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import type { V3SessionStatusResponse, VideoElementRef } from '../../types/v3-player';
import { useLiveSessionController } from './useLiveSessionController';

const getSessionEventsMock = vi.hoisted(() => vi.fn());

vi.mock('../../client-ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../client-ts')>()),
  getSessionEvents: getSessionEventsMock,
}));

/** An SSE subscription that ends immediately without ever emitting an event. */
function streamThatEndsSilently() {
  return Promise.resolve({
    stream: (async function* () { /* closes at once */ })(),
  });
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

const priming: Partial<V3SessionStatusResponse> = { sessionId: 'sid-1', state: 'PRIMING' };

function renderController() {
  const { result } = renderHook(() =>
    useLiveSessionController({
      apiBase: 'http://test/api/v3',
      t: ((key: string) => key) as unknown as TFunction,
      videoRef: createRef<VideoElementRef>(),
      setPlaybackMode: vi.fn(),
      setDurationSeconds: vi.fn(),
      onSessionPhaseChanged: vi.fn(),
      clearPlaybackFailure: vi.fn(),
      reportPlaybackFailure: vi.fn(),
      readResponseBody: async (res: Response) => ({ json: await res.json().catch(() => null), text: null }),
      createPlayerError: (message: string, details?: unknown) =>
        Object.assign(new Error(message), { details }),
    }),
  );
  return result;
}

describe('waitForSessionReady when the SSE stream ends without a terminal event', () => {
  beforeEach(() => {
    getSessionEventsMock.mockReset();
    getSessionEventsMock.mockImplementation(streamThatEndsSilently);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Session 1619cee0 (2026-08-08): the session failed R_UPSTREAM_SCRAMBLED 80ms
  // before the 60s timer fired, but the stream had already ended without delivering
  // the terminal event, so the caller sat on the timer and the user was told the
  // session "was not ready in time" — hiding the one fact that mattered.
  it('rejects with the real session reason, not the generic timeout', async () => {
    const failed = {
      sessionId: 'sid-1',
      state: 'FAILED',
      reason: 'R_UPSTREAM_SCRAMBLED',
      reasonDetail: 'preflight failed scrambled',
    };
    let calls = 0;
    vi.stubGlobal('fetch', vi.fn(async () => {
      calls += 1;
      return jsonResponse(calls <= 3 ? priming : failed);
    }));

    const result = renderController();

    // A 2s budget, so a regression here fails by timing out.
    await act(async () => {
      await expect(result.current.waitForSessionReady('sid-1', 2_000)).rejects.toThrow(
        'player.sessionFailed: player.reason.R_UPSTREAM_SCRAMBLED',
      );
    });
  });

  // A dropped stream must not doom a session that is still on its way: the poll
  // fallback has to pick up the READY transition the stream never delivered.
  it('still resolves when the session becomes ready after the stream dropped', async () => {
    const ready = {
      sessionId: 'sid-1',
      state: 'READY',
      playbackUrl: 'http://test/hls/sid-1/index.m3u8',
      heartbeatIntervalSeconds: 10,
    };
    let calls = 0;
    vi.stubGlobal('fetch', vi.fn(async () => {
      calls += 1;
      return jsonResponse(calls <= 4 ? priming : ready);
    }));

    const result = renderController();

    await act(async () => {
      await expect(result.current.waitForSessionReady('sid-1', 2_000)).resolves.toMatchObject({
        state: 'READY',
        playbackUrl: ready.playbackUrl,
      });
    });
  });

  // The deadline still applies when the record never leaves PRIMING — the poll
  // fallback must not turn a lost stream into an unbounded wait.
  it('still times out when the session never becomes ready', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(priming)));

    const result = renderController();

    await act(async () => {
      await expect(result.current.waitForSessionReady('sid-1', 2_000)).rejects.toThrow(
        'player.sessionNotReadyInTime',
      );
    });
  });
});
