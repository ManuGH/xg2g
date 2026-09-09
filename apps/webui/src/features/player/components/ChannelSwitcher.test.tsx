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
});
