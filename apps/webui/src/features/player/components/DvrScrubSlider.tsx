import { useCallback, useRef, useState, type CSSProperties } from 'react';
import styles from './V3Player.module.css';
import {
  clampFraction,
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
  const [isScrubbing, setIsScrubbing] = useState(false);
  const [scrubValue, setScrubValue] = useState<number | null>(null);

  const displayValue = isScrubbing && scrubValue !== null ? scrubValue : value;

  const commitSeek = useCallback(
    (targetVal: number) => {
      setIsScrubbing(false);
      setScrubValue(null);
      onSeek(targetVal);
    },
    [onSeek],
  );

  const updateHoverForFraction = useCallback(
    (fraction: number, targetWidth: number) => {
      if (!previewBaseUrl) return;
      const effectiveWidth = targetWidth > 0 ? targetWidth : 100;
      const offset = previewOffsetForFraction(fraction, max, segmentSeconds);
      const leftPx = Math.max(0, Math.min(effectiveWidth, effectiveWidth * clampFraction(fraction)));
      setHover({
        visible: true,
        leftPx,
        url: dvrPreviewImageUrl(previewBaseUrl, offset),
        label: previewHoverLabel(offset, windowStartUnix),
      });
    },
    [previewBaseUrl, max, segmentSeconds, windowStartUnix],
  );

  const handleMove = useCallback(
    (e: React.MouseEvent<HTMLInputElement>) => {
      if (!previewBaseUrl) return;
      const wrap = wrapRef.current;
      if (!wrap) return;
      const rect = e.currentTarget.getBoundingClientRect();
      if (rect.width <= 0) return;
      const fraction = (e.clientX - rect.left) / rect.width;
      const leftPx = e.clientX - wrap.getBoundingClientRect().left;
      const offset = previewOffsetForFraction(fraction, max, segmentSeconds);
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

  const handleFocus = useCallback(() => {
    const wrap = wrapRef.current;
    if (!wrap || max <= 0) return;
    const fraction = displayValue / max;
    const width = wrap.getBoundingClientRect().width;
    updateHoverForFraction(fraction, width);
  }, [max, displayValue, updateHoverForFraction]);

  const handleBlur = useCallback(() => setHover(HIDDEN), []);

  const isDraggingRef = useRef(false);

  const handlePointerDown = useCallback(() => {
    isDraggingRef.current = true;
    setIsScrubbing(true);
  }, []);

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const newValue = parseFloat(e.target.value);
      if (isDraggingRef.current) {
        setScrubValue(newValue);
      } else {
        onSeek(newValue);
      }
      const wrap = wrapRef.current;
      if (wrap && max > 0 && hover.visible) {
        const fraction = newValue / max;
        const width = wrap.getBoundingClientRect().width;
        updateHoverForFraction(fraction, width);
      }
    },
    [max, onSeek, hover.visible, updateHoverForFraction],
  );

  const handlePointerUp = useCallback(
    (e: React.PointerEvent<HTMLInputElement>) => {
      if (isDraggingRef.current) {
        isDraggingRef.current = false;
        const newValue = parseFloat(e.currentTarget.value);
        commitSeek(newValue);
      }
    },
    [commitSeek],
  );

  const handleKeyUp = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'Home' || e.key === 'End') {
        const newValue = parseFloat(e.currentTarget.value);
        commitSeek(newValue);
      }
    },
    [commitSeek],
  );

  // Filled-progress portion of the track (YouTube-style), driven purely by a CSS
  // custom property so the native <input type=range> keeps owning all seek
  // interaction — the visual fill never touches the DVR seek path.
  const fillPct = max > 0 ? Math.min(100, Math.max(0, (displayValue / max) * 100)) : 0;

  return (
    <div className={styles.dvrSliderWrap} ref={wrapRef}>
      {previewBaseUrl && hover.visible && (
        <div
          className={styles.dvrPreview}
          style={{ '--xg2g-dvr-preview-left': `${hover.leftPx}px`, '--xg2g-dvr-preview-image': `url("${hover.url}")` } as CSSProperties}
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
        style={{ '--xg2g-dvr-fill': `${fillPct}%` } as CSSProperties}
        value={displayValue}
        onPointerDown={handlePointerDown}
        onPointerUp={handlePointerUp}
        onChange={handleChange}
        onKeyUp={handleKeyUp}
        onFocus={handleFocus}
        onBlur={handleBlur}
        onMouseMove={handleMove}
        onMouseLeave={handleLeave}
      />
    </div>
  );
}
