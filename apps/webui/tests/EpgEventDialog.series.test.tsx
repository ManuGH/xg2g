import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { EpgEventDialog, type ScheduleSeriesConfig } from '../src/features/epg/components/EpgEventDialog';
import type { EpgEvent, EpgChannel } from '../src/features/epg/types';

describe('EpgEventDialog Series Scheduling', () => {
  const mockEvent: EpgEvent = {
    id: 12345,
    title: 'Café PULS mit PULS 4 Aktuell',
    desc: 'Das Morgenmagazin von Puls 24 HD',
    start: 1727586000,
    end: 1727598600,
    serviceRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
  };

  const mockChannel: EpgChannel = {
    id: '1:0:19:14B8:407:1:C00000:0:0:0:',
    name: 'PULS 24 HD',
    serviceRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
  };

  it('renders details view initially with series recording button', () => {
    const onScheduleSeries = vi.fn();
    render(
      <EpgEventDialog
        event={mockEvent}
        channel={mockChannel}
        onClose={vi.fn()}
        onRecord={vi.fn()}
        onScheduleSeries={onScheduleSeries}
      />
    );

    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('Café PULS mit PULS 4 Aktuell');
    expect(screen.getByTestId('series-record-trigger')).toBeInTheDocument();
  });

  it('switches to series view, pre-fills cleaned title and 7-day retention preset', () => {
    render(
      <EpgEventDialog
        event={mockEvent}
        channel={mockChannel}
        onClose={vi.fn()}
        onRecord={vi.fn()}
        onScheduleSeries={vi.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('series-record-trigger'));

    // Check title updated to series dialog header
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(/Schedule Series Recording|Serienaufnahme/i);

    // Check keyword input prefilled with cleaned title "Café PULS"
    const keywordInput = screen.getByTestId('series-modal-keyword') as HTMLInputElement;
    expect(keywordInput.value).toBe('Café PULS');

    // Check suggestions chips are present
    expect(screen.getByRole('button', { name: 'Café PULS mit PULS 4 Aktuell' })).toBeInTheDocument();

    // Check 7-day retention preset is active
    expect(screen.getByText(/7 (Tage|days) \(Empfohlen\)/i)).toBeInTheDocument();
  });

  it('submits series rule with 7-day retention and channel reference', async () => {
    const onScheduleSeries = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();

    render(
      <EpgEventDialog
        event={mockEvent}
        channel={mockChannel}
        onClose={onClose}
        onRecord={vi.fn()}
        onScheduleSeries={onScheduleSeries}
      />
    );

    fireEvent.click(screen.getByTestId('series-record-trigger'));

    // Click Save button
    const saveButton = screen.getByTestId('series-modal-save');
    fireEvent.click(saveButton);

    await waitFor(() => {
      expect(onScheduleSeries).toHaveBeenCalledTimes(1);
    });

    const [calledEvent, calledConfig]: [EpgEvent, ScheduleSeriesConfig] = onScheduleSeries.mock.calls[0];
    expect(calledEvent).toBe(mockEvent);
    expect(calledConfig).toEqual({
      keyword: 'Café PULS',
      channelRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
      days: undefined, // daily = all 7 days omitted
      startWindow: undefined,
      retentionDays: 7,
    });
    expect(onClose).toHaveBeenCalled();
  });

  it('supports weekdays filter and 14-day retention preset', async () => {
    const onScheduleSeries = vi.fn().mockResolvedValue(undefined);

    render(
      <EpgEventDialog
        event={mockEvent}
        channel={mockChannel}
        onClose={vi.fn()}
        onRecord={vi.fn()}
        onScheduleSeries={onScheduleSeries}
      />
    );

    fireEvent.click(screen.getByTestId('series-record-trigger'));

    // Verify channel is displayed
    expect(screen.getByText('PULS 24 HD')).toBeInTheDocument();

    // Select "Werktags (Mo-Fr)" / "Weekdays (Mon-Fri)"
    fireEvent.click(screen.getByRole('button', { name: /Werktags|Weekdays/i }));

    // Select 14 days retention
    fireEvent.click(screen.getByRole('button', { name: /^(14 Tage|14 days)/i }));

    // Submit
    fireEvent.click(screen.getByTestId('series-modal-save'));

    await waitFor(() => {
      expect(onScheduleSeries).toHaveBeenCalledTimes(1);
    });

    const [, calledConfig] = onScheduleSeries.mock.calls[0];
    expect(calledConfig).toEqual({
      keyword: 'Café PULS',
      channelRef: '1:0:19:14B8:407:1:C00000:0:0:0:',
      days: [1, 2, 3, 4, 5],
      startWindow: undefined,
      retentionDays: 14,
    });
  });

  it('switches back to details view when Back button is clicked', () => {
    render(
      <EpgEventDialog
        event={mockEvent}
        channel={mockChannel}
        onClose={vi.fn()}
        onRecord={vi.fn()}
        onScheduleSeries={vi.fn()}
      />
    );

    fireEvent.click(screen.getByTestId('series-record-trigger'));
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(/Schedule Series Recording|Serienaufnahme/i);

    fireEvent.click(screen.getByRole('button', { name: /Back|Zurück/i }));
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('Café PULS mit PULS 4 Aktuell');
    expect(screen.getByTestId('series-record-trigger')).toBeInTheDocument();
  });
});
