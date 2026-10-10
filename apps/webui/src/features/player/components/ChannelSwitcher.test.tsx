import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Service } from '../../../client-ts';
import { ChannelSwitcher } from './ChannelSwitcher';

const mockPostServicesNowNext = vi.fn();

vi.mock('../../../client-ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../../client-ts')>();
  return {
    ...actual,
    postServicesNowNext: (args: unknown) => mockPostServicesNowNext(args),
  };
});

const mockChannels: Service[] = [
  {
    id: 'ch-1',
    serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
    name: 'ORF 1 HD',
    number: '1',
  },
  {
    id: 'ch-2',
    serviceRef: '1:0:19:1334:3EF:1:C00000:0:0:0',
    name: 'ORF 2 HD',
    number: '2',
  },
  {
    id: 'ch-3',
    serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0',
    name: 'Das Erste HD',
    number: '3',
  },
];

describe('ChannelSwitcher', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders channels and fetches now/next programme titles when open', async () => {
    mockPostServicesNowNext.mockResolvedValueOnce({
      data: {
        items: [
          {
            serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
            now: { title: 'Tennis ATP 100 Tulln', desc: 'Live tennis' },
          },
          {
            serviceRef: '1:0:19:1334:3EF:1:C00000:0:0:0',
            now: { title: 'Bundesland heute', desc: 'Regional news' },
          },
        ],
      },
    });

    const onSwitch = vi.fn();
    const onClose = vi.fn();

    render(
      <ChannelSwitcher
        channels={mockChannels}
        current={mockChannels[0]}
        onSwitch={onSwitch}
        open={true}
        onClose={onClose}
        token="test-token"
      />
    );

    expect(screen.getByText('ORF 1 HD')).toBeInTheDocument();
    expect(screen.getByText('ORF 2 HD')).toBeInTheDocument();
    expect(screen.getByText('Das Erste HD')).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText('Tennis ATP 100 Tulln')).toBeInTheDocument();
      expect(screen.getByText('Bundesland heute')).toBeInTheDocument();
    });

    // Clicking channel 2 invokes onSwitch and onClose
    fireEvent.click(screen.getByText('ORF 2 HD'));
    expect(onSwitch).toHaveBeenCalledWith(mockChannels[1]);
    expect(onClose).toHaveBeenCalled();
  });

  it('filters channels by current programme title', async () => {
    mockPostServicesNowNext.mockResolvedValueOnce({
      data: {
        items: [
          {
            serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
            now: { title: 'Tennis ATP 100 Tulln', desc: 'Live tennis' },
          },
        ],
      },
    });

    render(
      <ChannelSwitcher
        channels={mockChannels}
        current={mockChannels[0]}
        onSwitch={vi.fn()}
        open={true}
        onClose={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByText('Tennis ATP 100 Tulln')).toBeInTheDocument();
    });

    const searchInput = screen.getByRole('textbox');
    fireEvent.change(searchInput, { target: { value: 'tennis' } });

    expect(screen.getByText('ORF 1 HD')).toBeInTheDocument();
    expect(screen.queryByText('ORF 2 HD')).not.toBeInTheDocument();
    expect(screen.queryByText('Das Erste HD')).not.toBeInTheDocument();
  });

  it('switches IPTV channels cleanly even when serviceRef is empty string and id holds the opaque ref', async () => {
    mockPostServicesNowNext.mockResolvedValueOnce({ data: { items: [] } });

    const iptvChannels: Service[] = [
      {
        id: 'iptv_rtl_hd_111',
        serviceRef: '',
        name: 'DE: RTL HD',
        number: '101',
      },
      {
        id: 'iptv_pro7_hd_222',
        serviceRef: '',
        name: 'DE: ProSieben HD',
        number: '102',
      },
    ];

    const onSwitch = vi.fn();
    const onClose = vi.fn();

    render(
      <ChannelSwitcher
        channels={iptvChannels}
        current={iptvChannels[0]}
        onSwitch={onSwitch}
        open={true}
        onClose={onClose}
      />
    );

    await waitFor(() => {
      expect(mockPostServicesNowNext).toHaveBeenCalled();
    });

    // Only the current IPTV channel should be marked live, not all IPTV channels
    expect(screen.getAllByText('● live')).toHaveLength(1);

    // Clicking the second IPTV channel must call onSwitch
    fireEvent.click(screen.getByText('DE: ProSieben HD'));
    expect(onSwitch).toHaveBeenCalledWith(iptvChannels[1]);
    expect(onClose).toHaveBeenCalled();
  });

  it('renders bouquet tabs when multiple bouquets are provided and invokes onSelectBouquet', async () => {
    mockPostServicesNowNext.mockResolvedValueOnce({ data: { items: [] } });

    const onSelectBouquet = vi.fn();
    render(
      <ChannelSwitcher
        channels={mockChannels}
        current={mockChannels[0]}
        onSwitch={vi.fn()}
        open={true}
        onClose={vi.fn()}
        bouquets={[
          { name: 'Favourites (TV)', services: 50 },
          { name: 'IPTV - Top', services: 112 },
        ]}
        selectedBouquet="Favourites (TV)"
        onSelectBouquet={onSelectBouquet}
      />
    );

    await waitFor(() => {
      expect(mockPostServicesNowNext).toHaveBeenCalled();
    });

    const iptvTab = screen.getByRole('tab', { name: 'IPTV - Top' });
    fireEvent.click(iptvTab);
    expect(onSelectBouquet).toHaveBeenCalledWith('IPTV - Top');
  });

  it('disables channel rows and prevents switching while loading another bouquet until the new bouquet commits', async () => {
    mockPostServicesNowNext.mockResolvedValueOnce({ data: { items: [] } });

    const onSwitch = vi.fn();
    const onSelectBouquet = vi.fn();
    const { rerender } = render(
      <ChannelSwitcher
        channels={mockChannels}
        current={mockChannels[0]}
        onSwitch={onSwitch}
        open={true}
        onClose={vi.fn()}
        bouquets={[
          { name: 'Favourites (TV)', services: 50 },
          { name: 'IPTV - Top', services: 112 },
        ]}
        selectedBouquet="Favourites (TV)"
        onSelectBouquet={onSelectBouquet}
      />
    );

    await waitFor(() => {
      expect(mockPostServicesNowNext).toHaveBeenCalled();
    });

    const iptvTab = screen.getByRole('tab', { name: 'IPTV - Top' });
    fireEvent.click(iptvTab);
    expect(onSelectBouquet).toHaveBeenCalledWith('IPTV - Top');

    // While waiting for the new bouquet response, existing channel rows must be disabled
    const orf2Btn = screen.getByText('ORF 2 HD').closest('button')!;
    expect(orf2Btn).toBeDisabled();

    // Clicking a row during the pending transition must not invoke onSwitch
    fireEvent.click(orf2Btn);
    expect(onSwitch).not.toHaveBeenCalled();

    // When the new bouquet commits with new channels, rows are re-enabled
    const iptvChannels: Service[] = [
      { id: 'iptv-1', serviceRef: '1:0:1:IPTV:1:0:0:0:0:0', name: 'IPTV News HD', number: '1' },
    ];
    rerender(
      <ChannelSwitcher
        channels={iptvChannels}
        current={mockChannels[0]}
        onSwitch={onSwitch}
        open={true}
        onClose={vi.fn()}
        bouquets={[
          { name: 'Favourites (TV)', services: 50 },
          { name: 'IPTV - Top', services: 112 },
        ]}
        selectedBouquet="IPTV - Top"
        onSelectBouquet={onSelectBouquet}
      />
    );

    const newsBtn = screen.getByText('IPTV News HD').closest('button')!;
    expect(newsBtn).not.toBeDisabled();
    fireEvent.click(newsBtn);
    expect(onSwitch).toHaveBeenCalledWith(iptvChannels[0]);
  });
});
