import { useCallback } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { AppError } from '../../../types/errors';
import type { PlayerStatus } from '../../../types/v3-player';
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';
import { buildPlaybackFailure } from './playbackMachine';
import {
  buildPlaybackAdvisorySignal,
  type PlaybackFailureReportOptions,
} from '../semantics/playbackFailureSemantics';

interface UsePlaybackStateSettersArgs {
  dispatchPlayback: Dispatch<PlaybackMachineEvent>;
  playbackStateRef: MutableRefObject<PlaybackDomainState>;
  acceptedPlaybackEpochRef: MutableRefObject<number>;
  setShowErrorDetails: Dispatch<SetStateAction<boolean>>;
}

export interface PlaybackStateSetters {
  setTraceId: Dispatch<SetStateAction<string>>;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  setPlaybackMode: Dispatch<SetStateAction<'LIVE' | 'VOD' | 'UNKNOWN'>>;
  setDurationSeconds: Dispatch<SetStateAction<number | null>>;
  setVodStreamMode: Dispatch<SetStateAction<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>>;
  setActiveHlsEngine: Dispatch<SetStateAction<'native' | 'hlsjs' | null>>;
  setCanSeek: Dispatch<SetStateAction<boolean>>;
  setStartUnix: Dispatch<SetStateAction<number | null>>;
  setPlayerError: (
    nextError: AppError | null,
    options?: PlaybackFailureReportOptions & {
      messageKey?: string | null;
      playerStatus?: PlayerStatus;
    },
  ) => void;
  reportPlaybackFailure: (
    nextError: AppError,
    options?: PlaybackFailureReportOptions,
  ) => void;
  clearPlaybackFailure: () => void;
  clearPlayerError: () => void;
  recordContractAdvisories: (
    epoch: number,
    warnings: Parameters<typeof buildPlaybackAdvisorySignal>[0][],
  ) => void;
}

