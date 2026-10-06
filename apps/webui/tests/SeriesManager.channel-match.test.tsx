// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import SeriesManager from '../src/components/SeriesManager';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';
import { UiOverlayProvider } from '../src/context/UiOverlayContext';

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSeriesRules: vi.fn(),
    getServices: vi.fn(),
    createSeriesRule: vi.fn(),
    updateSeriesRule: vi.fn(),
  };
});

describe('SeriesManager channel ref matching and fallback (Item 2)', () => {
  const openWebifRuleRefWithColon = '1:0:19:AAAA:BBB:1:C00000:0:0:0:';
  const backendChannelRefTrimmed = '1:0:19:AAAA:BBB:1:C00000:0:0:0';

  const mockChannels: client.Service[] = [
    {
      id: openWebifRuleRefWithColon,
      name: 'Channel One',
      serviceRef: backendChannelRefTrimmed,
      logoUrl: 'https://example.invalid/channel-one.png',
    },
    {
      id: '1:0:19:CCCC:DDD:1:C00000:0:0:0',
      name: 'Channel Two',
      serviceRef: '1:0:19:CCCC:DDD:1:C00000:0:0:0',
      logoUrl: 'https://example.invalid/channel-two.png',
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('matches a trailing-colon rule reference and renders channel name and logo', async () => {
    (client.getServices as any).mockResolvedValue({ data: mockChannels });
    (client.getSeriesRules as any).mockResolvedValue({
      data: [
        {
          id: 'rule-1',
          keyword: 'Example Show',
          channelRef: openWebifRuleRefWithColon,
          enabled: true,
        },
      ],
    });

    const { container } = render(
      <MemoryRouter>
        <UiOverlayProvider>
          <SeriesManager showLegacyNotice={false} />
        </UiOverlayProvider>
      </MemoryRouter>
    );

    expect(await screen.findByText('Channel One')).toBeInTheDocument();

    // Raw ref must NOT be displayed
    expect(screen.queryByText(openWebifRuleRefWithColon)).toBeNull();
    expect(screen.queryByText(backendChannelRefTrimmed)).toBeNull();

    // Logo should be rendered
    const logo = container.querySelector('img');
    expect(logo).toHaveAttribute('src', 'https://example.invalid/channel-one.png');
  });

  it('preselects the matching serviceRef in the edit dialog for a trailing-colon rule id', async () => {
    (client.getServices as any).mockResolvedValue({ data: mockChannels });
    (client.getSeriesRules as any).mockResolvedValue({
      data: [{
        id: 'rule-1',
        keyword: 'Example Show',
        channelRef: openWebifRuleRefWithColon,
        enabled: true,
      }],
    });

    render(
      <MemoryRouter>
        <UiOverlayProvider>
          <SeriesManager showLegacyNotice={false} />
        </UiOverlayProvider>
      </MemoryRouter>
    );

    expect(await screen.findByText('Channel One')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /edit|bearbeiten/i }));

    expect(screen.getByTestId('series-edit-channel')).toHaveValue(backendChannelRefTrimmed);
  });

  it('renders localized unknown channel fallback and never renders raw ref when channel is not in list', async () => {
    const unknownRef = '1:0:19:9999:999:1:C00000:0:0:0:';
    (client.getServices as any).mockResolvedValue({ data: mockChannels });
    (client.getSeriesRules as any).mockResolvedValue({
      data: [
        {
          id: 'rule-unknown',
          keyword: 'Mystery Show',
          channelRef: unknownRef,
          enabled: true,
        },
      ],
    });

    render(
      <MemoryRouter>
        <UiOverlayProvider>
          <SeriesManager showLegacyNotice={false} />
        </UiOverlayProvider>
      </MemoryRouter>
    );

    // Fallback text is shown
    expect(await screen.findByText(/Unbekannter Sender|Unknown Channel/i)).toBeInTheDocument();

    // Raw ref must NEVER leak to the user
    expect(screen.queryByText(unknownRef)).toBeNull();
  });

});
