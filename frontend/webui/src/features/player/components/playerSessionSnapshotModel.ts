import type { PlaybackTrace as PlaybackTraceContract } from '../../../client-ts';
import type { V3SessionSnapshot } from '../../../types/v3-player';
import {
  extractPlaybackTrace,
  normalizePlaybackWindowKind,
  type PlaybackWindowKind,
} from './playerPlaybackModel';

export type PlayerLiveSeekWindow = {
  start: number;
  end: number;
  liveEdge: number | null;
  capturedAtMs?: number;
};

export interface PlayerSessionSnapshotModel {
  traceId: string | null;
  profileReason: string | null;
  playbackTrace: PlaybackTraceContract | null;
  sessionWindowKind: PlaybackWindowKind;
  liveSeekWindow: PlayerLiveSeekWindow | null | undefined;
}

function finiteNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

export function buildPlayerSessionSnapshotModel(
  session: V3SessionSnapshot,
  capturedAtMs: number,
): PlayerSessionSnapshotModel {
  const snapshotWindowKind = normalizePlaybackWindowKind(session.windowKind);
  const traceId = session.requestId || null;
  const profileReason = session.profileReason ?? null;
  const playbackTrace = extractPlaybackTrace(session);

  if (session.mode === 'LIVE') {
    const seekableStart = finiteNumber(session.seekableStartSeconds);
    const seekableEnd = finiteNumber(session.seekableEndSeconds);
    const liveEdge = finiteNumber(session.liveEdgeSeconds);
    const durationSeconds = finiteNumber(session.durationSeconds);
    let liveSeekWindow: PlayerLiveSeekWindow | null = null;

    if (seekableStart !== null && seekableEnd !== null && seekableEnd > seekableStart) {
      liveSeekWindow = {
        start: Math.max(0, seekableStart),
        end: Math.max(seekableStart, seekableEnd),
        liveEdge: liveEdge !== null ? Math.max(seekableEnd, liveEdge) : seekableEnd,
        capturedAtMs,
      };
    } else if (durationSeconds !== null && durationSeconds > 0) {
      const derivedEnd = liveEdge ?? seekableEnd ?? durationSeconds;
      const derivedStart = Math.max(0, derivedEnd - durationSeconds);
      liveSeekWindow = {
        start: derivedStart,
        end: Math.max(derivedStart, derivedEnd),
        liveEdge: liveEdge ?? derivedEnd,
        capturedAtMs,
      };
    }

    return {
      traceId,
      profileReason,
      playbackTrace,
      sessionWindowKind: snapshotWindowKind,
      liveSeekWindow,
    };
  }

  if (session.mode === 'RECORDING') {
    return {
      traceId,
      profileReason,
      playbackTrace,
      sessionWindowKind: snapshotWindowKind !== 'unknown' ? snapshotWindowKind : 'vod',
      liveSeekWindow: null,
    };
  }

  return {
    traceId,
    profileReason,
    playbackTrace,
    sessionWindowKind: snapshotWindowKind,
    liveSeekWindow: undefined,
  };
}
