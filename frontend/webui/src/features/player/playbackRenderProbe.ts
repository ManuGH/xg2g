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
  stage: 'playing' | 'stable' | 'black_suspect' | 'heartbeat',
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
