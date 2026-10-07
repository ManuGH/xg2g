import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const { getSystemInfo } = vi.hoisted(() => ({
  getSystemInfo: vi.fn(),
}));

vi.mock('../../client-ts', () => ({
  getSystemInfo,
}));


import { SystemInfo, formatHardwareModel, formatUptime, formatStorageCapacity } from './SystemInfo';

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
    expect(screen.getByText(/192\.168\.1\.10/)).toBeInTheDocument();
    expect(screen.getByText('1 day')).toBeInTheDocument();
  });

  it('renders receiver and xg2g storage classes distinctly', async () => {
    getSystemInfo.mockResolvedValue({
      data: {
        hardware: {
          brand: 'Dreambox',
          model: 'One',
        },
        software: {},
        tuners: [],
        network: {
          interfaces: [],
        },
        storage: {
          devices: [
            {
              model: 'Samsung SSD',
              mount: '/media/hdd',
              mountStatus: 'mounted',
              healthStatus: 'ok',
              access: 'rw',
              isNas: false,
              origin: 'receiver',
              pathType: 'receiver_attached',
              fsType: 'ext4',
            },
          ],
          locations: [
            {
              mount: '/mnt/storage/media',
              mountStatus: 'mounted',
              healthStatus: 'ok',
              access: 'rw',
              isNas: false,
              origin: 'xg2g',
              pathType: 'xg2g_aggregate',
              fsType: 'fuse.mergerfs',
            },
          ],
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

    expect(await screen.findByText(/Samsung SSD/)).toBeInTheDocument();
    expect(screen.getByText('Receiver storage')).toBeInTheDocument();
    expect(screen.getByText('xg2g')).toBeInTheDocument();
    expect(screen.getByText('xg2g aggregate')).toBeInTheDocument();
  });

  it('cleanly separates tuner number from tuner model and localizes tuner label', async () => {
    getSystemInfo.mockResolvedValue({
      data: {
        hardware: { brand: 'Vu+', model: 'Uno 4K' },
        software: {},
        tuners: [
          { name: 'Tuner A', type: 'DVB-S NIM(45208 FBC)', status: 'live' },
        ],
        network: { interfaces: [] },
        storage: { devices: [], locations: [] },
        runtime: {},
        resource: {},
      },
    });

    renderWithQueryClient();

    const tunerNumber = await screen.findByText('Tuner #1');
    expect(tunerNumber).toBeInTheDocument();
    expect(tunerNumber.className).toContain('tunerNumber');

    const tunerType = screen.getByText('S NIM(45208 FBC)');
    expect(tunerType).toBeInTheDocument();
    expect(tunerType.className).toContain('tunerTypeLabel');
  });

  it('normalizes hardware model strings cleanly', () => {
    expect(formatHardwareModel('Vu+', 'Uno4K')).toBe('VU+ Uno 4K');
    expect(formatHardwareModel('Vu+', 'Uno4Kse')).toBe('VU+ Uno 4K SE');
    expect(formatHardwareModel('Vu+', 'Duo4Kse')).toBe('VU+ Duo 4K SE');
    expect(formatHardwareModel('Dreambox', 'One')).toBe('Dreambox One');
  });

  it('formats short and long uptimes to human readable strings', () => {
    const dummyT = ((key: string) => {
      if (key === 'system.minutes') return 'Min.';
      if (key === 'system.hours') return 'Std.';
      if (key === 'system.days') return 'Tage';
      return key;
    }) as any;

    expect(formatUptime('00:46', dummyT)).toBe('46 Min.');
    expect(formatUptime('03:18', dummyT)).toBe('3 Std. 18 Min.');
    expect(formatUptime('4d 07:12', dummyT)).toBe('4 Tage 7 Std.');
    expect(formatUptime('1 day', dummyT)).toBe('1 day');
  });

  it('simplifies and localizes storage capacity strings', () => {
    const dummyTDe = ((key: string, opts?: any) => {
      if (key === 'system.freeOf') return `${opts.free} frei von ${opts.total}`;
      return key;
    }) as any;

    expect(formatStorageCapacity('25.7 GB frei / 28.6 GB (31 GB) insgesamt', 'de', dummyTDe))
      .toBe('25,7 GB frei von 28,6 GB');
  });
});

