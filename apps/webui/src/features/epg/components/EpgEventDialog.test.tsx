import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { EpgEventDialog } from './EpgEventDialog';
import type { EpgEvent, EpgChannel } from '../types';
import * as epgApi from '../epgApi';

vi.mock('../epgApi', () => ({
  fetchEpgEvents: vi.fn(),
}));

describe('EpgEventDialog Smart Reruns and Broadcast Selection', () => {
  const dummyChannels: EpgChannel[] = [
    { serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:', name: 'PULS 4 Austria' },
    { serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:', name: 'ProSieben Austria' },
    { serviceRef: '1:0:16:4460:453:1:C00000:0:0:0:', name: 'Kabel Eins Austria' },
  ];

  const now = 1791396000;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders a movie without "Als Serie aufnehmen" button and hides rerun section when no reruns exist', async () => {
    vi.mocked(epgApi.fetchEpgEvents).mockResolvedValueOnce([]);

    const movieEvent: EpgEvent = {
      serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
      title: 'Top Gun (1986)',
      desc: 'Actionfilm, USA 1986.',
      start: now + 3600,
      end: now + 10200,
    };

    render(
      <EpgEventDialog
        event={movieEvent}
        channel={dummyChannels[0]}
        channels={dummyChannels}
        currentTime={now}
        onClose={vi.fn()}
        onRecord={vi.fn()}
        onScheduleSeries={vi.fn()}
      />
    );

    // Title should be visible
    expect(screen.getByText('Top Gun (1986)')).toBeInTheDocument();

    // Movie action button should be "Aufnehmen" / "Record", NOT "Als Serie aufnehmen" / "Record as Series"
    expect(screen.getByRole('button', { name: /● (Record|Aufnehmen)/i })).toBeInTheDocument();
    expect(screen.queryByTestId('series-record-trigger')).not.toBeInTheDocument();

    // When no reruns exist, the rerun section is completely hidden
    expect(screen.queryByTestId('rerun-section-same')).not.toBeInTheDocument();
    expect(screen.queryByTestId('rerun-section-other')).not.toBeInTheDocument();
  });

  it('displays "Weitere Ausstrahlungen" for movies when valid reruns exist across channels', async () => {
    const movieEvent: EpgEvent = {
      serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:', // PULS 4
      title: 'Top Gun (1986)',
      desc: 'Actionfilm, USA 1986.',
      start: now + 3600,
      end: now + 10200,
    };

    const rerunOnKabelEins: EpgEvent = {
      serviceRef: '1:0:16:4460:453:1:C00000:0:0:0:', // Kabel Eins
      title: 'Top Gun (1986)',
      desc: 'Actionfilm (1986). Mit Tom Cruise.',
      start: now + 86400,
      end: now + 86400 + 6600,
    };

    vi.mocked(epgApi.fetchEpgEvents).mockResolvedValueOnce([rerunOnKabelEins]);
    const onRecordMock = vi.fn();

    render(
      <EpgEventDialog
        event={movieEvent}
        channel={dummyChannels[0]}
        channels={dummyChannels}
        currentTime={now}
        onClose={vi.fn()}
        onRecord={onRecordMock}
        onScheduleSeries={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByTestId('rerun-section-same')).toBeInTheDocument();
    });

    expect(screen.getByText(/Further Broadcasts|Weitere Ausstrahlungen/i)).toBeInTheDocument();
    expect(screen.getByText('Kabel Eins Austria')).toBeInTheDocument();

    // Clicking "Aufnehmen" directly on the rerun row calls onRecord for that specific rerun event
    const rerunRecordBtn = screen.getByRole('button', { name: /^⏺ (Record|Aufnehmen)/i });
    fireEvent.click(rerunRecordBtn);

    expect(onRecordMock).toHaveBeenCalledWith(rerunOnKabelEins);
  });

  it('renders series with "Diese Folge aufnehmen" and "Als Serie aufnehmen", and allows broadcast picking', async () => {
    const seriesEvent: EpgEvent = {
      serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:', // ProSieben
      title: 'Monk - Mr. Monk und die Hellseherin',
      desc: 'Staffel 3, Folge 7: Mr. Monk und die Hellseherin.',
      start: now + 3600,
      end: now + 6600,
    };

    const sameEpRerun: EpgEvent = {
      serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:', // PULS 4
      title: 'Monk (S03E07)',
      desc: 'Mr. Monk und die Hellseherin.',
      start: now + 86400,
      end: now + 86400 + 3000,
    };

    vi.mocked(epgApi.fetchEpgEvents).mockResolvedValueOnce([sameEpRerun]);
    const onRecordMock = vi.fn();
    const onCloseMock = vi.fn();

    render(
      <EpgEventDialog
        event={seriesEvent}
        channel={dummyChannels[1]}
        channels={dummyChannels}
        currentTime={now}
        onClose={onCloseMock}
        onRecord={onRecordMock}
        onScheduleSeries={vi.fn()}
      />
    );

    // Series actions
    expect(screen.getByRole('button', { name: /● (Record this episode|Diese Folge aufnehmen)/i })).toBeInTheDocument();
    expect(screen.getByTestId('series-record-trigger')).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByTestId('rerun-section-same')).toBeInTheDocument();
    });

    // Section title for series is "Diese Folge erneut" / "This Episode Again"
    expect(screen.getByText(/This Episode Again|Diese Folge erneut/i)).toBeInTheDocument();
    expect(screen.getByText('PULS 4 Austria')).toBeInTheDocument();

    // Clicking the main footer button "Diese Folge aufnehmen" when reruns exist opens "Welche Ausstrahlung aufnehmen?"
    const mainRecordBtn = screen.getByRole('button', { name: /● (Record this episode|Diese Folge aufnehmen)/i });
    fireEvent.click(mainRecordBtn);

    expect(screen.getByText(/Which broadcast to record\?|Welche Ausstrahlung aufnehmen\?/i)).toBeInTheDocument();
    expect(screen.getByText(/Current broadcast|Aktuelle Ausstrahlung/i)).toBeInTheDocument();

    // Selecting the rerun from the picker triggers recording for the rerun
    const rerunPickerItem = screen.getAllByRole('button', { name: /Record|Aufnehmen/i })[1];
    expect(rerunPickerItem).toBeDefined();
    fireEvent.click(rerunPickerItem!);

    expect(onRecordMock).toHaveBeenCalledWith(sameEpRerun);
    expect(onCloseMock).toHaveBeenCalled();
  });
});
