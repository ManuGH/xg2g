// Composes the live-DVR position readout shown above the timeline scrubber:
//   behind live -> "14:23 · 7:00 behind live"
//   at the edge -> "14:23 · Live"
//   VOD         -> the elapsed position only ("12:34")
// Kept pure (translate + clock formatter injected) so the edge / behind / VOD
// branching is unit-tested with a negative control, independent of render gating.
export interface DvrPositionDisplayInput {
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  behindLiveSeconds: number;
  currentTimeDisplay: string;
}

export type DvrPositionTranslate = (key: string, options: Record<string, unknown>) => string;

// Within this many seconds of the edge we render "Live" instead of an offset. It
// covers the chrome's own isAtLiveEdge tolerance plus a little slack so the readout
// doesn't flicker between "Live" and "0:03 behind live" right at the edge.
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
    return t('player.dvrPosition.live', { defaultValue: '{{time}} · Live', time: currentTimeDisplay });
  }
  return t('player.dvrPosition.behindLive', {
    defaultValue: '{{time}} · {{offset}} behind live',
    time: currentTimeDisplay,
    offset: formatClock(behindLiveSeconds),
  });
}
