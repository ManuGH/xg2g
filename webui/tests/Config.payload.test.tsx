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

describe('Config Component Payload Hardening', () => {
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

  it('UI-INV-002: preserves technical fields (epg) from backend during save', async () => {
    // 1. Setup mock data with non-default values
    const mockConfig = {
      openWebIF: { baseUrl: 'http://127.0.0.1:80', username: '', password: '', streamPort: 9999 },
      epg: { enabled: true, days: 7, source: 'per-service' },
      bouquets: ['Favorites'],
      featureFlags: {}
    };

    (client.getSystemConfig as any).mockResolvedValue({ data: mockConfig });
    (client.putSystemConfig as any).mockResolvedValue({ data: { success: true, restartRequired: false } });

    // Mock the validate fetch call
    (global.fetch as any).mockResolvedValue({
      ok: true,
      json: async () => ({ valid: true, bouquets: ['Favorites'], version: { info: { brand: 'Test', model: 'Box' } } })
    });

    render(<Config />);

    // 2. Wait for loading (Config component renders wizard or settings based on config)
    await waitFor(() => {
      expect(screen.getByTestId('config-settings')).toBeInTheDocument();
    });

    // 3. Trigger validation using stable selector
    const validateBtn = screen.getByTestId('config-validate');
    fireEvent.click(validateBtn);

    // Wait for validation success: save button should become enabled
    await waitFor(() => {
      expect(screen.getByTestId('config-save')).not.toBeDisabled();
    });

    // 4. Click Save using stable selector
    const saveBtn = screen.getByTestId('config-save');
    expect(saveBtn).not.toBeDisabled();
    fireEvent.click(saveBtn);

    // 5. Assert: The payload must be a pure function of backend state + user edits
    await waitFor(() => {
      expect(client.putSystemConfig).toHaveBeenCalledOnce();
      expect(client.putSystemConfig).toHaveBeenCalledWith(expect.objectContaining({
        body: expect.objectContaining({
          openWebIF: expect.objectContaining({
            streamPort: 9999
          }),
          epg: {
            enabled: true,
            days: 7,
            source: 'per-service'
          }
        })
      }));
    });
  });

  it('UI-INV-002: omits streamPort from payload if missing from backend (no UI defaulting)', async () => {
    // 1. Setup mock data with missing streamPort
    const mockConfig = {
      openWebIF: { baseUrl: 'http://127.0.0.1:80', username: '', password: '' },
      epg: { enabled: true, days: 7, source: 'per-service' },
      bouquets: ['Favorites'],
      featureFlags: {}
    };

    (client.getSystemConfig as any).mockResolvedValue({ data: mockConfig });
    (client.putSystemConfig as any).mockResolvedValue({ data: { success: true, restartRequired: false } });

    (global.fetch as any).mockResolvedValue({
      ok: true,
      json: async () => ({ valid: true, bouquets: ['Favorites'], version: { info: { brand: 'Test', model: 'Box' } } })
    });

    render(<Config />);

    await waitFor(() => {
      expect(screen.getByTestId('config-settings')).toBeInTheDocument();
    });

    const validateBtn = screen.getByTestId('config-validate');
    fireEvent.click(validateBtn);

    await waitFor(() => {
      expect(screen.getByTestId('config-save')).not.toBeDisabled();
    });

    fireEvent.click(screen.getByTestId('config-save'));

    await waitFor(() => {
      expect(client.putSystemConfig).toHaveBeenCalledOnce();
      const call = (client.putSystemConfig as any).mock.calls[0][0];
      // Assert streamPort is NOT in the payload
      expect(call.body.openWebIF).not.toHaveProperty('streamPort');
    });
  });
});
