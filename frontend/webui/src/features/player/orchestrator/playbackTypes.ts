import type { AppError } from '../../../types/errors';
import type { PlayerStatus } from '../../../types/v3-player';
import type { NormalizedPlaybackMode } from '../contracts/normalizedPlaybackTypes';
import type {
  PlaybackAdvisorySignal,
  PlaybackFailureClass,
  PlaybackFailureSemantics,
  PlaybackFailureSource,
} from '../semantics/playbackFailureSemantics';
export type { PlaybackFailureClass, PlaybackFailureSource } from '../semantics/playbackFailureSemantics';
import type { PlaybackRequestProfile } from '../utils/playbackRequestProfile';
export type { PlaybackRequestProfile } from '../utils/playbackRequestProfile';

export type DomainPlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';
export type ContractPlaybackMode = NormalizedPlaybackMode;
export type VodStreamMode = ContractPlaybackMode | null;
export type ActiveHlsEngine = 'native' | 'hlsjs' | null;
export type SessionPhase = 'idle' | 'starting' | 'ready' | 'stopped' | 'error';
export type MediaPhase = 'idle' | 'starting' | 'buffering' | 'playing' | 'paused' | 'recovering' | 'stopped' | 'error';

export interface PlaybackEpochState {
  playback: number;
  session: number;
}

export interface PlaybackContractState {
  kind: 'live' | 'recording';
  requestId: string | null;
  mode: ContractPlaybackMode;
  streamUrl: string | null;
  canSeek: boolean;
  live: boolean;
  autoplayAllowed: boolean;
  sessionRequired: boolean;
  sessionId: string | null;
  expiresAt: string | null;
  decisionToken: string | null;
  durationSeconds: number | null;
  startUnix: number | null;
  mimeType: string | null;
}

export interface PlaybackFailure {
  class: PlaybackFailureClass;
  code: string;
  message: string;
  terminal: boolean;
  retryable: boolean;
  recoverable: boolean;
  userVisible: boolean;
  policyImpact: PlaybackFailureSemantics['policyImpact'];
  source: PlaybackFailureSource;
  messageKey: string | null;
  appError: AppError | null;
  status: number | null;
  telemetryContext: string | null;
  telemetryReason: string | null;
}

import type { RecoveryLadderState } from './recoveryLadder';
export type PlaybackRecoveryState = RecoveryLadderState;

export interface PlaybackDomainState {
  epoch: PlaybackEpochState;
  traceId: string;
  status: PlayerStatus;
  playbackMode: DomainPlaybackMode;
  vodStreamMode: VodStreamMode;
  activeHlsEngine: ActiveHlsEngine;
  durationSeconds: number | null;
  canSeek: boolean;
  startUnix: number | null;
  sessionPhase: SessionPhase;
  mediaPhase: MediaPhase;
  contract: PlaybackContractState | null;
  failure: PlaybackFailure | null;
  lastAdvisory: PlaybackAdvisorySignal | null;
  /** User pinned an explicit profile in the UI — blocks automatic profile fallbacks. */
  explicitProfilePinned: boolean;
  /** Whether the orchestrator is running an active backend intent (channel/recording) vs static src. */
  hasSessionIntent: boolean;
  recovery: PlaybackRecoveryState;
}

export type PlaybackStopReason = 'user_stop' | 'auto_recovery_restart';

