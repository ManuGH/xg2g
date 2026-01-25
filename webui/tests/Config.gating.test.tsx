import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import Config from '../src/components/Config';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';

// Mock the API client
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSystemConfig: vi.fn(),
  };
});

describe('Config Component Gating Invariant (UI-INV-001)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders wizard view when unconfigured (UI-INV-001)', async () => {
    const unconfiguredConfig = {
      openWebIF: { baseUrl: '', username: '', password: '', streamPort: 8001 },
      epg: { enabled: false, days: 14, source: 'per-service' },
      bouquets: [],
      featureFlags: {}
    };

    (client.getSystemConfig as any).mockResolvedValue({ data: unconfiguredConfig });

    render(<Config />);

    await waitFor(() => {
      expect(screen.getByTestId('config-wizard')).toBeInTheDocument();
      expect(screen.queryByTestId('config-settings')).toBeNull();
    });
  });

  it('renders settings view when configured (UI-INV-001)', async () => {
    const configuredConfig = {
      openWebIF: { baseUrl: 'http://127.0.0.1', username: '', password: '', streamPort: 8001 },
      epg: { enabled: true, days: 14, source: 'per-service' },
      bouquets: ['Favorites'],
      featureFlags: {}
    };

    (client.getSystemConfig as any).mockResolvedValue({ data: configuredConfig });

    render(<Config />);

    await waitFor(() => {
      expect(screen.getByTestId('config-settings')).toBeInTheDocument();
      expect(screen.queryByTestId('config-wizard')).toBeNull();
    });
  });
});
