// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import OnAirHero from './OnAirHero';

const NOW_MS = 1_770_000_000_000;
const HEALTH_CHIP = { state: 'success' as const, label: 'System healthy' };
const ACTION = { label: 'Open Live TV', onAction: vi.fn() };

function receiverOnAir(overrides: Record<string, unknown> = {}) {
  return {
    status: 'ok' as const,
    channel: { name: 'ORF 1' },
    now: {
      title: 'Tatort: Der schwarze Troll',
      // started 15 minutes ago, runs for an hour
      beginTimestamp: NOW_MS / 1000 - 15 * 60,
      durationSec: 3600,
    },
    next: { title: 'ZIB 2' },
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW_MS);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('OnAirHero', () => {
  it('leads with the programme, not the channel', () => {
    render(<OnAirHero receiver={receiverOnAir()} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Tatort: Der schwarze Troll');
    expect(screen.getByText('ORF 1')).toBeInTheDocument();
    expect(screen.getByText('ZIB 2')).toBeInTheDocument();
  });

  it('puts the programme on its clock', () => {
    render(<OnAirHero receiver={receiverOnAir()} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);

    expect(screen.getByRole('group', { name: /programme progress/i })).toBeInTheDocument();
    expect(screen.getByText('15 min in')).toBeInTheDocument();
    expect(screen.getByText('45 min left')).toBeInTheDocument();
  });

  it('keeps the clock running while the surface stays open', () => {
    render(<OnAirHero receiver={receiverOnAir()} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);
    expect(screen.getByText('15 min in')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(60_000);
    });

    expect(screen.getByText('16 min in')).toBeInTheDocument();
    expect(screen.getByText('44 min left')).toBeInTheDocument();
  });

  // The null contract from computeOnAirProgress, seen from the outside: a
  // timeline is only ever drawn for a programme whose anchors the receiver
  // actually reported.
  it('draws no timeline when the guide gave no duration', () => {
    const receiver = receiverOnAir({
      now: { title: 'Tatort: Der schwarze Troll', beginTimestamp: NOW_MS / 1000 - 900 },
    });
    render(<OnAirHero receiver={receiver} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Tatort: Der schwarze Troll');
    expect(screen.queryByRole('group', { name: /programme progress/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/min in$/)).not.toBeInTheDocument();
  });

  it('falls back to the channel when no programme is known', () => {
    const receiver = { status: 'ok' as const, channel: { name: 'ORF 1' } };
    render(<OnAirHero receiver={receiver} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('ORF 1');
    expect(screen.queryByRole('group', { name: /programme progress/i })).not.toBeInTheDocument();
  });

  it('says the box is asleep instead of showing a stale programme', () => {
    const receiver = receiverOnAir({ status: 'unavailable' as const });
    render(<OnAirHero receiver={receiver} healthChip={HEALTH_CHIP} primaryAction={ACTION} />);

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('The receiver is asleep');
    expect(screen.getByText('Standby')).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: /programme progress/i })).not.toBeInTheDocument();
  });

  it('shows what is being recorded only while a recording runs', () => {
    const { rerender } = render(
      <OnAirHero
        receiver={receiverOnAir()}
        recording={{ isRecording: false }}
        healthChip={HEALTH_CHIP}
        primaryAction={ACTION}
      />,
    );
    expect(screen.queryByText(/Recording/)).not.toBeInTheDocument();

    rerender(
      <OnAirHero
        receiver={receiverOnAir()}
        recording={{ isRecording: true, serviceName: 'ORF 2' }}
        healthChip={HEALTH_CHIP}
        primaryAction={ACTION}
      />,
    );
    expect(screen.getByText('Recording ORF 2')).toBeInTheDocument();
  });

  it('renders without a receiver at all', () => {
    render(<OnAirHero healthChip={HEALTH_CHIP} primaryAction={ACTION} />);
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: /programme progress/i })).not.toBeInTheDocument();
  });
});
