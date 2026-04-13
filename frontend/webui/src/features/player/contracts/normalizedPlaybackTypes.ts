import type {
  PlaybackTargetProfile,
  PlaybackTraceOperator,
  ResumeSummary,
} from '../../../client-ts';

export type NormalizedPlaybackMode = 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode';

export type NormalizedContractFailureKind =
  | 'contract'
  | 'auth'
  | 'session'
  | 'unavailable'
  | 'unsupported';

export type NormalizedContractFailure = {
  kind: NormalizedContractFailureKind;
  code: string;
  message: string;
  retryable: boolean;
  terminal: boolean;
};

export type NormalizedAdvisoryWarning = {
  code: string;
  message: string;
  source: 'backend' | 'normalizer';
};

export type NormalizedPlaybackDecisionObservability = {
  requestProfile: string | null;
  requestedIntent: string | null;
  resolvedIntent: string | null;
  qualityRung: string | null;
  audioQualityRung: string | null;
  videoQualityRung: string | null;
  degradedFrom: string | null;
  hostPressureBand: string | null;
  hostOverrideApplied: boolean;
  targetProfileHash: string | null;
  targetProfile: PlaybackTargetProfile | null;
  operator: PlaybackTraceOperator | null;
  selectedOutputKind: 'file' | 'hls' | null;
};

export type NormalizedPlaybackContractObservability = {
  requestId: string | null;
  backendReason: string | null;
  decision: NormalizedPlaybackDecisionObservability | null;
};

export type NormalizedPlayablePlaybackContract = {
  kind: 'playable';
  playback: {
    mode: NormalizedPlaybackMode;
    // Live decisions may be session-backed and therefore expose the URL only
    // after intent admission; VOD contracts must always supply it.
    outputUrl: string | null;
    seekable: boolean;
    live: boolean;
    autoplayAllowed: boolean;
  };
  session: {
    required: boolean;
    sessionId: string | null;
    expiresAt: string | null;
    decisionToken: string | null;
  };
  media: {
    mimeType: string | null;
    durationSeconds: number | null;
    startUnix: number | null;
    liveEdgeUnix: number | null;
  };
  resume: ResumeSummary | null;
  observability: NormalizedPlaybackContractObservability;
  advisory: {
    warnings: NormalizedAdvisoryWarning[];
  };
};

export type NormalizedBlockedPlaybackContract = {
  kind: 'blocked';
  failure: NormalizedContractFailure;
  observability: NormalizedPlaybackContractObservability;
  advisory: {
    warnings: NormalizedAdvisoryWarning[];
  };
};

export type NormalizedPlaybackContract =
  | NormalizedPlayablePlaybackContract
  | NormalizedBlockedPlaybackContract;

export type NormalizePlaybackInfoContext = {
  surface: 'recording' | 'live';
  preferredHlsEngine: 'native' | 'hlsjs';
};
