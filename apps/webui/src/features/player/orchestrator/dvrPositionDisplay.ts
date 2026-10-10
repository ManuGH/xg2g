// Composes the live-DVR position readout shown at the timeline scrubber:
//   behind live (<60s)  -> "5 s behind live"
//   behind live (>=60s) -> "7:00 behind live"
//   at the edge         -> "● LIVE"
//   VOD                 -> the elapsed position only ("12:34")
// Kept pure (translate + clock formatter injected) so the edge / behind / VOD
// branching is unit-tested with a negative control, independent of render gating.
export interface DvrPositionDisplayInput {
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  behindLiveSeconds: number;
  currentTimeDisplay: string;
}

export type DvrPositionKey = 'player.dvrPosition.live' | 'player.dvrPosition.behindLive';
export type DvrPositionTranslate = (key: DvrPositionKey, options: Record<string, unknown>) => string;

// Within this many seconds of the edge we render "● LIVE" instead of an offset. It
// covers the chrome's own isAtLiveEdge tolerance plus a little slack so the readout
// doesn't flicker between "● LIVE" and "5 s behind live" right at the edge.
const LIVE_EDGE_SLACK_SECONDS = 5;

export function formatDvrPositionDisplay(
  input: DvrPositionDisplayInput,
  formatClock: (seconds: number) => string,
  t: DvrPositionTranslate,
): string {
  const { isLiveMode, isAtLiveEdge, behindLiveSeconds, currentTimeDisplay } = input;
  if (!isLiveMode) {
    return currentTimeDisplay;
  }
  if (isAtLiveEdge || behindLiveSeconds < LIVE_EDGE_SLACK_SECONDS) {
    return t('player.dvrPosition.live', { defaultValue: '● LIVE', time: currentTimeDisplay });
  }
  const offsetDisplay =
    behindLiveSeconds < 60
      ? `${Math.max(1, Math.round(behindLiveSeconds))} s`
      : formatClock(behindLiveSeconds);
  return t('player.dvrPosition.behindLive', {
    defaultValue: '{{offset}} behind live',
    time: currentTimeDisplay,
    offset: offsetDisplay,
  });
}
