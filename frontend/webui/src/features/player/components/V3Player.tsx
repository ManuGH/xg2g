import { useRef } from 'react';
import type {
  HlsInstanceRef,
  V3PlayerProps,
  VideoElementRef,
} from '../../../types/v3-player';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';
import { V3PlayerView } from './V3PlayerView';

function V3Player(props: V3PlayerProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
  const { viewState, actions } = usePlaybackOrchestrator(props, {
    containerRef,
    videoRef,
    hlsRef,
    resumePrimaryActionRef,
  });

  // `durationSeconds` remains the normative duration truth at the composition seam.
  void viewState.playback.durationSeconds;
  const currentChannel = 'channel' in props ? props.channel : undefined;

  return (
    <V3PlayerView
      containerRef={containerRef}
      videoRef={videoRef}
      resumePrimaryActionRef={resumePrimaryActionRef}
      viewState={viewState}
      actions={actions}
      channelList={props.channelList}
      currentChannel={currentChannel}
      onSelectChannel={props.onSelectChannel}
    />
  );
}

export default V3Player;
