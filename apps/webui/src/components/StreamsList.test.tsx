import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const {
  confirm,
  toast,
  mutateAsync,
  mockUseStreams,
  mockUseStopStreamMutation,
} = vi.hoisted(() => ({
  confirm: vi.fn(),
  toast: vi.fn(),
  mutateAsync: vi.fn(),
  mockUseStreams: vi.fn(),
  mockUseStopStreamMutation: vi.fn(),
}));

vi.mock('../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));

vi.mock('../hooks/useServerQueries', () => ({
  useStreams: () => mockUseStreams(),
  useStopStreamMutation: () => mockUseStopStreamMutation(),
}));

import StreamsList from './StreamsList';

describe('StreamsList', () => {
  beforeEach(() => {
    confirm.mockResolvedValue(true);
    toast.mockReset();
    mutateAsync.mockResolvedValue({ requestId: 'stop-1' });
    mockUseStreams.mockReturnValue({
      data: [
        {
          sessionId: 'sid-live-1',
          requestId: 'req-live-1',
          channelName: 'Das Erste',
          clientFamily: 'ios_safari_native',
          preferredHlsEngine: 'native',
          deviceType: 'phone',
          clientIp: '192.168.0.54',
          startedAt: '2026-03-26T10:00:00.000Z',
          state: 'active',
          detailedState: 'active',
          program: {
            title: 'Tagesschau',
          },
        },
      ],
      error: null,
    });
    mockUseStopStreamMutation.mockReturnValue({
      mutateAsync,
      isPending: false,
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders operator-style session rows with primary session metadata', () => {
    render(<StreamsList compact />);

    screen.getByText('Das Erste');
    screen.getByText('Tagesschau');
    screen.getByText('Client');
    screen.getByText('iOS Safari');
    screen.getByText('Player');
    screen.getByText('Native HLS');
  });

  it('stops a stream after confirmation', async () => {
    render(<StreamsList compact />);

    fireEvent.click(screen.getByRole('button', { name: 'Stop' }));

    await waitFor(() => {
      expect(confirm).toHaveBeenCalledWith(expect.objectContaining({
        title: 'Stop stream',
        message: 'Stop stream for "Das Erste"?',
      }));
    });

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith('sid-live-1');
    });

    expect(toast).toHaveBeenCalledWith({
      kind: 'success',
      message: 'Stopped stream for "Das Erste".',
    });
  });
});
