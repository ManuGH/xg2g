import { render, screen, waitFor, cleanup } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../../context/HouseholdProfilesContext';
import { ClientRequestError, setClientAuthToken } from '../../services/clientWrapper';
import type { EpgEvent, Timer } from './types';

const {
  fetchEpgEvents,
  fetchTimers,
  addTimer,
  confirm,
  toast,
} = vi.hoisted(() => ({
  fetchEpgEvents: vi.fn<(...args: any[]) => Promise<EpgEvent[]>>(),
  fetchTimers: vi.fn<() => Promise<Timer[]>>(),
  addTimer: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('./epgApi', () => ({
  fetchEpgEvents,
  fetchTimers,
}));

vi.mock('../../client-ts', () => ({
  addTimer,
}));

vi.mock('../../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));

vi.mock('./components/EpgToolbar', () => ({
  EpgToolbar: () => <div data-testid="epg-toolbar" />,
}));

vi.mock('./components/EpgChannelList', () => ({
  EpgChannelList: () => <div data-testid="epg-channel-list" />,
}));

import EPG from './EPG';

function renderWithProviders(ui: ReactNode) {
  return render(
    <HouseholdProfilesProvider>
      {ui}
    </HouseholdProfilesProvider>
  );
}

describe('EPG auth handling', () => {
  beforeEach(() => {
    setClientAuthToken('');
    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  it('does not render a local error panel when the initial EPG load returns 401', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(
      new ClientRequestError({
        status: 401,
        title: 'Unauthorized',
        detail: 'Token expired',
      })
    );

    renderWithProviders(<EPG channels={[]} />);

    await waitFor(() => {
      expect(screen.getByRole('status', { name: /Loading EPG/i })).toBeInTheDocument();
    });

    expect(screen.queryByRole('heading', { name: 'EPG could not be loaded.' })).not.toBeInTheDocument();
  });

  it('shows a forbidden error when the EPG endpoint returns 403', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(
      new ClientRequestError({
        status: 403,
        title: 'Forbidden',
        detail: 'Missing scope',
      })
    );

    const authRequiredHandler = vi.fn();
    window.addEventListener('auth-required', authRequiredHandler);

    renderWithProviders(<EPG channels={[]} />);

    await waitFor(() => {
      expect(screen.getByText('Forbidden')).toBeInTheDocument();
    });

    expect(authRequiredHandler).not.toHaveBeenCalled();
    window.removeEventListener('auth-required', authRequiredHandler);
  });
});
