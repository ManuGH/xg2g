import type { AppError } from '../../../types/errors';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackAttemptToken } from '../playbackEngineContract';
import type {
  PlaybackCommand,
  PlaybackDomainState,
  PlaybackMachineEvent,
  PlaybackMachineResult,
  PlaybackFailure,
  PlaybackFailureSource,
  MediaPhase,
} from './playbackTypes';
import { classifyPlaybackFailure } from '../semantics/playbackFailureSemantics';
import { createRecoveryLadderState, decideRecoveryEscalation } from './recoveryLadder';

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
    currentAttemptId: null,
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
    explicitProfilePinned: false,
    hasSessionIntent: false,
    recovery: createRecoveryLadderState(),
  };
}

function isCurrentPlaybackEpoch(state: PlaybackDomainState, epoch: number): boolean {
  return epoch === state.epoch.playback;
}

function isCurrentAttempt(state: PlaybackDomainState, attempt: PlaybackAttemptToken): boolean {
  if (attempt.epoch !== state.epoch.playback) {
    return false;
  }
  if (state.currentAttemptId !== null && attempt.attemptId !== state.currentAttemptId) {
    return false;
  }
  return true;
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
        currentAttemptId: event.attemptId ?? `att-${event.epoch}`,
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
        explicitProfilePinned: event.explicitProfilePinned ?? false,
        hasSessionIntent: event.hasSessionIntent ?? false,
        // An attempt that continues an automatic recovery inherits the budget;
        // any other new attempt (user retry, channel change, remount) starts
        // with a fresh one. See RecoveryLadderState.restartPending for why the
        // marker is explicit rather than read off the status.
        recovery: state.recovery.restartPending
          ? { ...state.recovery, restartPending: false }
          : createRecoveryLadderState(),
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
        currentAttemptId: null,
        traceId: '-',
        status: 'stopped',
        playbackMode: 'UNKNOWN',
        vodStreamMode: null,
        activeHlsEngine: null,
        sessionPhase: 'stopped',
        mediaPhase: 'stopped',
        contract: null,
        explicitProfilePinned: false,
        hasSessionIntent: false,
        recovery: createRecoveryLadderState(),
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

    case 'normative.playback.contract.resolved': {
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      const nextStatus = event.contract.kind === 'recording' && state.status === 'building'
        ? 'buffering'
        : state.status;
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
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
    }

    case 'normative.playback.failure.raised': {
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      const resolvedStatus: PlayerStatus = event.status ?? 'error';
      return {
        ...state,
        status: resolvedStatus,
        mediaPhase: statusToMediaPhase(resolvedStatus),
        sessionPhase: event.failure.class === 'auth' || event.failure.class === 'session'
          ? 'error'
          : state.sessionPhase,
        failure: event.failure,
      };
    }

    case 'normative.playback.failure.cleared':
      return {
        ...state,
        failure: null,
      };

    case 'engine.media.ready': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      // Only transition to 'ready' if coming from starting/priming/buffering
      const nextStatus: PlayerStatus =
        state.status === 'starting' || state.status === 'priming' || state.status === 'buffering'
          ? 'ready'
          : state.status;
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
        activeHlsEngine: event.engine === 'direct_mp4' ? null : event.engine,
      };
    }

    case 'engine.media.playing': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      return {
        ...state,
        status: 'playing',
        mediaPhase: 'playing',
        failure: null,
      };
    }

    case 'engine.media.waiting':
    case 'engine.media.stalled': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      // If currently playing or ready, enter buffering. If already starting/priming/buffering, keep current.
      const nextStatus: PlayerStatus =
        state.status === 'playing' || state.status === 'ready' ? 'buffering' : state.status;
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
      };
    }

    case 'engine.media.paused': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      if (state.status === 'stopped' || state.status === 'error' || state.status === 'idle') {
        return state;
      }
      return {
        ...state,
        status: 'paused',
        mediaPhase: 'paused',
      };
    }

    case 'engine.autoplay.blocked': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      // Specification: If blocked while in background, DO NOT degrade to 'ready'. Stay in current ('buffering'/'starting').
      // If foreground, degrade to 'ready' so user can click to play.
      const nextStatus: PlayerStatus = event.background ? state.status : 'ready';
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
      };
    }

    case 'engine.recovery.started': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      return {
        ...state,
        status: 'recovering',
        mediaPhase: 'recovering',
      };
    }

    case 'engine.media.observation': {
      if (!isCurrentAttempt(state, event.attempt)) {
        return state;
      }
      if (
        event.observation === 'playing_confirmed' &&
        (state.status === 'buffering' || state.status === 'priming' || state.status === 'starting' || state.status === 'ready')
      ) {
        return {
          ...state,
          status: 'playing',
          mediaPhase: 'playing',
          failure: null,
        };
      }
      if (
        event.observation === 'canplay' &&
        (state.status === 'buffering' || state.status === 'starting' || state.status === 'priming')
      ) {
        return {
          ...state,
          status: 'ready',
          mediaPhase: statusToMediaPhase('ready'),
        };
      }
      if (
        event.observation === 'stalled_confirmed' &&
        (state.status === 'playing' || state.status === 'ready')
      ) {
        return {
          ...state,
          status: 'buffering',
          mediaPhase: 'buffering',
        };
      }
      return state;
    }

    case 'intent.user_play': {
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      const nextStatus: PlayerStatus =
        state.status === 'paused' || state.status === 'ready' ? 'buffering' : state.status;
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
      };
    }

    case 'intent.user_pause': {
      if (!isCurrentPlaybackEpoch(state, event.epoch)) {
        return state;
      }
      const nextStatus: PlayerStatus =
        state.status === 'playing' || state.status === 'buffering' || state.status === 'ready'
          ? 'paused'
          : state.status;
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
      };
    }

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

    case 'normative.session.phase.changed': {
      if (!isCurrentSessionEpoch(state, event.playbackEpoch, event.sessionEpoch)) {
        return state;
      }
      let nextStatus = state.status;
      if (event.phase === 'priming' && (state.status === 'starting' || state.status === 'buffering')) {
        nextStatus = 'priming';
      } else if (event.phase === 'building' && (state.status === 'starting' || state.status === 'buffering')) {
        nextStatus = 'building';
      } else if (event.phase === 'ready' && (state.status === 'starting' || state.status === 'priming' || state.status === 'building')) {
        nextStatus = 'ready';
      }
      return {
        ...state,
        status: nextStatus,
        mediaPhase: statusToMediaPhase(nextStatus),
        traceId: event.requestId ?? state.traceId,
        sessionPhase: event.phase,
      };
    }

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

