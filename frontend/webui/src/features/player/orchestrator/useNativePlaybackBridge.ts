import { useCallback, useEffect, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import {
  getNativePlaybackState,
  onNativePlaybackState,
  startNativePlayback,
} from '../../../lib/hostBridge';
import type {
  NativePlaybackRequest,
  NativePlaybackState as HostNativePlaybackState,
} from '../../../lib/hostBridge';
import type { PlaybackTrace as PlaybackTraceContract } from '../../../client-ts';
import type { AppError } from '../../../types/errors';
import {
  NATIVE_PLAYER_STATE_IDLE,
  resolveNativePlaybackStatus,
} from './nativePlaybackHelpers';
import {
  extractPlaybackTrace,
  resolvePlaybackObservability,
  type PlaybackObservability,
} from './observabilityFormatters';
import { normalizePlaybackInfo } from '../contracts/normalizePlaybackInfo';
import type { PlaybackStateSetters } from './usePlaybackStateSetters';

export interface NativePlaybackPipeline {
  setActiveHlsEngine: PlaybackStateSetters['setActiveHlsEngine'];
  setActiveRecordingId: Dispatch<SetStateAction<string | null>>;
  setPlaybackMode: PlaybackStateSetters['setPlaybackMode'];
  setStatus: PlaybackStateSetters['setStatus'];
  setTraceId: PlaybackStateSetters['setTraceId'];
  setSessionProfileReason: Dispatch<SetStateAction<string | null>>;
  setPlaybackObservability: Dispatch<SetStateAction<PlaybackObservability | null>>;
  mergeSessionPlaybackTrace: (next: PlaybackTraceContract | null) => void;
  clearPlayerError: PlaybackStateSetters['clearPlayerError'];
  reportPlaybackFailure: PlaybackStateSetters['reportPlaybackFailure'];
}

interface UseNativePlaybackBridgeArgs {
  isNativePlaybackHost: boolean;
  resolvePreferredHlsEngine: () => 'native' | 'hlsjs';
  pipeline: NativePlaybackPipeline;
  activeRecordingRef: MutableRefObject<string | null>;
}

export interface NativePlaybackBridge {
  nativePlaybackState: HostNativePlaybackState | null;
  nativeSessionId: string | null;
  beginNativePlayback: (request: NativePlaybackRequest) => void;
  syncNativePlaybackState: (state: HostNativePlaybackState | null) => void;
  resetBridgeState: () => void;
}

export function useNativePlaybackBridge({
  isNativePlaybackHost,
  resolvePreferredHlsEngine,
  pipeline,
  activeRecordingRef,
}: UseNativePlaybackBridgeArgs): NativePlaybackBridge {
  const [nativePlaybackState, setNativePlaybackState] = useState<HostNativePlaybackState | null>(null);
  const [nativeSessionId, setNativeSessionId] = useState<string | null>(null);
  const nativePlaybackWasActiveRef = useRef(false);

  const {
    setActiveHlsEngine,
    setActiveRecordingId,
    setPlaybackMode,
    setStatus,
    setTraceId,
    setSessionProfileReason,
    setPlaybackObservability,
    mergeSessionPlaybackTrace,
    clearPlayerError,
    reportPlaybackFailure,
  } = pipeline;

  const beginNativePlayback = useCallback((request: NativePlaybackRequest): void => {
    const started = startNativePlayback(request);
    if (!started) {
      throw new Error('Native playback bridge unavailable');
    }

    nativePlaybackWasActiveRef.current = true;
    setNativePlaybackState({
      activeRequest: request,
      session: null,
      playerState: NATIVE_PLAYER_STATE_IDLE,
      playWhenReady: true,
      isInPip: false,
      lastError: null,
    });
    clearPlayerError();
    setActiveHlsEngine(null);
    if (request.kind === 'recording') {
      activeRecordingRef.current = request.recordingId;
      setActiveRecordingId(request.recordingId);
      setPlaybackMode('VOD');
    } else {
      activeRecordingRef.current = null;
      setActiveRecordingId(null);
      setPlaybackMode('LIVE');
    }
    setStatus('starting');
  }, [
    activeRecordingRef,
    clearPlayerError,
    setActiveHlsEngine,
    setActiveRecordingId,
    setPlaybackMode,
    setStatus,
  ]);

  const syncNativePlaybackState = useCallback((nextState: HostNativePlaybackState | null) => {
    if (!isNativePlaybackHost) {
      return;
    }

    setNativePlaybackState(nextState);

    const hadActiveNativePlayback = nativePlaybackWasActiveRef.current;
    const activeRequest = nextState?.activeRequest ?? null;
    const hasActiveNativePlayback = nextState != null && activeRequest != null;
    nativePlaybackWasActiveRef.current = hasActiveNativePlayback;

    if (!hasActiveNativePlayback) {
      if (hadActiveNativePlayback) {
        activeRecordingRef.current = null;
        setNativeSessionId(null);
        setActiveRecordingId(null);
        setActiveHlsEngine(null);
        setPlaybackMode('UNKNOWN');
        if (nextState?.lastError) {
          reportPlaybackFailure({
            title: nextState.lastError,
            retryable: true,
            code: 'NATIVE_HOST_ERROR',
          } as AppError, {
            source: 'native-host',
            failureClass: 'media',
            retryable: true,
            recoverable: true,
            terminal: false,
          });
          setStatus('error');
        } else {
          setStatus('stopped');
        }
      }
      return;
    }

    const resolvedState = nextState;
    const diagnostics = resolvedState.diagnostics ?? null;
    const nextNativeSessionId =
      resolvedState.session?.sessionId ??
      (typeof diagnostics?.trace?.sessionId === 'string' ? diagnostics.trace.sessionId : null) ??
      (resolvedState.session?.trace && typeof resolvedState.session.trace === 'object' && typeof resolvedState.session.trace.sessionId === 'string'
        ? resolvedState.session.trace.sessionId
        : null);
    setNativeSessionId(nextNativeSessionId);

    const nextTraceId = diagnostics?.requestId ?? resolvedState.session?.requestId ?? null;
    if (nextTraceId) {
      setTraceId(nextTraceId);
    }

    const nextProfileReason = resolvedState.session?.profileReason ?? diagnostics?.profileReason ?? null;
    setSessionProfileReason(nextProfileReason);

    if (diagnostics?.playbackInfo) {
      const diagnosticsContract = normalizePlaybackInfo(diagnostics.playbackInfo, {
        surface: nextNativeSessionId ? 'live' : 'recording',
        preferredHlsEngine: resolvePreferredHlsEngine(),
      });
      setPlaybackObservability(resolvePlaybackObservability(
        diagnosticsContract.observability.decision,
        typeof diagnostics.trace?.clientPath === 'string' ? diagnostics.trace.clientPath : 'android/native',
      ));
    }

    mergeSessionPlaybackTrace(extractPlaybackTrace(diagnostics?.trace));
    mergeSessionPlaybackTrace(extractPlaybackTrace(resolvedState.session?.trace));

    if (resolvedState.lastError) {
      reportPlaybackFailure({
        title: resolvedState.lastError,
        retryable: true,
        code: 'NATIVE_HOST_ERROR',
      } as AppError, {
        source: 'native-host',
        failureClass: 'media',
        retryable: true,
        recoverable: true,
        terminal: false,
      });
    } else {
      clearPlayerError();
    }

    if (activeRequest.kind === 'recording') {
      activeRecordingRef.current = activeRequest.recordingId;
      setActiveRecordingId(activeRequest.recordingId);
      setPlaybackMode('VOD');
    } else {
      activeRecordingRef.current = null;
      setActiveRecordingId(null);
      setPlaybackMode('LIVE');
    }
    setActiveHlsEngine(null);

    const mappedStatus = resolveNativePlaybackStatus(nextState);
    if (mappedStatus) {
      setStatus(mappedStatus);
    }
  }, [
    activeRecordingRef,
    clearPlayerError,
    isNativePlaybackHost,
    mergeSessionPlaybackTrace,
    reportPlaybackFailure,
    resolvePreferredHlsEngine,
    setActiveHlsEngine,
    setActiveRecordingId,
    setPlaybackMode,
    setPlaybackObservability,
    setSessionProfileReason,
    setStatus,
    setTraceId,
  ]);

  useEffect(() => {
    if (!isNativePlaybackHost) {
      setNativePlaybackState(null);
      nativePlaybackWasActiveRef.current = false;
      return;
    }

    syncNativePlaybackState(getNativePlaybackState());
    const unsubscribe = onNativePlaybackState((nextState) => {
      syncNativePlaybackState(nextState);
    });

    return () => {
      unsubscribe();
    };
  }, [isNativePlaybackHost, syncNativePlaybackState]);

  const resetBridgeState = useCallback(() => {
    nativePlaybackWasActiveRef.current = false;
    setNativePlaybackState(null);
    setNativeSessionId(null);
  }, []);

  return {
    nativePlaybackState,
    nativeSessionId,
    beginNativePlayback,
    syncNativePlaybackState,
    resetBridgeState,
  };
}
