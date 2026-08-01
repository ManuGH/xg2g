import type { PlaybackRequestProfile } from './playbackRequestProfile';

// How the player should behave given the kind of link it is on.
//
// This is player runtime tuning — hls.js loader settings — not a playback
// decision; the codec and mode remain the backend planner's call.
//
// The hls.js defaults here were tuned for a stable connection: low-latency mode
// chases the live edge, and a 12s sync target plus a 60s buffer keeps the
// picture close to live. On a phone that is the wrong trade in every respect —
// low-latency mode is deliberately intolerant of buffer gaps, and sitting near
// the edge means there is nothing in hand when the link dips. A commuter train
// dips constantly.
//
// Being further behind live costs nothing a mobile viewer would notice and buys
// the one thing that keeps playback alive through a dead spot: buffer.

export type PlaybackLinkProfile = 'stable' | 'constrained';

export interface HlsLinkTuning {
  lowLatencyMode: boolean;
  /** Target distance behind the live edge, in seconds. */
  liveSyncDuration: number;
  /** Forward buffer the loader tries to keep, in seconds. */
  maxBufferLength: number;
  /** Fatal network errors absorbed before the orchestrator ladder is asked. */
  maxNetworkRetries: number;
  /** Ceiling for the exponential retry backoff. */
  networkBackoffCapMs: number;
}

const STABLE_TUNING: HlsLinkTuning = {
  lowLatencyMode: true,
  liveSyncDuration: 12,
  maxBufferLength: 60,
  maxNetworkRetries: 6,
  networkBackoffCapMs: 30_000,
};

// A tighter backoff cap with more attempts covers a longer outage in total
// while still noticing a returning link within seconds rather than half a
// minute — on a train the link comes back abruptly, and a 30s sleep spends most
// of the tunnel exit doing nothing.
const CONSTRAINED_TUNING: HlsLinkTuning = {
  lowLatencyMode: false,
  liveSyncDuration: 30,
  maxBufferLength: 180,
  maxNetworkRetries: 8,
  networkBackoffCapMs: 15_000,
};

export function hlsTuningForLink(link: PlaybackLinkProfile): HlsLinkTuning {
  return link === 'constrained' ? CONSTRAINED_TUNING : STABLE_TUNING;
}

export interface ResolveLinkProfileInput {
  /** The profile the request actually went out with. */
  requestProfile?: PlaybackRequestProfile;
  /** Verdict of the server-side probe, when one was reached. */
  probeKind?: 'lan' | 'measured' | 'constrained';
  network?: {
    kind?: string;
    metered?: boolean;
    saveData?: boolean;
  };
}

export function resolvePlaybackLinkProfile({
  requestProfile,
  probeKind,
  network,
}: ResolveLinkProfileInput): PlaybackLinkProfile {
  if (requestProfile === 'bandwidth') {
    return 'constrained';
  }
  if (probeKind === 'constrained') {
    return 'constrained';
  }
  if (network?.metered || network?.saveData || network?.kind === 'cellular') {
    return 'constrained';
  }
  return 'stable';
}
