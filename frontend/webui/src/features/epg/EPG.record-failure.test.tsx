import { render, waitFor, cleanup } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setClientAuthToken } from '../../services/clientWrapper';
import type { EpgEvent, Timer } from './types';

const {
  fetchEpgEvents,
  fetchTimers,
  addTimer,
  confirm,
  toast,
  capturedOnRecord,
} = vi.hoisted(() => ({
  fetchEpgEvents: vi.fn<(...args: any[]) => Promise<EpgEvent[]>>(),
  fetchTimers: vi.fn<() => Promise<Timer[]>>(),
  addTimer: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  capturedOnRecord: {
    current: undefined as undefined | ((event: EpgEvent) => void | Promise<void>),
  },
}));

vi.mock('./epgApi', () => ({ fetchEpgEvents, fetchTimers }));
vi.mock('../../client-ts', () => ({ addTimer }));
vi.mock('../../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({ confirm, toast }),
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
  EpgToolbar: () => <div data-testid="epg-toolbar" />,
}));
vi.mock('./components/EpgChannelList', () => ({
  EpgChannelList: (props: { onRecord?: (event: EpgEvent) => void | Promise<void> }) => {
    if (props.onRecord) {
      capturedOnRecord.current = props.onRecord;
    }
    return <div data-testid="epg-channel-list" />;
  },
}));
vi.mock('../../components/Timers', () => ({ __esModule: true, default: () => <div /> }));

import EPG from './EPG';

function renderWithProviders(ui: ReactNode) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('EPG record button failure handling (SDK resolves { error } instead of throwing)', () => {
  beforeEach(() => {
    setClientAuthToken('');
    capturedOnRecord.current = undefined;
    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    setClientAuthToken('');
  });

  it('shows an error toast (not a success toast) when the backend rejects the timer', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockResolvedValue([]);
    confirm.mockResolvedValue(true);
    addTimer.mockResolvedValue({
      data: undefined,
      error: {
        type: 'about:blank',
        title: 'Conflict',
        status: 409,
        requestId: 'req-conflict',
        code: 'TIMER_CONFLICT',
        detail: 'Timer overlaps an existing recording',
      },
      response: { status: 409 },
    });

    renderWithProviders(<EPG channels={[]} />);

    // EPG mounts the channel list once the (mocked) EPG load resolves; capture its onRecord.
    await waitFor(() => {
      expect(capturedOnRecord.current).toBeTypeOf('function');
    });

    await capturedOnRecord.current!({
      serviceRef: '1:0:1:abc',
      start: 1700000000,
      end: 1700003600,
      title: 'Tagesschau',
      desc: 'News',
    } as EpgEvent);

    await waitFor(() => {
      expect(addTimer).toHaveBeenCalledOnce();
    });

    // The rejected create must NOT surface as success.
    expect(toast).not.toHaveBeenCalledWith(expect.objectContaining({ kind: 'success' }));
    expect(toast).toHaveBeenCalledWith(expect.objectContaining({ kind: 'error' }));
  });
});
