import { act, fireEvent, render, screen } from '@testing-library/react';
import type { ComponentProps } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EpgChannelList } from './EpgChannelList';

vi.mock('../../player/utils/playbackCapabilities', () => ({
  gatherPlaybackCapabilities: vi.fn().mockResolvedValue({
    capabilitiesVersion: 3,
    container: ['mp4', 'ts'],
    videoCodecs: ['h264'],
    audioCodecs: ['aac', 'ac3'],
    supportsHls: true,
    supportsRange: true,
    deviceType: 'test',
    runtimeProbeUsed: true,
    runtimeProbeVersion: 2,
    clientFamilyFallback: 'chromium_hlsjs',
    allowTranscode: true,
    hlsEngines: ['hlsjs'],
    preferredHlsEngine: 'hlsjs',
  }),
}));

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

async function renderChannelList(props: ComponentProps<typeof EpgChannelList>) {
  render(<EpgChannelList {...props} />);
  await act(async () => {});
}

describe('EpgChannelList playback affordance', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        mode: 'direct_stream',
        videoCodec: 'h264',
        audioCodec: 'ac3',
        decision: {
          mode: 'direct_stream',
          trace: {
            source: {
              width: 1920,
              height: 1080,
              videoCodec: 'h264',
              audioCodec: 'ac3',
            },
          },
        },
      }),
    }));
  });

  afterEach(() => {
    clearHostEnvironment();
    window.localStorage.clear();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('starts playback when the channel header is clicked', async () => {
    const onPlay = vi.fn();

    await renderChannelList({
      mode: 'main',
      channels: [{ id: 'svc-1', serviceRef: 'svc-1', name: 'Das Erste', number: '1' }],
      eventsByServiceRef: new Map([
        ['svc-1', [{ serviceRef: 'svc-1', start: 100, end: 200, title: 'Tagesschau' }]],
      ]),
      currentTime: 120,
      timeRangeHours: 6,
      expandedChannels: new Set(),
      onToggleExpand: () => {},
      onPlay,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Play Stream' }));

    expect(onPlay).toHaveBeenCalledTimes(1);
    expect(onPlay).toHaveBeenCalledWith(
      expect.objectContaining({ serviceRef: 'svc-1', name: 'Das Erste' })
    );
  });

  it('starts playback via keyboard on the channel header', async () => {
    const onPlay = vi.fn();

    await renderChannelList({
      mode: 'main',
      channels: [{ id: 'svc-8f', serviceRef: 'svc-8f', name: '8F Sender' }],
      eventsByServiceRef: new Map([
        ['svc-8f', [{ serviceRef: 'svc-8f', start: 100, end: 200, title: 'Live' }]],
      ]),
      currentTime: 120,
      timeRangeHours: 6,
      expandedChannels: new Set(),
      onToggleExpand: () => {},
      onPlay,
    });

    fireEvent.keyDown(screen.getByRole('button', { name: 'Play Stream' }), { key: 'Enter' });

    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('renders a device-specific playback badge for the channel', async () => {
    window.localStorage.setItem('XG2G_API_TOKEN', 'dev-token');

    await renderChannelList({
      mode: 'main',
      channels: [{ id: 'svc-badge-auth', serviceRef: 'svc-badge-auth', name: 'Das Erste', number: '101', resolution: '1920x1080', codec: 'h264' }],
      eventsByServiceRef: new Map([
        ['svc-badge-auth', [{ serviceRef: 'svc-badge-auth', start: 100, end: 200, title: 'Tagesschau' }]],
      ]),
      currentTime: 120,
      timeRangeHours: 6,
      expandedChannels: new Set(),
      onToggleExpand: () => {},
      onPlay: () => {},
    });

    expect(await screen.findByText('Remux')).toBeTruthy();
    expect(screen.getByText('1080p · h264/ac3')).toBeTruthy();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/live/stream-info'),
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({
          Authorization: 'Bearer dev-token',
        }),
      })
    );
  });

  it('jumps directly to a channel when digits are entered on TV hosts', async () => {
    setTvHostEnvironment();

    await renderChannelList({
      mode: 'main',
      channels: buildChannelRange(140),
      eventsByServiceRef: buildEventsByChannel(140),
      currentTime: 120,
      timeRangeHours: 6,
      expandedChannels: new Set(),
      onToggleExpand: () => {},
      onPlay: () => {},
    });

    const firstChannel = screen.getByText('1 · Channel 1').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;
    const targetChannel = screen.getByText('133 · Channel 133').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;

    firstChannel.focus();
    await act(async () => {
      fireEvent.keyDown(window, { key: '1' });
      fireEvent.keyDown(window, { key: '3' });
      fireEvent.keyDown(window, { key: '3' });
    });

    expect(document.activeElement).toBe(targetChannel);
    expect(screen.getByText('CH 133')).toBeTruthy();

  });

  it('accelerates channel navigation while holding down on TV hosts', async () => {
    vi.useFakeTimers();
    setTvHostEnvironment();

    await renderChannelList({
      mode: 'main',
      channels: buildChannelRange(40),
      eventsByServiceRef: buildEventsByChannel(40),
      currentTime: 120,
      timeRangeHours: 6,
      expandedChannels: new Set(),
      onToggleExpand: () => {},
      onPlay: () => {},
    });

    const firstChannel = screen.getByText('1 · Channel 1').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;
    const acceleratedTarget = screen.getByText('10 · Channel 10').closest('[data-xg2g-channel-focus="true"]') as HTMLElement;

    firstChannel.focus();
    await act(async () => {
      fireEvent.keyDown(window, { key: 'ArrowDown' });
      vi.advanceTimersByTime(360);
    });

    expect(document.activeElement).toBe(acceleratedTarget);
  });
});
