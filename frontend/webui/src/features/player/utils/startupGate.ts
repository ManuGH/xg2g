/**
 * Startup gate policy.
 *
 * Live HLS hands the player roughly one segment of headroom, and calling play()
 * the moment the first segment lands leaves none: any PDT or encoder jitter
 * then surfaces as an immediate bufferStalledError — a visible stall and
 * recovery jolt seconds into playback. Live input is realtime-paced, so
 * headroom can only be bought by waiting.
 *
 * Waiting therefore needs a cap, and the cap has to be unconditional. A gate
 * that re-checks the buffer target on the timeout path is not a cap at all: it
 * spins forever whenever the buffer never reaches the target, which is exactly
 * the situation the cap exists for (stalled encoder, corrupt receiver input,
 * segments that stop arriving). The user sees an endless spinner instead of
 * either a picture or an error.
 *
 * This lives apart from usePlaybackEngine so the policy can be tested directly.
 * It is deliberately pure: no timers, no video element, no hls.js.
 */

export type StartGateTrigger = 'buffer_target' | 'timeout' | 'vod' | 'retry';

export type StartGateInput = {
  /**
   * Whether the loaded playlist is live. Until a playlist says otherwise the
   * caller should assume live: holding a VOD start briefly is harmless, while
   * starting a live stream with no headroom is the stall this gate prevents.
   */
  isLive: boolean;
  /** Seconds of media buffered ahead of the playhead. */
  bufferedAheadSeconds: number;
  /** Whether the startup cap has elapsed. Once true the gate must open. */
  capElapsed: boolean;
  /** Headroom target in seconds for live playlists. */
  targetSeconds: number;
};

/**
 * Decides whether playback may start. The order of these rules is the contract:
 * VOD never waits, the cap always wins, and only then does the buffer target
 * apply.
 */
export function shouldOpenStartGate(input: StartGateInput): boolean {
  // VOD is not realtime-paced — the media already exists server-side, so the
  // player can refill instantly and needs no headroom bought by waiting.
  if (!input.isLive) {
    return true;
  }

  // The cap is a cap. Starting with a thin buffer may stutter; never starting
  // shows a spinner forever, which is strictly worse.
  if (input.capElapsed) {
    return true;
  }

  return input.bufferedAheadSeconds >= input.targetSeconds;
}
