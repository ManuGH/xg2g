import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const {
  getSystemConfig,
  getSystemScanStatus,
  triggerSystemScan,
} = vi.hoisted(() => ({
  getSystemConfig: vi.fn(),
  getSystemScanStatus: vi.fn(),
  triggerSystemScan: vi.fn(),
}));

vi.mock('../client-ts', () => ({
  getSystemConfig,
  getSystemScanStatus,
  triggerSystemScan,
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

vi.mock('./Config', () => ({
  __esModule: true,
  default: () => <div data-testid="settings-config" />,
  isConfigured: (config: { openWebIF?: { baseUrl?: string } } | null) => Boolean(config?.openWebIF?.baseUrl),
}));

import Settings from './Settings';

function renderWithQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <Settings />
    </QueryClientProvider>
  );
}

describe('Settings', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('loads config and scan status from shared query hooks', async () => {
    getSystemConfig.mockResolvedValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        streaming: { deliveryPolicy: 'universal' },
      },
    });
    getSystemScanStatus.mockResolvedValue({
      data: {
        state: 'idle',
        scannedChannels: 10,
        totalChannels: 20,
        updatedCount: 2,
      },
    });

    renderWithQueryClient();

    expect(await screen.findByDisplayValue('settings.streaming.policy.universal')).toBeInTheDocument();
    expect(screen.getByText('settings.streaming.scan.status.idle')).toBeInTheDocument();
    expect(getSystemConfig).toHaveBeenCalledTimes(1);
    expect(getSystemScanStatus).toHaveBeenCalledTimes(1);
  });

  it('starts a scan and refetches scan status', async () => {
    let scanState: 'idle' | 'running' = 'idle';

    getSystemConfig.mockResolvedValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        streaming: { deliveryPolicy: 'universal' },
      },
    });
    getSystemScanStatus.mockImplementation(async () => ({
      data: {
        state: scanState,
        scannedChannels: scanState === 'running' ? 3 : 0,
        totalChannels: 20,
        updatedCount: 0,
      },
    }));
    triggerSystemScan.mockImplementation(async () => {
      scanState = 'running';
      return { data: { status: 'started' } };
    });

    renderWithQueryClient();

    fireEvent.click(await screen.findByRole('button', { name: 'settings.streaming.scan.start' }));

    await waitFor(() => {
      expect(triggerSystemScan).toHaveBeenCalledTimes(1);
    });

    await waitFor(() => {
      expect(getSystemScanStatus.mock.calls.length).toBeGreaterThanOrEqual(2);
      expect(screen.getByRole('button', { name: 'settings.streaming.scan.status.running' })).toBeDisabled();
    });
  });
});
