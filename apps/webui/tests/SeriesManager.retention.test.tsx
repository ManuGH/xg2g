import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import SeriesManager from '../src/components/SeriesManager';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';
import { UiOverlayProvider } from '../src/context/UiOverlayContext';

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSeriesRules: vi.fn().mockResolvedValue({ data: [] }),
    getServices: vi.fn().mockResolvedValue({ data: [] }),
    createSeriesRule: vi.fn(),
    updateSeriesRule: vi.fn(),
    runSeriesRule: vi.fn(),
  };
});

describe('SeriesManager Retention Policy UI', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('allows setting retentionDays to 7 and sends it in create payload', async () => {
    (client.createSeriesRule as any).mockResolvedValue({ data: { id: 'rule-new', keyword: 'Cafe Puls' } });

    render(
      <MemoryRouter>
        <UiOverlayProvider>
          <SeriesManager />
        </UiOverlayProvider>
      </MemoryRouter>
    );

    const addBtn = await screen.findByTestId('series-add-btn');
    fireEvent.click(addBtn);

    const keywordInput = screen.getByTestId('series-edit-keyword');
    fireEvent.change(keywordInput, { target: { value: 'Cafe Puls' } });

    // Click 7 Days preset
    const sevenDayBtn = screen.getByRole('button', { name: /7 Tage/i });
    fireEvent.click(sevenDayBtn);

    const retentionInput = screen.getByTestId('series-edit-retention') as HTMLInputElement;
    expect(retentionInput.value).toBe('7');

    const saveBtn = screen.getByTestId('series-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.createSeriesRule).toHaveBeenCalledOnce();
      const call = (client.createSeriesRule as any).mock.calls[0][0];
      expect(call.body.keyword).toBe('Cafe Puls');
      expect(call.body.retentionDays).toBe(7);
    });
  });

  it('renders retention information on rule cards', async () => {
    (client.getSeriesRules as any).mockResolvedValue({
      data: [
        {
          id: 'rule-cafepuls',
          enabled: true,
          keyword: 'Cafe Puls',
          retentionDays: 7,
          priority: 5,
        },
      ],
    });

    render(
      <MemoryRouter>
        <UiOverlayProvider>
          <SeriesManager />
        </UiOverlayProvider>
      </MemoryRouter>
    );

    expect(await screen.findByText('Cafe Puls')).toBeInTheDocument();
    expect(screen.getByText(/7 days \(auto-delete\)/i)).toBeInTheDocument();
  });
});
