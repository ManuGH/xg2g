import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../context/HouseholdProfilesContext';
import { setClientAuthToken } from '../services/clientWrapper';

const {
  getTimers,
  getDvrCapabilities,
  deleteTimer,
  confirm,
  toast,
  editTimerDialog,
} = vi.hoisted(() => ({
  getTimers: vi.fn(),
  getDvrCapabilities: vi.fn(),
  deleteTimer: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  editTimerDialog: vi.fn(({ timer }: { timer?: { timerId?: string } }) => (
    <div data-testid={timer ? 'timer-edit-dialog' : 'timer-create-dialog'} />
  )),
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

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    channels: {
      channels: [
        { serviceRef: '1:0:1:bbc', id: '1:0:1:bbc', name: 'BBC One' },
        { serviceRef: '1:0:1:ard', id: '1:0:1:ard', name: 'Das Erste' },
      ],
    },
  }),
}));

vi.mock('./EditTimerDialog', () => ({
  default: editTimerDialog,
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
      <HouseholdProfilesProvider>
        <Timers />
      </HouseholdProfilesProvider>
    </QueryClientProvider>
  );
}

describe('Timers', () => {
  afterEach(() => {
    vi.clearAllMocks();
    setClientAuthToken('');
    window.localStorage.clear();
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

  it('opens the timer dialog in create mode from the toolbar', async () => {
    getTimers.mockResolvedValue({ data: { items: [] } });
    getDvrCapabilities.mockResolvedValue({ data: null });

    renderWithQueryClient();

    fireEvent.click(await screen.findByRole('button', { name: 'New Timer' }));

    expect(await screen.findByTestId('timer-create-dialog')).toBeInTheDocument();
    expect(editTimerDialog).toHaveBeenCalledWith(
      expect.objectContaining({
        availableServices: expect.arrayContaining([
          expect.objectContaining({ name: 'BBC One' }),
          expect.objectContaining({ name: 'Das Erste' }),
        ]),
        onClose: expect.any(Function),
        onSave: expect.any(Function),
      }),
      undefined,
    );
  });
});
