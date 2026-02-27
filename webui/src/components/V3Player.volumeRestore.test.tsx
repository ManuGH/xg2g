import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../types/v3-player';

vi.mock('../client-ts/sdk.gen', () => ({
  createSession: vi.fn(),
  postRecordingPlaybackInfo: vi.fn(),
  postLivePlaybackInfo: vi.fn()
}));

describe('V3Player Volume Restore', () => {
  it('restores last non-zero volume when unmuting after slider hits zero', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const volumeSlider = screen.getByRole('slider');
    fireEvent.change(volumeSlider, { target: { value: '0.6' } });

    await waitFor(() => {
      expect((volumeSlider as HTMLInputElement).value).toBe('0.6');
    });

    fireEvent.change(volumeSlider, { target: { value: '0' } });

    await waitFor(() => {
      expect((volumeSlider as HTMLInputElement).value).toBe('0');
    });

    const unmuteButton = await screen.findByTitle('player.unmute');
    fireEvent.click(unmuteButton);

    await waitFor(() => {
      expect((volumeSlider as HTMLInputElement).value).toBe('0.6');
    });
  });
});
