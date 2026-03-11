import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { EpgChannelList } from './EpgChannelList';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}));

describe('EpgChannelList playback affordance', () => {
  it('starts playback when the channel header is clicked', () => {
    const onPlay = vi.fn();

    render(
      <EpgChannelList
        mode="main"
        channels={[{ id: 'svc-1', serviceRef: 'svc-1', name: 'Das Erste', number: '1' }]}
        eventsByServiceRef={new Map([
          ['svc-1', [{ serviceRef: 'svc-1', start: 100, end: 200, title: 'Tagesschau' }]],
        ])}
        currentTime={120}
        timeRangeHours={6}
        expandedChannels={new Set()}
        onToggleExpand={() => {}}
        onPlay={onPlay}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: 'epg.playStream' }));

    expect(onPlay).toHaveBeenCalledTimes(1);
    expect(onPlay).toHaveBeenCalledWith(
      expect.objectContaining({ serviceRef: 'svc-1', name: 'Das Erste' })
    );
  });

  it('starts playback via keyboard on the channel header', () => {
    const onPlay = vi.fn();

    render(
      <EpgChannelList
        mode="main"
        channels={[{ id: 'svc-8f', serviceRef: 'svc-8f', name: '8F Sender' }]}
        eventsByServiceRef={new Map([
          ['svc-8f', [{ serviceRef: 'svc-8f', start: 100, end: 200, title: 'Live' }]],
        ])}
        currentTime={120}
        timeRangeHours={6}
        expandedChannels={new Set()}
        onToggleExpand={() => {}}
        onPlay={onPlay}
      />
    );

    fireEvent.keyDown(screen.getByRole('button', { name: 'epg.playStream' }), { key: 'Enter' });

    expect(onPlay).toHaveBeenCalledTimes(1);
  });
});
