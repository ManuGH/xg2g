import type { VideoElementRef } from '../../types/v3-player';
import type { PlaybackStallCounters } from './playbackStallTracker';

export interface PlaybackFrameCounters {
  totalFrames: number | null;
  droppedFrames: number | null;
}

export interface HlsRenderProbeSnapshot extends PlaybackFrameCounters {
  currentTime: number;
  readyState: number;
  networkState: number;
  videoWidth: number;
  videoHeight: number;
  paused: boolean;
  bufferedAhead: number;
  playbackRate: number;
  /** Number of disjoint buffered ranges; more than one means holes ahead or behind. */
  bufferedRanges?: number;
  /** Seconds from the playhead to the end of the last buffered range. */
  bufferedTail?: number;
  /** hls.js distance to the live edge in seconds, when known. */
  liveLatency?: number | null;
  /** Stall and hole counters for the session, cumulative. */
  stalls?: PlaybackStallCounters;
}

export function readPlaybackFrameCounters(videoEl: NonNullable<VideoElementRef>): PlaybackFrameCounters {
  if (typeof videoEl.getVideoPlaybackQuality === 'function') {
    const quality = videoEl.getVideoPlaybackQuality();
    return {
      totalFrames: Number.isFinite(quality.totalVideoFrames) ? quality.totalVideoFrames : null,
      droppedFrames: Number.isFinite(quality.droppedVideoFrames) ? quality.droppedVideoFrames : null,
    };
  }

  interface WebkitVideoElement extends HTMLVideoElement {
    webkitDecodedFrameCount?: number;
    webkitDroppedFrameCount?: number;
  }

  const webkitVideo = videoEl as WebkitVideoElement;
  return {
    totalFrames: typeof webkitVideo.webkitDecodedFrameCount === 'number' ? webkitVideo.webkitDecodedFrameCount : null,
    droppedFrames: typeof webkitVideo.webkitDroppedFrameCount === 'number' ? webkitVideo.webkitDroppedFrameCount : null,
  };
}

function formatProbeFloat(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return 'na';
  }
  return value.toFixed(2);
}

function formatProbeInt(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return 'na';
  }
  return String(Math.round(value));
}

export function describeHlsRenderProbe(
  stage: 'playing' | 'stable' | 'black_suspect' | 'heartbeat',
  snapshot: HlsRenderProbeSnapshot,
  baseline?: HlsRenderProbeSnapshot
): string {
  const deltaTime = baseline ? snapshot.currentTime - baseline.currentTime : null;
  const deltaFrames = baseline && baseline.totalFrames !== null && snapshot.totalFrames !== null
    ? snapshot.totalFrames - baseline.totalFrames
    : null;

  const stalls = snapshot.stalls;
  const baseStalls = baseline?.stalls;
  const stallDelta = (pick: (c: PlaybackStallCounters) => number): string =>
    stalls && baseStalls ? formatProbeInt(pick(stalls) - pick(baseStalls)) : 'na';

  return [
    `hlsjs_render stage=${stage}`,
    `t=${formatProbeFloat(snapshot.currentTime)}`,
    `rs=${snapshot.readyState}`,
    `ns=${snapshot.networkState}`,
    `paused=${snapshot.paused ? 1 : 0}`,
    `dims=${snapshot.videoWidth}x${snapshot.videoHeight}`,
    `buf=${formatProbeFloat(snapshot.bufferedAhead)}`,
    `rate=${formatProbeFloat(snapshot.playbackRate)}`,
    `frames=${formatProbeInt(snapshot.totalFrames)}`,
    `drop=${formatProbeInt(snapshot.droppedFrames)}`,
    baseline ? `dt=${formatProbeFloat(deltaTime)}` : null,
    baseline ? `df=${formatProbeInt(deltaFrames)}` : null,
    snapshot.bufferedRanges !== undefined ? `ranges=${snapshot.bufferedRanges}` : null,
    snapshot.bufferedTail !== undefined ? `tail=${formatProbeFloat(snapshot.bufferedTail)}` : null,
    snapshot.liveLatency !== undefined ? `lat=${formatProbeFloat(snapshot.liveLatency)}` : null,
    stalls ? `stalls=${stalls.stalls}` : null,
    stalls ? `stall_ms=${formatProbeInt(stalls.stallMs)}` : null,
    stalls ? `holes=${stalls.holeJumps}` : null,
    stalls ? `nudges=${stalls.nudges}` : null,
    stalls ? `stall_errs=${stalls.stallErrors}` : null,
    stalls ? `append_errs=${stalls.appendErrors}` : null,
    stalls && baseStalls ? `dstalls=${stallDelta((c) => c.stalls)}` : null,
    stalls && baseStalls ? `dstall_ms=${stallDelta((c) => c.stallMs)}` : null,
    stalls && baseStalls ? `dholes=${stallDelta((c) => c.holeJumps)}` : null,
    stalls && baseStalls ? `dnudges=${stallDelta((c) => c.nudges)}` : null,
    stalls && baseStalls ? `dstall_errs=${stallDelta((c) => c.stallErrors)}` : null,
  ].filter(Boolean).join(' ');
}

export function isBlackRenderSuspect(start: HlsRenderProbeSnapshot, current: HlsRenderProbeSnapshot): boolean {
  const progressed = current.currentTime - start.currentTime;
  const hasBufferedPlayback = current.bufferedAhead >= 0.5 && current.readyState >= 2 && !current.paused;
  if (progressed < 1 || !hasBufferedPlayback) {
    return false;
  }
  if (current.videoWidth <= 0 || current.videoHeight <= 0) {
    return true;
  }
  if (start.totalFrames === null || current.totalFrames === null) {
    return false;
  }
  return current.totalFrames <= start.totalFrames;
}
