import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { PlaybackTraceRuntimeReplay } from '../../../client-ts';
import { PlayerRuntimeReplayExport, serializeRuntimePolicyReplay } from './PlayerRuntimeReplayExport';

const replay: PlaybackTraceRuntimeReplay = {
  metadata: {
    sessionId: 'sess-123',
    sourceType: 'live',
  },
  ticks: [
    {
      input: {
        tickAt: '2026-04-19T00:00:00Z',
      },
      expected: {
        action: 'probe_up',
      },
    },
  ],
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe('PlayerRuntimeReplayExport', () => {
  it('copies the replay payload to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });

    render(<PlayerRuntimeReplayExport replay={replay} />);

    fireEvent.click(screen.getByRole('button', { name: /copy replay/i }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(serializeRuntimePolicyReplay(replay));
    });
    expect(screen.getByRole('status')).toHaveTextContent(/replay copied/i);
  });

  it('shows an error state when clipboard copy fails', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.defineProperty(window.navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });

    render(<PlayerRuntimeReplayExport replay={replay} />);

    fireEvent.click(screen.getByRole('button', { name: /copy replay/i }));

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent(/replay copy failed/i);
    });
  });
});
