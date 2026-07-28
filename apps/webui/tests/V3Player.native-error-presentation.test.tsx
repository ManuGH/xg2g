import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import V3Player from '../src/features/player/components/V3Player';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(false);
  (HlsMock as any).Events = {};
  (HlsMock as any).ErrorTypes = {};
  return { default: HlsMock };
});

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    postRecordingPlaybackInfo: vi.fn(),
    postLivePlaybackInfo: vi.fn(),
    getSessionStatus: vi.fn(),
    postSessionHeartbeat: vi.fn(),
  };
});

describe('V3Player native media error presentation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') {
        return 'probably';
      }
      return '';
    });
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows a user-facing unsupported-source message for media error 4', async () => {
    const { container } = render(
      <V3Player
        autoStart={true}
        src="https://example.test/api/v3/recordings/rec-1/playlist.m3u8"
      />
    );

    const video = container.querySelector('video') as HTMLVideoElement | null;
    expect(video).not.toBeNull();
    if (!video) {
      return;
    }

    Object.defineProperty(video, 'currentSrc', {
      configurable: true,
      get: () => 'https://example.test/api/v3/recordings/rec-1/playlist.m3u8',
    });
    Object.defineProperty(video, 'error', {
      configurable: true,
      get: () => ({
        code: 4,
        message: 'MEDIA_ERR_SRC_NOT_SUPPORTED',
      }),
    });

    fireEvent.error(video);

    await waitFor(() => {
      expect(screen.getByText(/This device rejected the stream source/i)).toBeInTheDocument();
    });
  });
});
