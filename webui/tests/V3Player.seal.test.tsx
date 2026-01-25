import React from 'react';
import { render, waitFor } from '@testing-library/react';
import V3Player from '../src/components/V3Player';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock fetch
const originalFetch = global.fetch;

describe('V3Player Truth Sealing (UI-INV-PLAYER-001)', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ sessionId: '123' })
    }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    global.fetch = originalFetch;
  });

  it('gating: does not auto-start if no explicit source is provided', async () => {
    // Render with autostart but NO channel/src/recordingId
    render(<V3Player autoStart={true} />);

    // Deterministic Verification: Flush microtasks to settle effects
    await Promise.resolve();
    await Promise.resolve();

    const fetchCalls = (global.fetch as any).mock.calls;
    const hasIntentsPost = fetchCalls.some((call: any) =>
      call[0].includes('/intents') && call[1]?.method === 'POST'
    );

    expect(hasIntentsPost).toBe(false);
  });

  it('resolution: uses channel truth for stream start', async () => {
    const mockChannel = {
      id: '1:0:1:ABCD',
      serviceRef: '1:0:1:ABCD',
      name: 'Test Channel'
    };

    render(<V3Player autoStart={true} channel={mockChannel} />);

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/intents'),
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('1:0:1:ABCD')
        })
      );
    });
  });
});
