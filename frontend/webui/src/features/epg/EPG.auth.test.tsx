import { render, screen, waitFor, cleanup } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
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

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
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

describe('EPG auth handling', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('dispatches auth-required when the initial EPG load returns 401', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(Object.assign(new Error('Unauthorized'), { status: 401 }));

    const dispatchSpy = vi.spyOn(window, 'dispatchEvent');

    render(<EPG channels={[]} />);

    await waitFor(() => {
      expect(dispatchSpy).toHaveBeenCalled();
    });

    expect(dispatchSpy.mock.calls.some(([event]) => event.type === 'auth-required')).toBe(true);
    dispatchSpy.mockRestore();
  });

  it('shows a forbidden error when the EPG endpoint returns 403', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(Object.assign(new Error('Forbidden'), { status: 403 }));

    const dispatchSpy = vi.spyOn(window, 'dispatchEvent');

    render(<EPG channels={[]} />);

    await waitFor(() => {
      expect(screen.getByText('player.forbidden')).toBeInTheDocument();
    });

    expect(dispatchSpy.mock.calls.some(([event]) => event.type === 'auth-required')).toBe(false);
    dispatchSpy.mockRestore();
  });
});
