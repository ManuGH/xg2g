import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const { getSystemInfo } = vi.hoisted(() => ({
  getSystemInfo: vi.fn(),
}));

vi.mock('../../client-ts', () => ({
  getSystemInfo,
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

import { SystemInfo } from './SystemInfo';

function renderWithQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SystemInfo />
    </QueryClientProvider>
  );
}

describe('SystemInfo', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('loads system information through the shared query hook', async () => {
    getSystemInfo.mockResolvedValue({
      data: {
        hardware: {
          brand: 'Dreambox',
          model: 'One',
          chipsetDescription: 'BCM7252S',
        },
        software: {
          imageDistro: 'OpenATV',
          imageVersion: '7.5',
          kernelVersion: '5.15.0',
          webifVersion: '2.0',
        },
        tuners: [
          { name: 'Tuner A', type: 'DVB-S2', status: 'idle' },
        ],
        network: {
          interfaces: [
            { name: 'eth0', type: 'ethernet', speed: '1 Gbit/s', ip: '192.168.1.10', ipv6: '', dhcp: true },
          ],
        },
        storage: {
          devices: [],
          locations: [],
        },
        runtime: {
          uptime: '1 day',
        },
        resource: {
          memoryUsed: '1024 MB',
          memoryAvailable: '1024 MB',
          memoryTotal: '2048 MB',
        },
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Dreambox One')).toBeInTheDocument();
    expect(screen.getByText('OpenATV')).toBeInTheDocument();
    expect(screen.getByText('192.168.1.10')).toBeInTheDocument();
    expect(screen.getByText('1 day')).toBeInTheDocument();
  });
});
