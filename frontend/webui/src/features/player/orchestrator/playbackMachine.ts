import type { AppError } from '../../../types/errors';
import type { PlayerStatus } from '../../../types/v3-player';
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
  PlaybackFailure,
  PlaybackFailureSource,
  MediaPhase,
} from './playbackTypes';
import { classifyPlaybackFailure } from '../semantics/playbackFailureSemantics';

function statusToMediaPhase(status: PlayerStatus): MediaPhase {
  switch (status) {
    case 'starting':
    case 'priming':
    case 'building':
      return 'starting';
    case 'ready':
    case 'playing':
      return 'playing';
    case 'buffering':
      return 'buffering';
    case 'paused':
      return 'paused';
    case 'recovering':
      return 'recovering';
    case 'stopped':
      return 'stopped';
    case 'error':
      return 'error';
    case 'idle':
    default:
      return 'idle';
  }
}

export function buildPlaybackFailure(
  error: AppError | null,
  source: PlaybackFailureSource,
  overrides: Partial<Omit<PlaybackFailure, 'appError' | 'source' | 'status' | 'telemetryContext' | 'telemetryReason'>> & {
    telemetryContext?: string | null;
    telemetryReason?: string | null;
  } = {},
): PlaybackFailure {
  const semantics = classifyPlaybackFailure({
    appError: error,
    source,
    failureClass: overrides.class,
    code: overrides.code,
    message: overrides.message,
    retryable: overrides.retryable,
    recoverable: overrides.recoverable,
    terminal: overrides.terminal,
    userVisible: overrides.userVisible,
    policyImpact: overrides.policyImpact,
  });

  return {
    class: semantics.class,
    code: semantics.code,
    message: semantics.message,
    terminal: semantics.terminal,
    retryable: semantics.retryable,
    recoverable: semantics.recoverable,
    userVisible: semantics.userVisible,
    policyImpact: semantics.policyImpact,
    source,
    messageKey: overrides.messageKey ?? null,
    appError: error,
    status: error?.status ?? null,
    telemetryContext: overrides.telemetryContext ?? null,
    telemetryReason: overrides.telemetryReason ?? null,
  };
}

export function createInitialPlaybackDomainState(requestedDuration: number | null = null): PlaybackDomainState {
  return {
    epoch: {
      playback: 0,
      session: 0,
    },
    traceId: '-',
    status: 'idle',
    playbackMode: 'UNKNOWN',
    vodStreamMode: null,
    activeHlsEngine: null,
    durationSeconds: requestedDuration,
    canSeek: true,
    startUnix: null,
    sessionPhase: 'idle',
    mediaPhase: 'idle',
    contract: null,
    failure: null,
    lastAdvisory: null,
  };
}

function isCurrentPlaybackEpoch(state: PlaybackDomainState, epoch: number): boolean {
  return epoch === state.epoch.playback;
}

function isCurrentSessionEpoch(state: PlaybackDomainState, playbackEpoch: number, sessionEpoch: number): boolean {
  return playbackEpoch === state.epoch.playback && sessionEpoch === state.epoch.session;
}

export function playbackMachine(state: PlaybackDomainState, event: PlaybackMachineEvent): PlaybackDomainState {
  switch (event.type) {
    case 'system.requested_duration.synced':
      return {
        ...state,
        durationSeconds: event.durationSeconds,
      };

    case 'normative.playback.attempt.started':
      if (event.epoch < state.epoch.playback) {
        return state;
      }
      return {
        ...state,
        epoch: {
          playback: event.epoch,
          session: 0,
        },
        traceId: '-',
        status: event.status,
        playbackMode: event.playbackMode,
        vodStreamMode: null,
        activeHlsEngine: null,
        durationSeconds: event.requestedDuration,
        canSeek: true,
        startUnix: null,
        sessionPhase: event.playbackMode === 'LIVE' ? 'starting' : 'idle',
        mediaPhase: statusToMediaPhase(event.status),
        contract: null,
        failure: null,
        lastAdvisory: null,
      };

    case 'normative.playback.stopped':
      if (event.epoch < state.epoch.playback) {
        return state;
      }
      return {
        ...state,
        epoch: {
          playback: event.epoch,
          session: 0,
        },
        traceId: '-',
        status: 'stopped',
        playbackMode: 'UNKNOWN',
        vodStreamMode: null,
        activeHlsEngine: null,
        sessionPhase: 'stopped',
        mediaPhase: 'stopped',
        contract: null,
      };

    case 'normative.playback.mode.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        playbackMode: event.playbackMode,
      };

    case 'normative.playback.duration.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        durationSeconds: event.durationSeconds,
      };

    case 'normative.playback.trace.updated':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        traceId: event.traceId,
      };

    case 'normative.playback.seekability.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        canSeek: event.canSeek,
      };

    case 'normative.playback.start_unix.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        startUnix: event.startUnix,
      };

    case 'normative.playback.vod_mode.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        vodStreamMode: event.vodStreamMode,
      };

    case 'normative.playback.contract.resolved':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        traceId: event.contract.requestId ?? state.traceId,
        contract: event.contract,
        vodStreamMode: event.contract.kind === 'recording'
          ? event.contract.mode
          : state.vodStreamMode,
        durationSeconds: event.contract.durationSeconds ?? state.durationSeconds,
        canSeek: event.contract.canSeek,
        startUnix: event.contract.startUnix,
        failure: null,
      };

    case 'normative.playback.failure.raised':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        status: event.status ?? 'error',
        mediaPhase: statusToMediaPhase(event.status ?? 'error'),
        sessionPhase: event.failure.class === 'auth' || event.failure.class === 'session'
          ? 'error'
          : state.sessionPhase,
        failure: event.failure,
      };

    case 'normative.playback.failure.cleared':
      return {
        ...state,
        failure: null,
      };

    case 'normative.media.status.changed':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        status: event.status,
        mediaPhase: statusToMediaPhase(event.status),
      };

    case 'normative.media.engine.selected':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        activeHlsEngine: event.engine,
      };

    case 'normative.session.attempt.started':
      if (event.playbackEpoch !== state.epoch.playback || event.sessionEpoch < state.epoch.session) {
        return state;
      }
      return {
        ...state,
        epoch: {
          ...state.epoch,
          session: event.sessionEpoch,
        },
        sessionPhase: 'starting',
      };

    case 'normative.session.phase.changed':
      if (!isCurrentSessionEpoch(state, event.playbackEpoch, event.sessionEpoch)) {
        return state;
      }
      return {
        ...state,
        traceId: event.requestId ?? state.traceId,
        sessionPhase: event.phase,
      };

    case 'advisory.signal.recorded':
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      return {
        ...state,
        lastAdvisory: event.advisory,
      };

    default:
      return state;
  }
}