export type PlaybackNormativeEvent =
  | {
      type: 'system.requested_duration.synced';
      durationSeconds: number | null;
    }
  | {
      /**
       * User/orchestrator intent to stop playback. Pure pass-through on state
       * (the stopped transition arrives via 'normative.playback.stopped' once
       * teardown completed); the machine answers with the stop command chain.
       */
      type: 'intent.stop.requested';
      epoch: number;
      reason: PlaybackStopReason;
      notifyClose: boolean;
    }
  | {
      type: 'intent.start.requested';
      epoch: number;
      kind: 'live' | 'vod' | 'src';
      serviceRef?: string;
      recordingId?: string;
      srcUrl?: string;
      explicitProfile?: PlaybackRequestProfile | 'auto' | string;
    }
  | {
      type: 'normative.playback.attempt.started';
      epoch: number;
      playbackMode: DomainPlaybackMode;
      status: PlayerStatus;
      requestedDuration: number | null;
      explicitProfilePinned?: boolean;
      hasSessionIntent?: boolean;
    }
  | {
      type: 'normative.playback.stopped';
      epoch: number;
    }
  | {
      type: 'normative.playback.mode.changed';
      epoch: number;
      playbackMode: DomainPlaybackMode;
    }
  | {
      type: 'normative.playback.duration.changed';
      epoch: number;
      durationSeconds: number | null;
    }
  | {
      type: 'normative.playback.trace.updated';
      epoch: number;
      traceId: string;
    }
  | {
      type: 'normative.playback.seekability.changed';
      epoch: number;
      canSeek: boolean;
    }
  | {
      type: 'normative.playback.start_unix.changed';
      epoch: number;
      startUnix: number | null;
    }
  | {
      type: 'normative.playback.vod_mode.changed';
      epoch: number;
      vodStreamMode: VodStreamMode;
    }
  | {
      type: 'normative.playback.contract.resolved';
      epoch: number;
      contract: PlaybackContractState;
    }
  | {
      type: 'normative.playback.failure.raised';
      epoch: number;
      failure: PlaybackFailure;
      status?: PlayerStatus;
      explicitProfilePinned?: boolean;
    }
  | {
      type: 'normative.playback.failure.cleared';
    }
  | {
      type: 'normative.media.status.changed';
      epoch: number;
      status: PlayerStatus;
    }
  | {
      type: 'normative.media.engine.selected';
      epoch: number;
      engine: ActiveHlsEngine;
    }
  | {
      type: 'normative.session.attempt.started';
      playbackEpoch: number;
      sessionEpoch: number;
    }
  | {
      type: 'normative.session.phase.changed';
      playbackEpoch: number;
      sessionEpoch: number;
      phase: SessionPhase;
      requestId?: string | null;
    };

export type PlaybackAdvisoryEvent = {
  type: 'advisory.signal.recorded';
  epoch: number;
  advisory: PlaybackAdvisorySignal;
};

export type PlaybackMachineEvent = PlaybackNormativeEvent | PlaybackAdvisoryEvent;

// --- Command-driven machine (Phase 1 of the orchestrator refactor) ---
//
// The machine does not execute side effects; it RETURNS them as declarative
// commands. The runtime (usePlaybackMachineRuntime) executes each command
// exactly once per dispatched event, outside the React render — immune to
// StrictMode double-invocation. The union grows flow by flow in Phase 2
// (teardown → recovery ladder → startup).

export type PlaybackCommand =
  | {
      /** Append an entry to the client session timeline. */
      type: 'command.timeline.record';
      kind: string;
      detail?: string;
    }
  | {
      /** Execute the asynchronous startup/initiation flow for live, VOD, or direct source playback. */
      type: 'command.playback.start';
      epoch: number;
      kind: 'live' | 'vod' | 'src';
      serviceRef?: string;
      recordingId?: string;
      srcUrl?: string;
      explicitProfile?: PlaybackRequestProfile | 'auto' | string;
    }
  | {
      /** Close the current timeline attempt (subsequent records are dropped). */
      type: 'command.timeline.end_attempt';
      reason: PlaybackStopReason;
    }
  | {
      /** Ship the timeline snapshot to the session's server log. */
      type: 'command.timeline.report';
      reason: PlaybackStopReason;
    }
  | {
      /**
       * Tear down engine + session asynchronously. On completion the executor
       * dispatches 'normative.playback.stopped' with this epoch and, when
       * notifyClose is set, invokes the host's onClose — same ordering as the
       * previous imperative stopStream.
       */
      type: 'command.playback.stop';
      epoch: number;
      reason: PlaybackStopReason;
      notifyClose: boolean;
    }
  | {
      /** Emit an analytics/telemetry event to the backend telemetry pipe. */
      type: 'command.telemetry.emit';
      eventName: string;
      payload: Record<string, unknown>;
    }
  | {
      /**
       * Schedule an automatic profile fallback retry after a transient media failure.
       * Executed once per viewing intent via the recovery ladder.
       */
      type: 'command.playback.schedule_auto_fallback';
      epoch: number;
      delayMs: number;
      profile: string;
      failureCode: string;
      failureClass: string;
    };

export interface PlaybackMachineResult {
  state: PlaybackDomainState;
  commands: PlaybackCommand[];
}
