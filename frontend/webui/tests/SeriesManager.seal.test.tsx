import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import SeriesManager from '../src/components/SeriesManager';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as client from '../src/client-ts';
import { UiOverlayProvider } from '../src/context/UiOverlayContext';

// Mock the API client
vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<any>('../src/client-ts');
  return {
    ...actual,
    getSeriesRules: vi.fn().mockResolvedValue({ data: [] }),
    getServices: vi.fn().mockResolvedValue({ data: [] }),
    createSeriesRule: vi.fn(),
    updateSeriesRule: vi.fn(),
  };
});

describe('SeriesManager Truth Sealing (UI-INV-SERIES-001)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('omits optional filters from create payload if not set', async () => {
    render(
      <UiOverlayProvider>
        <SeriesManager />
      </UiOverlayProvider>
    );

    // Open "New Rule" modal (Wait for loading to finish)
    const addBtn = await screen.findByTestId('series-add-btn');
    fireEvent.click(addBtn);

    // Set keyword
    const keywordInput = screen.getByTestId('series-edit-keyword');
    fireEvent.change(keywordInput, { target: { value: 'Tatort' } });

    // Save
    const saveBtn = screen.getByTestId('series-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.createSeriesRule).toHaveBeenCalledOnce();
      const call = (client.createSeriesRule as any).mock.calls[0][0];

      // Asserts
      expect(call.body.keyword).toBe('Tatort');
      expect(call.body).not.toHaveProperty('channelRef');
      expect(call.body).not.toHaveProperty('days');
      expect(call.body).not.toHaveProperty('startWindow');
    });
  });

  it('updates an existing rule instead of falling back to recreate', async () => {
    (client.getSeriesRules as any).mockResolvedValueOnce({
      data: [{
        id: 'rule-1',
        enabled: true,
        keyword: 'Tatort',
        channelRef: '',
        days: [],
        startWindow: '',
        priority: 3,
      }],
    });

    render(
      <UiOverlayProvider>
        <SeriesManager />
      </UiOverlayProvider>
    );

    const editButton = await screen.findByRole('button', { name: 'Edit' });
    fireEvent.click(editButton);

    const keywordInput = screen.getByTestId('series-edit-keyword');
    fireEvent.change(keywordInput, { target: { value: 'Polizeiruf' } });

    const saveBtn = screen.getByTestId('series-edit-save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(client.updateSeriesRule).toHaveBeenCalledOnce();
      expect(client.createSeriesRule).not.toHaveBeenCalled();

      const call = (client.updateSeriesRule as any).mock.calls[0][0];
      expect(call.path).toEqual({ id: 'rule-1' });
      expect(call.body).toEqual({
        enabled: true,
        keyword: 'Polizeiruf',
        priority: 3,
      });
    });
  });
});
