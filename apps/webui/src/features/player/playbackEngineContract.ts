import type { AppError } from '../../types/errors';
import type { PlaybackFailureReportOptions } from './semantics/playbackFailureSemantics';

export interface PlaybackAttemptToken {
  readonly epoch: number;
  readonly attemptId: string;
}

export type PlaybackEngineEvent =
  | {
      type: 'media.ready';
      attempt: PlaybackAttemptToken;
      engine: 'native' | 'hlsjs' | 'direct_mp4';
    }
  | {
      type: 'media.playing';
      attempt: PlaybackAttemptToken;
    }
  | {
      type: 'media.waiting';
      attempt: PlaybackAttemptToken;
    }
  | {
      type: 'media.stalled';
      attempt: PlaybackAttemptToken;
    }
  | {
      type: 'media.paused';
      attempt: PlaybackAttemptToken;
    }
  | {
      type: 'autoplay.blocked';
      attempt: PlaybackAttemptToken;
      error: unknown;
      background: boolean;
    }
  | {
      type: 'recovery.started';
      attempt: PlaybackAttemptToken;
      phase: 'decode' | 'network' | 'audio';
    }
  | {
      type: 'playback.failure';
      attempt: PlaybackAttemptToken;
      error: AppError;
      options?: PlaybackFailureReportOptions;
    }
  | {
      type: 'playback.milestone';
      attempt: PlaybackAttemptToken;
      milestone: 'manifest' | 'firstFrame';
    }
  | {
      type: 'media.observation';
      attempt: PlaybackAttemptToken;
      observation: 'canplay' | 'playing_confirmed' | 'stalled_confirmed';
    };

export type PlaybackEngineEventSink = (event: PlaybackEngineEvent) => void;