export function usePlaybackStateSetters({
  dispatchPlayback,
  playbackStateRef,
  acceptedPlaybackEpochRef,
  setShowErrorDetails,
}: UsePlaybackStateSettersArgs): PlaybackStateSetters {
  const setTraceId = useCallback<Dispatch<SetStateAction<string>>>((next) => {
    const currentTraceId = playbackStateRef.current.traceId;
    const resolvedTraceId = typeof next === 'function' ? next(currentTraceId) : next;
    dispatchPlayback({
      type: 'normative.playback.trace.updated',
      epoch: acceptedPlaybackEpochRef.current,
      traceId: resolvedTraceId,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setStatus = useCallback<Dispatch<SetStateAction<PlayerStatus>>>((next) => {
    const currentStatus = playbackStateRef.current.status;
    const resolvedStatus = typeof next === 'function' ? next(currentStatus) : next;
    dispatchPlayback({
      type: 'normative.media.status.changed',
      epoch: acceptedPlaybackEpochRef.current,
      status: resolvedStatus,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setPlaybackMode = useCallback<Dispatch<SetStateAction<'LIVE' | 'VOD' | 'UNKNOWN'>>>((next) => {
    const currentMode = playbackStateRef.current.playbackMode;
    const resolvedMode = typeof next === 'function' ? next(currentMode) : next;
    dispatchPlayback({
      type: 'normative.playback.mode.changed',
      epoch: acceptedPlaybackEpochRef.current,
      playbackMode: resolvedMode,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setDurationSeconds = useCallback<Dispatch<SetStateAction<number | null>>>((next) => {
    const currentDurationSeconds = playbackStateRef.current.durationSeconds;
    const resolvedDurationSeconds = typeof next === 'function' ? next(currentDurationSeconds) : next;
    dispatchPlayback({
      type: 'normative.playback.duration.changed',
      epoch: acceptedPlaybackEpochRef.current,
      durationSeconds: resolvedDurationSeconds,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setVodStreamMode = useCallback<Dispatch<SetStateAction<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>>>((next) => {
    const currentMode = playbackStateRef.current.vodStreamMode;
    const resolvedMode = typeof next === 'function' ? next(currentMode) : next;
    dispatchPlayback({
      type: 'normative.playback.vod_mode.changed',
      epoch: acceptedPlaybackEpochRef.current,
      vodStreamMode: resolvedMode,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setActiveHlsEngine = useCallback<Dispatch<SetStateAction<'native' | 'hlsjs' | null>>>((next) => {
    const currentEngine = playbackStateRef.current.activeHlsEngine;
    const resolvedEngine = typeof next === 'function' ? next(currentEngine) : next;
    dispatchPlayback({
      type: 'normative.media.engine.selected',
      epoch: acceptedPlaybackEpochRef.current,
      engine: resolvedEngine,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setCanSeek = useCallback<Dispatch<SetStateAction<boolean>>>((next) => {
    const currentCanSeek = playbackStateRef.current.canSeek;
    const resolvedCanSeek = typeof next === 'function' ? next(currentCanSeek) : next;
    dispatchPlayback({
      type: 'normative.playback.seekability.changed',
      epoch: acceptedPlaybackEpochRef.current,
      canSeek: resolvedCanSeek,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setStartUnix = useCallback<Dispatch<SetStateAction<number | null>>>((next) => {
    const currentStartUnix = playbackStateRef.current.startUnix;
    const resolvedStartUnix = typeof next === 'function' ? next(currentStartUnix) : next;
    dispatchPlayback({
      type: 'normative.playback.start_unix.changed',
      epoch: acceptedPlaybackEpochRef.current,
      startUnix: resolvedStartUnix,
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback, playbackStateRef]);

  const setPlayerError = useCallback((
    nextError: AppError | null,
    options: PlaybackFailureReportOptions & {
      messageKey?: string | null;
      playerStatus?: PlayerStatus;
    } = {},
  ) => {
    if (!nextError) {
      dispatchPlayback({ type: 'normative.playback.failure.cleared' });
      return;
    }
    dispatchPlayback({
      type: 'normative.playback.failure.raised',
      epoch: acceptedPlaybackEpochRef.current,
      status: options.playerStatus,
      failure: buildPlaybackFailure(nextError, options.source ?? 'orchestrator', {
        class: options.failureClass,
        code: options.code ?? nextError.code ?? undefined,
        message: nextError.title,
        terminal: options.terminal,
        retryable: options.retryable,
        recoverable: options.recoverable,
        userVisible: options.userVisible,
        policyImpact: options.policyImpact,
        messageKey: options.messageKey,
        telemetryContext: options.telemetryContext,
        telemetryReason: options.telemetryReason,
      }),
    });
  }, [acceptedPlaybackEpochRef, dispatchPlayback]);

  const reportPlaybackFailure = useCallback((
    nextError: AppError,
    options: PlaybackFailureReportOptions = {},
  ) => {
    setShowErrorDetails(false);
    setPlayerError(nextError, options);
  }, [setPlayerError, setShowErrorDetails]);

  const clearPlaybackFailure = useCallback(() => {
    dispatchPlayback({ type: 'normative.playback.failure.cleared' });
    setShowErrorDetails(false);
  }, [dispatchPlayback, setShowErrorDetails]);

  const clearPlayerError = useCallback(() => {
    clearPlaybackFailure();
  }, [clearPlaybackFailure]);

  const recordContractAdvisories = useCallback((
    epoch: number,
    warnings: Parameters<typeof buildPlaybackAdvisorySignal>[0][],
  ) => {
    warnings.forEach((warning) => {
      dispatchPlayback({
        type: 'advisory.signal.recorded',
        epoch,
        advisory: buildPlaybackAdvisorySignal(warning),
      });
    });
  }, [dispatchPlayback]);

  return {
    setTraceId,
    setStatus,
    setPlaybackMode,
    setDurationSeconds,
    setVodStreamMode,
    setActiveHlsEngine,
    setCanSeek,
    setStartUnix,
    setPlayerError,
    reportPlaybackFailure,
    clearPlaybackFailure,
    clearPlayerError,
    recordContractAdvisories,
  };
}
