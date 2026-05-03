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
}

export type PlaybackNormativeEvent =
  | {
      type: 'system.requested_duration.synced';
      durationSeconds: number | null;
    }
  | {
      type: 'normative.playback.attempt.started';
      epoch: number;
      playbackMode: DomainPlaybackMode;
      status: PlayerStatus;
      requestedDuration: number | null;
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
