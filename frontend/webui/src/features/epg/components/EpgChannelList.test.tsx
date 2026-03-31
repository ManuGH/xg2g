import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EpgChannelList } from './EpgChannelList';

function setTvHostEnvironment() {
  (window as Window & {
    __XG2G_HOST__?: {
      platform: 'android-tv';
      isTv: boolean;
      supportsKeepScreenAwake: boolean;
      supportsHostMediaKeys: boolean;
      supportsInputFocus: boolean;
      supportsNativePlayback: boolean;
    };
  }).__XG2G_HOST__ = {
    platform: 'android-tv',
    isTv: true,
    supportsKeepScreenAwake: true,
    supportsHostMediaKeys: true,
    supportsInputFocus: true,
    supportsNativePlayback: true,
  };
}

function clearHostEnvironment() {
  delete (window as Window & { __XG2G_HOST__?: unknown }).__XG2G_HOST__;
}

function buildChannelRange(count: number) {
  return Array.from({ length: count }, (_, index) => {
    const number = String(index + 1);
    return {
      id: `svc-${number}`,
      serviceRef: `svc-${number}`,
      name: `Channel ${number}`,
      number,
    };
  });
}

function buildEventsByChannel(count: number) {
  return new Map(
    Array.from({ length: count }, (_, index) => {
      const number = String(index + 1);
      return [
        `svc-${number}`,
        [{ serviceRef: `svc-${number}`, start: 100, end: 200, title: `Show ${number}` }],
      ];
    })
  );
}

describe('EpgChannelList playback affordance', () => {
  afterEach(() => {
    clearHostEnvironment();
  });

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

    fireEvent.click(screen.getByRole('button', { name: 'Play Stream' }));

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

    fireEvent.keyDown(screen.getByRole('button', { name: 'Play Stream' }), { key: 'Enter' });

    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('jumps directly to a channel when digits are entered on TV hosts', () => {
    setTvHostEnvironment();

    render(
      <EpgChannelList
        mode="main"
        channels={buildChannelRange(140)}
        eventsByServiceRef={buildEventsByChannel(140)}
        currentTime={120}
        timeRangeHours={6}
        expandedChannels={new Set()}
        onToggleExpand={() => {}}
        onPlay={() => {}}
      />
    );

    const firstChannel = screen.getByText('1 · Channel 1').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;
    const targetChannel = screen.getByText('133 · Channel 133').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;

    firstChannel.focus();
    fireEvent.keyDown(window, { key: '1' });
    fireEvent.keyDown(window, { key: '3' });
    fireEvent.keyDown(window, { key: '3' });

    expect(document.activeElement).toBe(targetChannel);
    expect(screen.getByText('CH 133')).toBeTruthy();

  });

  it('accelerates channel navigation while holding down on TV hosts', () => {
    setTvHostEnvironment();

    render(
      <EpgChannelList
        mode="main"
        channels={buildChannelRange(40)}
        eventsByServiceRef={buildEventsByChannel(40)}
        currentTime={120}
        timeRangeHours={6}
        expandedChannels={new Set()}
        onToggleExpand={() => {}}
        onPlay={() => {}}
      />
    );

    const firstChannel = screen.getByText('1 · Channel 1').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;
    const acceleratedTarget = screen.getByText('14 · Channel 14').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;

    firstChannel.focus();
    fireEvent.keyDown(window, { key: 'ArrowDown' });
    fireEvent.keyDown(window, { key: 'ArrowDown', repeat: true });
    fireEvent.keyDown(window, { key: 'ArrowDown', repeat: true });

    expect(document.activeElement).toBe(acceleratedTarget);

  });
});
