import { render, screen, waitFor, act, cleanup } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ClientRequestError, setClientAuthToken } from '../../services/clientWrapper';
import type { EpgEvent, Timer } from './types';

const {
  fetchEpgEvents,
  fetchTimers,
  addTimer,
  capturedOnRefresh,
} = vi.hoisted(() => ({
  fetchEpgEvents: vi.fn<(...args: any[]) => Promise<EpgEvent[]>>(),
  fetchTimers: vi.fn<() => Promise<Timer[]>>(),
  addTimer: vi.fn(),
  capturedOnRefresh: { current: undefined as undefined | (() => void | Promise<void>) },
}));

vi.mock('./epgApi', () => ({ fetchEpgEvents, fetchTimers }));
vi.mock('../../client-ts', () => ({ addTimer }));
vi.mock('../../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({ confirm: vi.fn(), toast: vi.fn() }),
}));
vi.mock('../../context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => ({
    selectedProfile: { id: 'p1', name: 'Main', favoriteServiceRefs: [] },
    isReady: true,
    isFavoriteService: () => false,
    toggleFavoriteService: vi.fn(),
    canManageDvr: true,
  }),
}));
vi.mock('./components/EpgToolbar', () => ({
  EpgToolbar: (props: { onRefresh?: () => void | Promise<void> }) => {
    if (props.onRefresh) capturedOnRefresh.current = props.onRefresh;
    return <div data-testid="epg-toolbar" />;
  },
}));
vi.mock('./components/EpgChannelList', () => ({
  EpgChannelList: () => <div data-testid="epg-channel-list" />,
}));
vi.mock('../../components/Timers', () => ({ __esModule: true, default: () => <div /> }));

import EPG from './EPG';

function renderWithProviders(ui: ReactNode) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('EPG abort race (aborted load must not render the error panel)', () => {
  beforeEach(() => {
    setClientAuthToken('');
    capturedOnRefresh.current = undefined;
    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    setClientAuthToken('');
  });

  it('does not dispatch LOAD_ERROR when an in-flight load is aborted by a newer one', async () => {
    fetchTimers.mockResolvedValue([]);

    // First load stays pending until we reject it; the second (refresh) load resolves.
    let rejectFirst: (err: unknown) => void = () => {};
    const firstPending = new Promise<EpgEvent[]>((_resolve, reject) => {
      rejectFirst = reject;
    });
    fetchEpgEvents.mockReturnValueOnce(firstPending).mockResolvedValue([]);

    renderWithProviders(<EPG channels={[]} />);

    // Capture the toolbar's onRefresh (= loadEpgEvents) to trigger the second load.
    await waitFor(() => expect(capturedOnRefresh.current).toBeTypeOf('function'));

    // Second load: aborts the first in-flight request, then resolves successfully → LOAD_SUCCESS.
    await act(async () => {
      await capturedOnRefresh.current!();
      await Promise.resolve();
    });

    // The channel list is mounted because the second load reached the 'ready' state.
    await waitFor(() => expect(screen.getByTestId('epg-channel-list')).toBeInTheDocument());

    // Now the aborted first request rejects. The SDK wraps the AbortError into a
    // ClientRequestError (name !== 'AbortError'), so the old isAbortError(err) guard misses
    // it. Without the signal.aborted check this dispatches LOAD_ERROR, unmounting the list
    // and showing the error panel. With the fix it bails.
    await act(async () => {
      rejectFirst(new ClientRequestError({ status: undefined, title: 'signal is aborted without reason' }));
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(screen.getByTestId('epg-channel-list')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'EPG could not be loaded.' })).not.toBeInTheDocument();
  });
});
