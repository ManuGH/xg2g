import { useCallback, useRef, useState } from 'react';
import type {
  HlsInstanceRef,
  V3PlayerProps,
  VideoElementRef,
} from '../../../types/v3-player';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';
import { V3PlayerView } from './V3PlayerView';
import { ChannelSwitcher } from './ChannelSwitcher';

function V3Player(props: V3PlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
  const [channelsOpen, setChannelsOpen] = useState(false);
  const { viewState, actions } = usePlaybackOrchestrator(props, {
    containerRef,
    videoRef,
    hlsRef,
    resumePrimaryActionRef,
  });

  // `durationSeconds` remains the normative duration truth at the composition seam.
  void viewState.playback.durationSeconds;

  const hasChannels = Boolean(
    props.onSwitchChannel &&
      ((props.channels && props.channels.length > 0) || (props.bouquets && props.bouquets.length > 0)),
  );
  const handleCloseChannels = useCallback(() => setChannelsOpen(false), []);

  return (
    <V3PlayerView
      containerRef={containerRef}
      videoRef={videoRef}
      resumePrimaryActionRef={resumePrimaryActionRef}
      viewState={viewState}
      actions={actions}
      onOpenChannels={hasChannels ? () => setChannelsOpen(true) : undefined}
    >
      {hasChannels ? (
        <ChannelSwitcher
          channels={props.channels ?? []}
          current={'channel' in props ? props.channel : undefined}
          onSwitch={props.onSwitchChannel!}
          open={channelsOpen}
          onClose={handleCloseChannels}
          token={props.token}
          bouquets={props.bouquets}
          selectedBouquet={props.selectedBouquet}
          onSelectBouquet={props.onSelectBouquet}
        />
      ) : null}
    </V3PlayerView>
  );
}

export default V3Player;
