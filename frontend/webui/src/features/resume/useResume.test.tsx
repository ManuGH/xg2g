import { fireEvent, render, waitFor } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useResume } from './useResume';

const { saveResume } = vi.hoisted(() => ({
  saveResume: vi.fn()
}));

vi.mock('./api', () => ({
  saveResume
}));

function ResumeHarness() {
  const videoRef = useRef<HTMLVideoElement>(null);

  useResume({
    recordingId: 'rec-123',
    duration: 120,
    videoRef,
    isPlaying: false,
    isSeekable: true
  });

  return <video ref={videoRef} />;
}

describe('useResume', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('binds video listeners from the ref after mount', async () => {
    const { container } = render(<ResumeHarness />);
    const video = container.querySelector('video');

    expect(video).not.toBeNull();
    Object.defineProperty(video, 'currentTime', {
      configurable: true,
      value: 42,
      writable: true
    });

    fireEvent.pause(video as HTMLVideoElement);

    await waitFor(() => {
      expect(saveResume).toHaveBeenCalledWith('rec-123', {
        position: 42,
        total: 120,
        finished: false
      });
    });
  });
});
