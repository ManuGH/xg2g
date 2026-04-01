import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../context/HouseholdProfilesContext';
import { PendingChangesProvider } from '../context/PendingChangesContext';
import { setClientAuthToken } from '../services/clientWrapper';

const {
  getSystemConfig,
  getSystemScanStatus,
  triggerSystemScan,
  confirm,
  toast,
  loadChannels,
} = vi.hoisted(() => ({
  getSystemConfig: vi.fn(),
  getSystemScanStatus: vi.fn(),
  triggerSystemScan: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  loadChannels: vi.fn(),
}));

vi.mock('../client-ts', () => ({
  getSystemConfig,
  getSystemScanStatus,
  triggerSystemScan,
}));

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    channels: {
      bouquets: [],
      selectedBouquet: '',
      channels: [],
      loading: false,
    },
    loadChannels,
  }),
}));

vi.mock('../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
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
      <PendingChangesProvider>
        <HouseholdProfilesProvider>
          <Settings />
        </HouseholdProfilesProvider>
      </PendingChangesProvider>
    </QueryClientProvider>
  );
}

describe('Settings', () => {
  afterEach(() => {
    vi.clearAllMocks();
    setClientAuthToken('');
    window.localStorage.clear();
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

    expect(await screen.findByDisplayValue('Universal (H.264/AAC/fMP4)')).toBeInTheDocument();
    expect(screen.getByText('Idle')).toBeInTheDocument();
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

    fireEvent.click(await screen.findByRole('button', { name: 'Start Scan' }));

    await waitFor(() => {
      expect(triggerSystemScan).toHaveBeenCalledTimes(1);
    });

    await waitFor(() => {
      expect(getSystemScanStatus.mock.calls.length).toBeGreaterThanOrEqual(2);
      expect(screen.getByRole('button', { name: 'Running...' })).toBeDisabled();
    });
  });

  it('confirms before discarding a dirty profile draft', async () => {
    getSystemConfig.mockResolvedValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        streaming: { deliveryPolicy: 'universal' },
      },
    });
    getSystemScanStatus.mockResolvedValue({
      data: {
        state: 'idle',
        scannedChannels: 0,
        totalChannels: 20,
        updatedCount: 0,
      },
    });
    confirm.mockResolvedValue(false);

    renderWithQueryClient();

    fireEvent.change(await screen.findByDisplayValue('Haushalt'), {
      target: { value: 'Wohnzimmer' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Kinderprofil' }));

    await waitFor(() => {
      expect(confirm).toHaveBeenCalledTimes(1);
    });

    expect(screen.getByDisplayValue('Wohnzimmer')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 4, name: 'Haushalt' })).toBeInTheDocument();
  });
});