export function runPlaybackMachine(
  state: PlaybackDomainState,
  event: PlaybackMachineEvent,
): PlaybackMachineResult {
  const commands: PlaybackCommand[] = [];

  if (event.type === 'intent.stop.requested') {
    if (event.epoch < state.epoch.playback) {
      return { state, commands: [] };
    }
    return {
      state,
      commands: [
        { type: 'command.timeline.end_attempt', reason: event.reason },
        { type: 'command.timeline.report', reason: event.reason },
        { type: 'command.playback.stop', epoch: event.epoch, reason: event.reason, notifyClose: event.notifyClose },
      ],
    };
  }

  if (event.type === 'intent.start.requested') {
    if (event.epoch < state.epoch.playback) {
      return { state, commands: [] };
    }
    return {
      state,
      commands: [
        {
          type: 'command.playback.start',
          epoch: event.epoch,
          kind: event.kind,
          serviceRef: event.serviceRef,
          recordingId: event.recordingId,
          srcUrl: event.srcUrl,
          explicitProfile: event.explicitProfile,
        },
      ],
    };
  }

  const nextState = playbackMachine(state, event);
  if (nextState === state) {
    return { state, commands: [] };
  }

  if (event.type === 'normative.playback.failure.raised') {
    const escalation = decideRecoveryEscalation({
      failure: event.failure,
      explicitProfilePinned: event.explicitProfilePinned ?? state.explicitProfilePinned,
      hasActiveIntent: state.hasSessionIntent,
      // Read from the pre-event state: this is about what the session was doing
      // when it died, not what the failure has just turned it into.
      hasEstablishedSession: state.sessionPhase === 'ready',
      state: state.recovery,
    });

    // Re-establishing a reaped lease waits longer than a profile fallback: the
    // server has just torn the session down, and the dedup lease on the service
    // ref is released as part of that teardown. Starting into it earns a 409.
    const SESSION_RESTART_DELAY_MS = 1500;
    const PROFILE_FALLBACK_DELAY_MS = 250;

    const restart = (
      profile: string | null,
      delayMs: number,
      recovery: typeof state.recovery,
      holdBandwidth = false,
    ): PlaybackMachineResult => ({
      state: {
        ...nextState,
        status: 'recovering',
        mediaPhase: 'recovering',
        failure: null,
        recovery: { ...recovery, restartPending: true },
      },
      commands: [{
        type: 'command.playback.schedule_auto_fallback',
        epoch: event.epoch,
        delayMs,
        profile,
        holdBandwidth,
        failureCode: event.failure.code,
        failureClass: event.failure.class,
      }],
    });

    if (escalation === 'restart_session') {
      return restart(null, SESSION_RESTART_DELAY_MS, {
        ...state.recovery,
        sessionRestarts: state.recovery.sessionRestarts + 1,
      });
    }

    if (escalation === 'restart_on_safe_bandwidth') {
      return restart(null, PROFILE_FALLBACK_DELAY_MS, {
        ...state.recovery,
        autoFallbackUsed: true,
      }, true);
    }

    if (escalation === 'restart_with_fallback_profile') {
      return restart('repair', PROFILE_FALLBACK_DELAY_MS, {
        ...state.recovery,
        autoFallbackUsed: true,
      });
    }
  }

  if (nextState.sessionPhase !== state.sessionPhase) {
    commands.push({
      type: 'command.timeline.record',
      kind: 'session_phase',
      detail: nextState.sessionPhase,
    });
  }

  return { state: nextState, commands };
}
