import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../../src/components/V3Player';
import * as sdk from '../../src/client-ts';
import * as resumeApi from '../../src/features/resume/api';

vi.mock('../../src/client-ts', async () => {
  return {
    createSession: vi.fn(),
    postRecordingPlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

vi.mock('../../src/features/resume/api', async () => {
  return {
    saveResume: vi.fn(),
  };
});

function mockResponse(status: number, body: Record<string, unknown> = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: () => null
    },
    json: async () => body,
    text: async () => JSON.stringify(body)
  };
}

describe('Gate O Phase 2: Seek/Resume Proof', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.clearAllMocks();

    (sdk.createSession as any).mockResolvedValue({
      data: {},
      response: { status: 200, headers: new Map() }
    });

    (globalThis as any).fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes('/intents')) {
        return Promise.resolve(mockResponse(200, { sessionId: 'unused' }));
      }
      return Promise.resolve(mockResponse(200, {}));
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  async function renderRecordingPlayer(options: {
    recordingId: string;
    isSeekable: boolean;
    durationMs: number;
    resumePosSeconds?: number;
  }) {
    const { recordingId, isSeekable, durationMs, resumePosSeconds } = options;

    (sdk.postRecordingPlaybackInfo as any).mockResolvedValue({
      data: {
        requestId: `req-${recordingId}`,
        sessionId: `sess-${recordingId}`,
        mode: 'direct_mp4',
        isSeekable,
        durationMs,
        resume: resumePosSeconds
          ? {
              posSeconds: resumePosSeconds,
              durationSeconds: Math.floor(durationMs / 1000),
              finished: false
            }
          : undefined,
        decision: {
          selectedOutputUrl: `/vod/${recordingId}.mp4`,
          selectedOutputKind: 'file'
        }
      }
    });

    render(<V3Player autoStart={true} recordingId={recordingId} />);

    await waitFor(() => {
      expect(sdk.postRecordingPlaybackInfo as any).toHaveBeenCalled();
    });

    await waitFor(() => {
      const calls = (globalThis.fetch as any).mock.calls.map((c: any[]) => String(c[0]));
      expect(calls.some((u: string) => u.includes(`/vod/${recordingId}.mp4`))).toBe(true);
    });

    const video = document.querySelector('video') as HTMLVideoElement | null;
    expect(video).toBeTruthy();
    return video as HTMLVideoElement;
  }

  it('does not expose seek/resume actions and does not save resume when DTO says isSeekable=false', async () => {
    const video = await renderRecordingPlayer({
      recordingId: 'rec-gate-o-1',
      isSeekable: false,
      durationMs: 60_000,
      resumePosSeconds: 42
    });

    expect(screen.queryByText(/player\.resumeTitle/i)).not.toBeInTheDocument();
    expect(screen.queryByTitle('player.seekBack15s')).not.toBeInTheDocument();

    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      writable: true,
      value: 35
    });
    fireEvent.pause(video);

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(resumeApi.saveResume).not.toHaveBeenCalled();
  });

  it('does not client-clamp resume/save payload to duration when saving progress', async () => {
    const video = await renderRecordingPlayer({
      recordingId: 'rec-gate-o-2',
      isSeekable: true,
      durationMs: 60_000,
      resumePosSeconds: 120
    });

    await waitFor(() => {
      expect(screen.getByTitle('player.seekBack15s')).toBeInTheDocument();
    });
    expect(screen.queryByText(/player\.resumeTitle/i)).not.toBeInTheDocument();

    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      writable: true,
      value: 120
    });
    fireEvent.pause(video);

    await waitFor(() => {
      expect(resumeApi.saveResume).toHaveBeenCalled();
    });
    expect(resumeApi.saveResume).toHaveBeenCalledWith(
      'rec-gate-o-2',
      expect.objectContaining({
        position: 120,
        total: 60,
        finished: true
      })
    );
  });
});
