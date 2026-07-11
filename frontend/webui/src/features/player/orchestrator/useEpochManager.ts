import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject } from 'react';
import type {
  PlaybackEpochState,
  PlaybackMachineEvent,
} from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';

interface UseEpochManagerArgs {
  initialEpoch: PlaybackEpochState;
  trackedEpoch: PlaybackEpochState;
  dispatchPlayback: Dispatch<PlaybackMachineEvent>;
  requestedDuration: number | null;
  onAttemptStarted?: () => void;
}

export interface EpochManager {
  playbackEpochRef: MutableRefObject<number>;
  sessionEpochRef: MutableRefObject<number>;
  acceptedPlaybackEpochRef: MutableRefObject<number>;
  acceptedSessionEpochRef: MutableRefObject<number>;
  allocatePlaybackEpoch: () => number;
  beginPlaybackAttempt: (
    epoch: number,
    nextPlaybackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
    nextStatus: PlayerStatus,
  ) => void;
  markPlaybackStopped: (epoch: number) => void;
  allocateSessionEpoch: (playbackEpoch: number) => number;
  isStalePlaybackEpoch: (epoch: number) => boolean;
  isStaleSessionEpoch: (playbackEpoch: number, sessionEpoch: number) => boolean;
}

export function useEpochManager({
  initialEpoch,
  trackedEpoch,
  dispatchPlayback,
  requestedDuration,
  onAttemptStarted,
}: UseEpochManagerArgs): EpochManager {
  const playbackEpochRef = useRef(initialEpoch.playback);
  const sessionEpochRef = useRef(initialEpoch.session);
  const acceptedPlaybackEpochRef = useRef(initialEpoch.playback);
  const acceptedSessionEpochRef = useRef(initialEpoch.session);

  useEffect(() => {
    acceptedPlaybackEpochRef.current = trackedEpoch.playback;
    acceptedSessionEpochRef.current = trackedEpoch.session;
  }, [trackedEpoch.playback, trackedEpoch.session]);

  const allocatePlaybackEpoch = useCallback(() => {
    playbackEpochRef.current += 1;
    sessionEpochRef.current = 0;
    return playbackEpochRef.current;
  }, []);

  const beginPlaybackAttempt = useCallback((
    epoch: number,
    nextPlaybackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
    nextStatus: PlayerStatus,
  ) => {
    onAttemptStarted?.();
    acceptedPlaybackEpochRef.current = epoch;
    acceptedSessionEpochRef.current = 0;
    dispatchPlayback({
      type: 'normative.playback.attempt.started',
      epoch,
      playbackMode: nextPlaybackMode,
      status: nextStatus,
      requestedDuration,
    });
  }, [dispatchPlayback, onAttemptStarted, requestedDuration]);

  const markPlaybackStopped = useCallback((epoch: number) => {
    acceptedPlaybackEpochRef.current = epoch;
    acceptedSessionEpochRef.current = 0;
    dispatchPlayback({
      type: 'normative.playback.stopped',
      epoch,
    });
  }, [dispatchPlayback]);

  const allocateSessionEpoch = useCallback((playbackEpoch: number) => {
    sessionEpochRef.current += 1;
    const sessionEpoch = sessionEpochRef.current;
    acceptedSessionEpochRef.current = sessionEpoch;
    dispatchPlayback({
      type: 'normative.session.attempt.started',
      playbackEpoch,
      sessionEpoch,
    });
    return sessionEpoch;
  }, [dispatchPlayback]);

  const isStalePlaybackEpoch = useCallback((epoch: number) => (
    epoch !== playbackEpochRef.current
  ), []);

  const isStaleSessionEpoch = useCallback((playbackEpoch: number, sessionEpoch: number) => (
    playbackEpoch !== playbackEpochRef.current || sessionEpoch !== sessionEpochRef.current
  ), []);

  return {
    playbackEpochRef,
    sessionEpochRef,
    acceptedPlaybackEpochRef,
    acceptedSessionEpochRef,
    allocatePlaybackEpoch,
    beginPlaybackAttempt,
    markPlaybackStopped,
    allocateSessionEpoch,
    isStalePlaybackEpoch,
    isStaleSessionEpoch,
  };
}
