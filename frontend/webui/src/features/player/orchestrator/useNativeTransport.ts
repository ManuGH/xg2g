import { useRef, useCallback, useState } from 'react';
import type { RefObject } from 'react';
import type { PlayerAudioTrack } from '../../types/v3-player';

export interface NativeTransportOptions {
  onAudioTracksChange?: (tracks: PlayerAudioTrack[], activeTrackId: number) => void;
}

export interface NativeTransportResult {
  videoRef: RefObject<HTMLVideoElement | null>;
  nativeAudioTracks: PlayerAudioTrack[];
  activeNativeTrack: number;
  bindNativeAudioTracks: (videoEl: HTMLVideoElement) => void;
  changeNativeAudioTrack: (trackId: number) => void;
}

// Sub-hook isolating native WebKit/Safari HTML5 video transport and controls per §A3
export function useNativeTransport(options?: NativeTransportOptions): NativeTransportResult {
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const [nativeAudioTracks, setNativeAudioTracks] = useState<PlayerAudioTrack[]>([]);
  const [activeNativeTrack, setActiveNativeTrack] = useState<number>(-1);

  const bindNativeAudioTracks = useCallback(
    (videoEl: HTMLVideoElement) => {
      videoRef.current = videoEl;
      if (!videoEl || !('audioTracks' in videoEl)) return;

      const tracksObj = (videoEl as any).audioTracks;
      if (!tracksObj || typeof tracksObj.length !== 'number') return;

      const tracks: PlayerAudioTrack[] = [];
      let activeIdx = -1;
      for (let i = 0; i < tracksObj.length; i++) {
        const tr = tracksObj[i];
        const isEnabled = Boolean(tr.enabled);
        if (isEnabled && activeIdx === -1) {
          activeIdx = i;
        }
        tracks.push({
          id: i,
          name: tr.label || tr.language || `Track ${i + 1}`,
          language: tr.language || '',
          default: isEnabled,
        });
      }
      setNativeAudioTracks(tracks);
      setActiveNativeTrack(activeIdx);
      options?.onAudioTracksChange?.(tracks, activeIdx);
    },
    [options],
  );

  const changeNativeAudioTrack = useCallback((trackId: number) => {
    if (videoRef.current && 'audioTracks' in videoRef.current) {
      const tracks = (videoRef.current as any).audioTracks;
      if (tracks && tracks.length > trackId) {
        for (let i = 0; i < tracks.length; i++) {
          tracks[i].enabled = i === trackId;
        }
        setActiveNativeTrack(trackId);
      }
    }
  }, []);

  return {
    videoRef,
    nativeAudioTracks,
    activeNativeTrack,
    bindNativeAudioTracks,
    changeNativeAudioTrack,
  };
}
