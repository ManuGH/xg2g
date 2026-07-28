import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';

const { confirm, toast } = vi.hoisted(() => ({ confirm: vi.fn(), toast: vi.fn() }));

// The hey-api SDK resolves with { error } instead of throwing on HTTP/network failures.
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSeriesRules: vi.fn().mockResolvedValue({ data: [] }),
    getServices: vi.fn().mockResolvedValue({ data: [] }),
    createSeriesRule: vi.fn(),
    updateSeriesRule: vi.fn(),
    deleteSeriesRule: vi.fn(),
    runSeriesRule: vi.fn(),
  };
});

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({ confirm, toast }),
}));

import SeriesManager from '../src/components/SeriesManager';

const failure = {
  data: undefined,
  error: {
    type: 'about:blank',
    title: 'Conflict',
    status: 409,
    requestId: 'r1',
    code: 'RULE_CONFLICT',
    detail: 'Rule already exists',
  },
  response: { status: 409 },
};

describe('SeriesManager failure handling (SDK resolves { error } instead of throwing)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('surfaces an error toast (not silent success) when rule creation fails', async () => {
    (client.createSeriesRule as any).mockResolvedValue(failure);

    render(<MemoryRouter><SeriesManager /></MemoryRouter>);

    fireEvent.click(await screen.findByTestId('series-add-btn'));
    fireEvent.change(screen.getByTestId('series-edit-keyword'), { target: { value: 'Tatort' } });
    fireEvent.click(screen.getByTestId('series-edit-save'));

    await waitFor(() => expect(client.createSeriesRule).toHaveBeenCalledOnce());
    await waitFor(() => expect(toast).toHaveBeenCalledWith(expect.objectContaining({ kind: 'error' })));
  });

  it('surfaces an error toast when rule deletion fails (rule would otherwise silently reappear)', async () => {
    (client.getSeriesRules as any).mockResolvedValueOnce({
      data: [{ id: 'rule-1', enabled: true, keyword: 'Tatort', channelRef: '', days: [], startWindow: '', priority: 3 }],
    });
    (client.deleteSeriesRule as any).mockResolvedValue(failure);
    confirm.mockResolvedValue(true);

    render(<MemoryRouter><SeriesManager /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(client.deleteSeriesRule).toHaveBeenCalledOnce());
    await waitFor(() => expect(toast).toHaveBeenCalledWith(expect.objectContaining({ kind: 'error' })));
  });
});
