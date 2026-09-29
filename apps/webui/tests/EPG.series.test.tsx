import { render, waitFor, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { EpgEvent } from '../src/features/epg/types';

const {
  fetchEpgEvents,
  fetchTimers,
  addTimer,
  createSeriesRule,
  runSeriesRule,
  confirm,
  toast,
  capturedOnScheduleSeries,
} = vi.hoisted(() => ({
  fetchEpgEvents: vi.fn(),
  fetchTimers: vi.fn(),
  addTimer: vi.fn(),
  createSeriesRule: vi.fn(),
  runSeriesRule: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  capturedOnScheduleSeries: {
    current: undefined as undefined | ((event: EpgEvent, config: any) => Promise<void> | void),
  },
}));

vi.mock('../src/features/epg/epgApi', () => ({
  fetchEpgEvents,
  fetchTimers,
}));

vi.mock('../src/client-ts', () => ({
  addTimer,
  createSeriesRule,
  runSeriesRule,
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({ confirm, toast }),
}));

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => ({
    selectedProfile: { id: 'p1', name: 'Main', favoriteServiceRefs: [] },
    isReady: true,
    isFavoriteService: () => false,
    toggleFavoriteService: vi.fn(),
    canManageDvr: true,
  }),
}));

vi.mock('../src/features/epg/components/EpgToolbar', () => ({
  EpgToolbar: () => <div data-testid="epg-toolbar" />,
}));

vi.mock('../src/features/epg/components/EpgChannelList', () => ({
  EpgChannelList: (props: { onEventClick?: (evt: EpgEvent) => void }) => (
    <div
      data-testid="epg-channel-list"
      onClick={() => {
        if (props.onEventClick) {
          props.onEventClick({
            id: 12345,
            title: 'Café PULS mit PULS 4 Aktuell',
            desc: 'Morgenmagazin',
            start: 1727586000,
            end: 1727598600,
            serviceRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
          });
        }
      }}
    />
  ),
}));

vi.mock('../src/features/epg/components/EpgEventDialog', () => ({
  EpgEventDialog: (props: {
    event: EpgEvent;
    onClose: () => void;
    onScheduleSeries?: (event: EpgEvent, config: any) => Promise<void> | void;
  }) => {
    capturedOnScheduleSeries.current = props.onScheduleSeries;
    return (
      <div data-testid="epg-dialog">
        <button
          data-testid="trigger-schedule-series"
          onClick={() => {
            if (props.onScheduleSeries) {
              props.onScheduleSeries(props.event, {
                keyword: 'Café PULS',
                channelRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
                retentionDays: 7,
              });
            }
          }}
        >
          Mock Schedule Series
        </button>
      </div>
    );
  },
}));

vi.mock('../src/components/Timers', () => ({ __esModule: true, default: () => <div /> }));

import EPG from '../src/features/epg/EPG';

describe('EPG Series Scheduling integration', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    fetchEpgEvents.mockResolvedValue([]);
    fetchTimers.mockResolvedValue([]);
    createSeriesRule.mockResolvedValue({
      data: {
        id: 'rule-cp-1',
        keyword: 'Café PULS',
        retentionDays: 7,
      },
    });
    runSeriesRule.mockResolvedValue({
      data: {
        timersCreated: 1,
        moviesDeleted: 0,
      },
    });
  });

  it('creates series rule, triggers run immediately, and reloads timers', async () => {
    render(
      <MemoryRouter>
        <EPG
          channels={[
            {
              id: '1:0:19:14B8:407:1:C00000:0:0:0:',
              name: 'PULS 24 HD',
              serviceRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
            },
          ]}
        />
      </MemoryRouter>
    );

    // Wait for EPG load to finish and channel list to appear
    const channelList = await screen.findByTestId('epg-channel-list');
    fireEvent.click(channelList);

    // Verify dialog rendered
    expect(await screen.findByTestId('epg-dialog')).toBeInTheDocument();

    // Trigger schedule series
    fireEvent.click(screen.getByTestId('trigger-schedule-series'));

    await waitFor(() => {
      expect(createSeriesRule).toHaveBeenCalledTimes(1);
    });

    expect(createSeriesRule).toHaveBeenCalledWith({
      body: {
        keyword: 'Café PULS',
        channelRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
        retentionDays: 7,
        enabled: true,
        priority: 0,
      },
    });

    await waitFor(() => {
      expect(runSeriesRule).toHaveBeenCalledWith({
        path: { id: 'rule-cp-1' },
        query: { trigger: 'manual' },
      });
    });

    expect(toast).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'success',
      })
    );

    // fetchTimers is called once on mount and once after series scheduling
    expect(fetchTimers).toHaveBeenCalledTimes(2);
  });
});
