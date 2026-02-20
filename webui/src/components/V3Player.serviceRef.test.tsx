import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import V3Player from './V3Player';
import type { V3PlayerProps } from '../types/v3-player';

vi.mock('../client-ts', () => ({
  createSession: vi.fn(),
  getRecordingPlaybackInfo: vi.fn()
}));

describe('V3Player ServiceRef Input', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    const headers = { get: vi.fn().mockReturnValue(null) };
    (globalThis as any).fetch = vi.fn().mockResolvedValue({
      status: 409,
      ok: false,
      headers,
      json: vi.fn().mockResolvedValue({ code: 'LEASE_BUSY', requestId: 'test' })
    });
  });

  afterEach(() => {
    (globalThis as any).fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('uses edited serviceRef when starting a live stream via Enter', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    const newRef = '1:0:1:1234:567:89AB:0:0:0:0:';
    fireEvent.change(input, { target: { value: newRef } });

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe(newRef);
    });

    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalled();
    });

    const [url, options] = (globalThis.fetch as any).mock.calls[0];
    expect(String(url)).toContain('/intents');
    const body = JSON.parse(options.body);
    expect(body.serviceRef).toBe(newRef);
  });

  it('uses edited serviceRef when starting a live stream via Start button', async () => {
    const props = { autoStart: false } as unknown as V3PlayerProps;
    render(<V3Player {...props} />);

    const input = screen.getByRole('textbox');
    const newRef = '1:0:1:9999:888:77AA:0:0:0:0:';
    fireEvent.change(input, { target: { value: newRef } });

    await waitFor(() => {
      expect((input as HTMLInputElement).value).toBe(newRef);
    });

    const startButton = screen.getByRole('button', { name: /common\.startStream/i });
    fireEvent.click(startButton);

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalled();
    });

    const [url, options] = (globalThis.fetch as any).mock.calls[0];
    expect(String(url)).toContain('/intents');
    const body = JSON.parse(options.body);
    expect(body.serviceRef).toBe(newRef);
  });
});
