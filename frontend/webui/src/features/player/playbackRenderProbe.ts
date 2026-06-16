import type { VideoElementRef } from '../../types/v3-player';

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
  stage: 'playing' | 'stable' | 'black_suspect',
  snapshot: HlsRenderProbeSnapshot,
  baseline?: HlsRenderProbeSnapshot
): string {
  const deltaTime = baseline ? snapshot.currentTime - baseline.currentTime : null;
  const deltaFrames = baseline && baseline.totalFrames !== null && snapshot.totalFrames !== null
    ? snapshot.totalFrames - baseline.totalFrames
    : null;

  return [
    `hlsjs_render stage=${stage}`,
    `t=${formatProbeFloat(snapshot.currentTime)}`,
    `rs=${snapshot.readyState}`,
    `ns=${snapshot.networkState}`,
    `paused=${snapshot.paused ? 1 : 0}`,
    `dims=${snapshot.videoWidth}x${snapshot.videoHeight}`,
    `buf=${formatProbeFloat(snapshot.bufferedAhead)}`,
    `frames=${formatProbeInt(snapshot.totalFrames)}`,
    `drop=${formatProbeInt(snapshot.droppedFrames)}`,
    baseline ? `dt=${formatProbeFloat(deltaTime)}` : null,
    baseline ? `df=${formatProbeInt(deltaFrames)}` : null,
  ].filter(Boolean).join(' ');
}

export interface RenderQualityThresholds {
  /** Minimum decoded frames in the window before a verdict is trustworthy. */
  minTotalFrames: number;
  /** Dropped/total ratio at or above which the window counts as degraded. */
  maxDroppedRatio: number;
}

export const DEFAULT_RENDER_QUALITY_THRESHOLDS: RenderQualityThresholds = {
  // ~6 s at 50 fps — enough to look past a noisy startup before judging.
  minTotalFrames: 300,
  // 5 % sustained dropped frames is the line between "smooth" and "stutters".
  maxDroppedRatio: 0.05,
};

export interface RenderQualityVerdict {
  ratio: number | null;
  droppedDelta: number | null;
  totalDelta: number | null;
  /** True only when there is enough signal AND the drop ratio is over threshold. */
  exceeded: boolean;
}

/**
 * Compare two cumulative frame-counter samples and decide whether the device is
 * actually decoding smoothly — the ground truth that confirms or refutes the
 * capability probe's `smooth`/`powerEfficient` estimate. Pure and side-effect
 * free so the threshold logic is unit-testable in isolation.
 */
export function evaluateRenderQuality(
  baseline: PlaybackFrameCounters,
  current: PlaybackFrameCounters,
  thresholds: RenderQualityThresholds = DEFAULT_RENDER_QUALITY_THRESHOLDS
): RenderQualityVerdict {
  if (
    baseline.totalFrames === null ||
    current.totalFrames === null ||
    baseline.droppedFrames === null ||
    current.droppedFrames === null
  ) {
    return { ratio: null, droppedDelta: null, totalDelta: null, exceeded: false };
  }

  const totalDelta = current.totalFrames - baseline.totalFrames;
  const droppedDelta = current.droppedFrames - baseline.droppedFrames;

  if (totalDelta < thresholds.minTotalFrames || totalDelta <= 0 || droppedDelta < 0) {
    return { ratio: null, droppedDelta, totalDelta, exceeded: false };
  }

  const ratio = droppedDelta / totalDelta;
  return {
    ratio,
    droppedDelta,
    totalDelta,
    exceeded: ratio >= thresholds.maxDroppedRatio,
  };
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
