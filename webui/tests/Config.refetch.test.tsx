import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import Config from '../src/components/Config';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as client from '../src/client-ts';

// Mock the API client
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSystemConfig: vi.fn(),
    putSystemConfig: vi.fn(),
    triggerSystemScan: vi.fn(),
    getSystemScanStatus: vi.fn(),
  };
});

describe('Config Component Re-fetch Invariant (UI-INV-003)', () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    global.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('re-fetches state from backend after successful save (UI-INV-003)', async () => {
    // 1. Initial Config A
    const configA = {
      openWebIF: { baseUrl: 'http://127.0.0.1:80', username: '', password: '', streamPort: 8001 },
      epg: { enabled: true, days: 7, source: 'per-service' },
      bouquets: ['Favorites'],
      featureFlags: {}
    };

    // 2. Updated Config B (normalized by backend)
    const configB = {
      ...configA,
      epg: { ...configA.epg, days: 14 } // Backend bumped it
    };

    // Mock sequence
    (client.getSystemConfig as any)
      .mockResolvedValueOnce({ data: configA }) // first load
      .mockResolvedValueOnce({ data: configB }); // second load (re-fetch)

    (client.putSystemConfig as any).mockResolvedValue({ data: { success: true, restartRequired: false } });

    // Mock validation
    (global.fetch as any).mockResolvedValue({
      ok: true,
      json: async () => ({ valid: true, bouquets: ['Favorites'], version: { info: { brand: 'Test', model: 'Box' } } })
    });

    render(<Config />);

    // Wait for first load
    await waitFor(() => {
      expect(screen.getByTestId('config-settings')).toBeInTheDocument();
    });

    // Validate: save button should become enabled
    fireEvent.click(screen.getByTestId('config-validate'));
    await waitFor(() => {
      expect(screen.getByTestId('config-save')).not.toBeDisabled();
    });

    // Save
    fireEvent.click(screen.getByTestId('config-save'));

    // Assert: UI-INV-003 re-fetch causality
    // 1. First ensure save was triggered
    await waitFor(() => {
      expect(client.putSystemConfig).toHaveBeenCalledOnce();
    });

    // 2. Then ensure the re-fetch occurred
    await waitFor(() => {
      expect(client.getSystemConfig).toHaveBeenCalledTimes(2);
    });

    // Optional: verify UI reflect Config B if there was a field to check
    // In our case, the component re-rendered with configB.
  });
});
