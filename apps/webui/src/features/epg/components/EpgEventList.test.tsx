import epgCss from '../EPG.module.css?raw';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EpgEventRow } from './EpgEventList';
import type { EpgEvent } from '../types';

describe('EpgEventRow recording states and progress stages', () => {
  const baseEvent: EpgEvent = {
    serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0:',
    start: 1000,
    end: 2000,
    title: 'Test Broadcast',
    desc: 'Broadcast description line 1\nBroadcast description line 2\nExtra long text that should be clamped',
  };

  it('renders "REC" active indicator when isRecorded is true and broadcast is in progress', () => {
    // Current time is 1500 (between 1000 and 2000 => inProgress)
    render(
      <EpgEventRow
        event={baseEvent}
        currentTime={1500}
        onRecord={vi.fn()}
        isRecorded={true}
      />
    );

    const recIndicator = screen.getByText('REC');
    expect(recIndicator).toBeInTheDocument();
    expect(screen.queryByText('Geplant')).not.toBeInTheDocument();
  });

  it('renders "Geplant" indicator when isRecorded is true and broadcast is in the future', () => {
    // Current time is 500 (before start 1000 => not inProgress)
    render(
      <EpgEventRow
        event={baseEvent}
        currentTime={500}
        onRecord={vi.fn()}
        isRecorded={true}
      />
    );

    const plannedIndicator = screen.getByText(/Geplant|Scheduled/i);
    expect(plannedIndicator).toBeInTheDocument();
  });

  it('renders record button affordance when not recorded and triggers onRecord on click', () => {
    const onRecordMock = vi.fn();
    render(
      <EpgEventRow
        event={baseEvent}
        currentTime={500}
        onRecord={onRecordMock}
        isRecorded={false}
      />
    );

    const recButton = screen.getByRole('button', { name: /(Sendung aufnehmen|Schedule recording)/i });
    expect(recButton).toBeInTheDocument();
    fireEvent.click(recButton);
    expect(onRecordMock).toHaveBeenCalledWith(baseEvent);
  });

  it('applies blue, amber, and orange progress bar tone styles according to completion percentage', () => {
    // 1. < 70%: 30% progress (start 1000, end 2000, current 1300)
    const { container: c1 } = render(
      <EpgEventRow
        event={baseEvent}
        currentTime={1300}
        onRecord={vi.fn()}
      />
    );
    const bar1 = c1.querySelector('[class*="progressBar"]');
    expect(bar1).toBeInTheDocument();
    expect(bar1?.className).not.toMatch(/progressBarAmber/);
    expect(bar1?.className).not.toMatch(/progressBarEnding/);

    // 2. 70-89%: 75% progress (current 1750)
    const { container: c2 } = render(
      <EpgEventRow
        event={baseEvent}
        currentTime={1750}
        onRecord={vi.fn()}
      />
    );
    const bar2 = c2.querySelector('[class*="progressBarAmber"]');
    expect(bar2).toBeInTheDocument();

    // 3. >= 90%: 95% progress (current 1950)
    const { container: c3 } = render(
      <EpgEventRow
        event={baseEvent}
        currentTime={1950}
        onRecord={vi.fn()}
      />
    );
    const bar3 = c3.querySelector('[class*="progressBarEnding"]');
    expect(bar3).toBeInTheDocument();
  });

  it('enforces horizontal gradient styling for amber and ending progress stages instead of solid fills', () => {
    // Negative control: reject old solid background fills
    expect(epgCss).not.toMatch(/\.progressBarAmber\s*\{[^}]*background:\s*var\(--status-warning\);/);
    expect(epgCss).not.toMatch(/\.progressBarEnding\s*\{[^}]*background:\s*var\(--status-orange\);/);

    // Positive assertion: require horizontal linear gradients with left-to-right progression
    expect(epgCss).toMatch(/\.progressBarAmber\s*\{[^}]*background:\s*linear-gradient\(\s*90deg/);
    expect(epgCss).toMatch(/\.progressBarEnding\s*\{[^}]*background:\s*linear-gradient\(\s*90deg/);
  });
});
