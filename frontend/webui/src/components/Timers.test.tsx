import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const {
  getTimers,
  getDvrCapabilities,
  deleteTimer,
  confirm,
  toast,
} = vi.hoisted(() => ({
  getTimers: vi.fn(),
  getDvrCapabilities: vi.fn(),
  deleteTimer: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../client-ts', () => ({
  getTimers,
  getDvrCapabilities,
  deleteTimer,
}));

vi.mock('../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));

vi.mock('./EditTimerDialog', () => ({
  default: () => null,
}));

import Timers from './Timers';

function renderWithQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <Timers />
    </QueryClientProvider>
  );
}

describe('Timers', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('loads timers and refetches them on refresh', async () => {
    getTimers.mockResolvedValue({
      data: {
        items: [
          {
            timerId: 'timer-1',
            name: 'Evening News',
            serviceName: 'BBC',
            begin: 1710000000,
            end: 1710003600,
            state: 'scheduled',
          },
        ],
      },
    });
    getDvrCapabilities.mockResolvedValue({ data: null });

    renderWithQueryClient();

    expect(await screen.findByText('Evening News')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    await waitFor(() => {
      expect(getTimers).toHaveBeenCalledTimes(2);
    });
  });

  it('deletes a timer and refreshes the timer list', async () => {
    getTimers
      .mockResolvedValueOnce({
        data: {
          items: [
            {
              timerId: 'timer-1',
              name: 'Evening News',
              serviceName: 'BBC',
              begin: 1710000000,
              end: 1710003600,
              state: 'scheduled',
            },
          ],
        },
      })
      .mockResolvedValueOnce({
        data: {
          items: [],
        },
      });
    getDvrCapabilities.mockResolvedValue({ data: null });
    deleteTimer.mockResolvedValue({ data: undefined });
    confirm.mockResolvedValue(true);

    renderWithQueryClient();

    expect(await screen.findByText('Evening News')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

    await waitFor(() => {
      expect(deleteTimer).toHaveBeenCalledWith({ path: { timerId: 'timer-1' } });
    });

    await waitFor(() => {
      expect(screen.queryByText('Evening News')).not.toBeInTheDocument();
    });

    expect(getTimers).toHaveBeenCalledTimes(2);
    expect(toast).not.toHaveBeenCalled();
  });
});
