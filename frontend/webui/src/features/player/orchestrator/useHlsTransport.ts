import { useRef, useCallback, useState } from 'react';
import type { RefObject } from 'react';
import Hls from '../lib/hlsRuntime';
import type { PlayerAudioTrack } from '../../types/v3-player';

export interface HlsTransportOptions {
  onAudioTracksChange?: (tracks: PlayerAudioTrack[], activeTrackId: number) => void;
  onError?: (error: unknown) => void;
}

export interface HlsTransportResult {
  hlsRef: RefObject<Hls | null>;
  audioTracks: PlayerAudioTrack[];
  activeAudioTrack: number;
  attachHls: (hlsInstance: Hls) => void;
  destroyHls: () => void;
  changeAudioTrack: (trackId: number) => void;
}

// Sub-hook isolating HLS.js transport lifecycle and audio track management per §A3
export function useHlsTransport(options?: HlsTransportOptions): HlsTransportResult {
  const hlsRef = useRef<Hls | null>(null);
  const [audioTracks, setAudioTracks] = useState<PlayerAudioTrack[]>([]);
  const [activeAudioTrack, setActiveAudioTrack] = useState<number>(-1);

  const destroyHls = useCallback(() => {
    if (hlsRef.current) {
      try {
        hlsRef.current.destroy();
      } catch {
        // ignore cleanup errors
      }
      hlsRef.current = null;
    }
    setAudioTracks([]);
    setActiveAudioTrack(-1);
  }, []);

  const attachHls = useCallback(
    (hlsInstance: Hls) => {
      destroyHls();
      hlsRef.current = hlsInstance;

      hlsInstance.on(Hls.Events.AUDIO_TRACKS_UPDATED, (_event, data) => {
        if (!data || !data.audioTracks) return;
        const tracks: PlayerAudioTrack[] = data.audioTracks.map((t, idx) => ({
          id: idx,
          name: t.name || t.lang || `Track ${idx + 1}`,
          language: t.lang || '',
          default: Boolean(t.default),
        }));
        setAudioTracks(tracks);
        const activeId = hlsInstance.audioTrack;
        setActiveAudioTrack(activeId);
        options?.onAudioTracksChange?.(tracks, activeId);
      });

      hlsInstance.on(Hls.Events.AUDIO_TRACK_SWITCHED, (_event, data) => {
        if (data && typeof data.id === 'number') {
          setActiveAudioTrack(data.id);
        }
      });
    },
    [destroyHls, options],
  );

  const changeAudioTrack = useCallback((trackId: number) => {
    if (hlsRef.current) {
      hlsRef.current.audioTrack = trackId;
      setActiveAudioTrack(trackId);
    }
  }, []);

  return {
    hlsRef,
    audioTracks,
    activeAudioTrack,
    attachHls,
    destroyHls,
    changeAudioTrack,
  };
}
