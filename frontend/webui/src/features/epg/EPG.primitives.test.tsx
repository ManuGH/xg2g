import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ClientRequestError } from '../../services/clientWrapper';
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

vi.mock('./components/EpgChannelList', () => ({
  EpgChannelList: () => <div data-testid="epg-channel-list" />,
}));

import EPG from './EPG';

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('EPG shared primitives', () => {
  beforeEach(() => {
    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it('renders a section skeleton while the initial EPG load is in progress', async () => {
    fetchTimers.mockResolvedValue([]);
    const deferred = createDeferred<EpgEvent[]>();
    fetchEpgEvents.mockImplementation(() => deferred.promise);

    render(<EPG channels={[]} />);

    expect(screen.getByRole('status', { name: /Loading EPG/i })).toHaveAttribute(
      'data-loading-variant',
      'section'
    );

    deferred.resolve([]);

    await waitFor(() => {
      expect(screen.queryByRole('status', { name: /Loading EPG/i })).not.toBeInTheDocument();
    });
  });

  it('renders a visible section skeleton while search is in progress', async () => {
    fetchTimers.mockResolvedValue([]);
    const deferred = createDeferred<EpgEvent[]>();
    fetchEpgEvents.mockResolvedValueOnce([]);
    fetchEpgEvents.mockImplementationOnce(() => deferred.promise);

    render(<EPG channels={[]} />);

    fireEvent.change(screen.getByPlaceholderText(/Search Services/i), {
      target: { value: 'news' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Search' }));

    expect(await screen.findByRole('status', { name: /Loading/i })).toHaveAttribute(
      'data-loading-variant',
      'section'
    );

    deferred.resolve([]);

    await waitFor(() => {
      expect(screen.queryByRole('status', { name: /Loading/i })).not.toBeInTheDocument();
    });
  });

  it('renders a retryable error panel for main EPG load failures', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(
      new ClientRequestError({
        status: 503,
        title: 'Service unavailable',
        detail: 'epg backend offline',
      })
    );

    render(<EPG channels={[]} />);

    expect(await screen.findByRole('heading', { name: 'Service unavailable' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await waitFor(() => {
      expect(fetchEpgEvents).toHaveBeenCalledTimes(2);
    });
  });
});
