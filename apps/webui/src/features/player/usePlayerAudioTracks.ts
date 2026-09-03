import { useCallback, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { HlsInstanceRef, PlayerAudioTrack, VideoElementRef } from '../../types/v3-player';

export function areAudioTrackListsEqual(current: PlayerAudioTrack[], next: PlayerAudioTrack[]): boolean {
  return current.length === next.length && current.every((track, index) => {
    const candidate = next[index];
    return candidate !== undefined
      && track.key === candidate.key
      && track.engineIndex === candidate.engineIndex
      && track.nativeId === candidate.nativeId
      && track.language === candidate.language
      && track.label === candidate.label
      && track.kind === candidate.kind
      && track.id === candidate.id
      && track.name === candidate.name;
  });
}

export interface UsePlayerAudioTracksProps {
  videoRef: RefObject<VideoElementRef>;
  hlsRef: MutableRefObject<HlsInstanceRef>;
}

export interface UsePlayerAudioTracksResult {
  audioTracks: PlayerAudioTrack[];
  activeAudioTrack: number;
  handleAudioTracksUpdated: (nextTracks: PlayerAudioTrack[]) => void;
  setActiveAudioTrack: Dispatch<SetStateAction<number>>;
  changeAudioTrack: (trackIdentifier: PlayerAudioTrack | number) => void;
  resetAudioTracks: () => void;
}

export function usePlayerAudioTracks({
  videoRef,
  hlsRef,
}: UsePlayerAudioTracksProps): UsePlayerAudioTracksResult {
  const [audioTracks, setAudioTracks] = useState<PlayerAudioTrack[]>([]);
  const [activeAudioTrack, setActiveAudioTrack] = useState<number>(-1);

  const handleAudioTracksUpdated = useCallback((nextTracks: PlayerAudioTrack[]) => {
    setAudioTracks((currentTracks) => (
      areAudioTrackListsEqual(currentTracks, nextTracks) ? currentTracks : nextTracks
    ));
  }, []);

  const changeAudioTrack = useCallback((trackIdentifier: PlayerAudioTrack | number) => {
    const targetTrack = typeof trackIdentifier === 'object' && trackIdentifier !== null
      ? trackIdentifier
      : audioTracks.find((t) => t.id === trackIdentifier)
        ?? audioTracks.find((t) => t.engineIndex === trackIdentifier)
        ?? null;

    if (hlsRef.current) {
      const hlsIndex = targetTrack ? targetTrack.engineIndex : (typeof trackIdentifier === 'number' ? trackIdentifier : 0);
      hlsRef.current.audioTrack = hlsIndex;
    } else if (videoRef.current && 'audioTracks' in videoRef.current) {
      const tracks = (videoRef.current as any).audioTracks;
      if (tracks) {
        for (let i = 0; i < tracks.length; i++) {
          const nativeTrack = tracks[i];
          const matchesNativeId = Boolean(targetTrack?.nativeId && nativeTrack.id === targetTrack.nativeId);
          const matchesEngineIndex = targetTrack?.engineIndex !== undefined ? i === targetTrack.engineIndex : false;
          const fallbackMatchesNumber = typeof trackIdentifier === 'number' && i === trackIdentifier && !targetTrack?.nativeId;
          nativeTrack.enabled = Boolean(matchesNativeId || matchesEngineIndex || fallbackMatchesNumber);
        }
      }
    }
    const nextActiveId = targetTrack ? targetTrack.id : (typeof trackIdentifier === 'number' ? trackIdentifier : -1);
    setActiveAudioTrack(nextActiveId);
  }, [audioTracks, hlsRef, videoRef]);

  const resetAudioTracks = useCallback(() => {
    setAudioTracks([]);
    setActiveAudioTrack(-1);
  }, []);

  return {
    audioTracks,
    activeAudioTrack,
    handleAudioTracksUpdated,
    setActiveAudioTrack,
    changeAudioTrack,
    resetAudioTracks,
  };
}
