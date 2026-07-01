import { useCallback, useRef, useState } from 'react';
import styles from './V3Player.module.css';
import {
  previewOffsetForFraction,
  dvrPreviewImageUrl,
  previewHoverLabel,
} from './dvrScrubPreviewModel';

interface DvrScrubSliderProps {
  value: number;
  max: number; // windowDuration (seconds)
  sliderClassName?: string;
  onSeek: (offsetSeconds: number) => void;
  // When null, the slider behaves exactly like the plain range input (no preview).
  previewBaseUrl: string | null;
  windowStartUnix: number | null;
  segmentSeconds?: number;
}

interface HoverState {
  visible: boolean;
  leftPx: number;
  url: string;
  label: string;
}

const HIDDEN: HoverState = { visible: false, leftPx: 0, url: '', label: '' };

/**
 * The DVR scrubber with a YouTube-style hover thumbnail. On mouse-move the cursor
 * position maps to a segment-aligned window offset; the backend renders that
 * segment's keyframe at /hls/preview.jpg?t=offset. We use a CSS background-image
 * (not an <img>) so a not-yet-generated frame just shows the placeholder box
 * instead of a broken-image icon, and the browser caches each segment's tile.
 * Mouse-only by design — touch DVR uses the native fullscreen controls.
 */
export function DvrScrubSlider({
  value,
  max,
  sliderClassName,
  onSeek,
  previewBaseUrl,
  windowStartUnix,
  segmentSeconds = 6,
}: DvrScrubSliderProps) {
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [hover, setHover] = useState<HoverState>(HIDDEN);

  const handleMove = useCallback(
    (e: React.MouseEvent<HTMLInputElement>) => {
      if (!previewBaseUrl) return;
      const wrap = wrapRef.current;
      if (!wrap) return;
      const rect = e.currentTarget.getBoundingClientRect();
      if (rect.width <= 0) return;
      const fraction = (e.clientX - rect.left) / rect.width;
      const offset = previewOffsetForFraction(fraction, max, segmentSeconds);
      const leftPx = e.clientX - wrap.getBoundingClientRect().left;
      setHover({
        visible: true,
        leftPx,
        url: dvrPreviewImageUrl(previewBaseUrl, offset),
        label: previewHoverLabel(offset, windowStartUnix),
      });
    },
    [previewBaseUrl, max, segmentSeconds, windowStartUnix],
  );

  const handleLeave = useCallback(() => setHover(HIDDEN), []);

  // Filled-progress portion of the track (YouTube-style), driven purely by a CSS
  // custom property so the native <input type=range> keeps owning all seek
  // interaction — the visual fill never touches the DVR seek path.
  const fillPct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;

  return (
    <div className={styles.dvrSliderWrap} ref={wrapRef}>
      {previewBaseUrl && hover.visible && (
        <div
          className={styles.dvrPreview}
          style={{ left: `${hover.leftPx}px`, backgroundImage: `url("${hover.url}")` }}
          aria-hidden="true"
        >
          <span className={styles.dvrPreviewLabel}>{hover.label}</span>
        </div>
      )}
      <input
        type="range"
        min="0"
        max={max}
        step="0.1"
        className={sliderClassName}
        style={{ '--dvr-fill': `${fillPct}%` } as React.CSSProperties}
        value={value}
        onChange={(e) => onSeek(parseFloat(e.target.value))}
        onMouseMove={handleMove}
        onMouseLeave={handleLeave}
      />
    </div>
  );
}
