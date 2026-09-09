import { useRef } from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Service } from '../../client-ts';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

function ChannelSwitchHarness({ channel }: { channel: Service }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

  const { viewState, actions } = usePlaybackOrchestrator(
    { autoStart: true, channel } as unknown as V3PlayerProps,
    { containerRef, videoRef, hlsRef, resumePrimaryActionRef }
  );

  return (
    <div>
      <video ref={videoRef} data-testid="player-video" />
      <span data-testid="channel-name">{viewState.channelName}</span>
      <span data-testid="service-ref">{viewState.serviceRef}</span>
      <span data-testid="is-muted">{String(viewState.isMuted)}</span>
      <button data-testid="unmute-btn" onClick={actions.toggleMute} type="button">
        Unmute
      </button>
    </div>
  );
}

describe('usePlaybackOrchestrator channel switching', () => {
  it('updates channel name and service ref when channel prop changes', async () => {
    const channel1: Service = {
      id: 'ch-1',
      serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
      name: 'ORF 1 HD',
    };
    const channel2: Service = {
      id: 'ch-2',
      serviceRef: '1:0:19:1334:3EF:1:C00000:0:0:0',
      name: 'ORF 2 HD',
    };

    const { rerender } = render(<ChannelSwitchHarness channel={channel1} />);
    expect(screen.getByTestId('channel-name')).toHaveTextContent('ORF 1 HD');
    expect(screen.getByTestId('service-ref')).toHaveTextContent('1:0:19:132F:3EF:1:C00000:0:0:0');

    act(() => {
      rerender(<ChannelSwitchHarness channel={channel2} />);
    });

    await waitFor(() => {
      expect(screen.getByTestId('channel-name')).toHaveTextContent('ORF 2 HD');
      expect(screen.getByTestId('service-ref')).toHaveTextContent('1:0:19:1334:3EF:1:C00000:0:0:0');
    });
  });

  it('preserves unmuted state across channel switches', async () => {
    const channel1: Service = {
      id: 'ch-1',
      serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0',
      name: 'ORF 1 HD',
    };
    const channel2: Service = {
      id: 'ch-2',
      serviceRef: '1:0:19:1334:3EF:1:C00000:0:0:0',
      name: 'ORF 2 HD',
    };

    const { rerender } = render(<ChannelSwitchHarness channel={channel1} />);

    // User un-mutes
    act(() => {
      screen.getByTestId('unmute-btn').click();
    });
    expect(screen.getByTestId('is-muted')).toHaveTextContent('false');

    // Switch channel
    act(() => {
      rerender(<ChannelSwitchHarness channel={channel2} />);
    });

    await waitFor(() => {
      expect(screen.getByTestId('channel-name')).toHaveTextContent('ORF 2 HD');
    });
    expect(screen.getByTestId('is-muted')).toHaveTextContent('false');
  });
});
