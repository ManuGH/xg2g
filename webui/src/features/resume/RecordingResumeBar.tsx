import { useMemo, type CSSProperties } from 'react';
import type { ResumeSummary } from './types';
import styles from './resume.module.css';

function clamp(n: number, min: number, max: number) {
  // display-only: progress-bar normalization for rendering.
  return Math.max(min, Math.min(max, n));
}

function formatClock(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '--:--';
  const total = Math.floor(value);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

export function isResumeEligible(resume?: ResumeSummary, durationSeconds?: number | null): boolean {
  if (!resume) return false;
  if (resume.finished) return false;
  if (resume.posSeconds < 15) return false;

  const d = (resume.durationSeconds ?? durationSeconds ?? 0);

  // PRODUCT DECISION:
  // If duration is missing (d <= 0) but we have significant progress (> 15s), show the "Resume at..." label.
  // The progress bar itself will be hidden (percent calculation requires valid duration).
  // This avoids hiding useful resume context just because duration probe failed.
  if (d > 0 && resume.posSeconds >= d - 10) return false;

  return true;
}

export default function RecordingResumeBar(props: {
  resume: ResumeSummary;
  durationSeconds?: number | null;
}) {
  const { resume, durationSeconds } = props;
  const d = resume.durationSeconds ?? durationSeconds ?? 0;

  const percent = useMemo(() => {
    if (!d || d <= 0) return 0;
    // display-only: normalize bar width percentage in UI.
    return Math.round(clamp(resume.posSeconds / d, 0, 1) * 100);
  }, [resume.posSeconds, d]);

  const barStyle = useMemo(
    () =>
      ({
        '--xg2g-resume-progress': `${percent}%`,
      }) as CSSProperties,
    [percent],
  );

  return (
    <div className={styles.summary}>
      <div className={styles.meta}>
        <span className={styles.label}>Resume at {formatClock(resume.posSeconds)}</span>
        {d > 0 && <span className={styles.percent}>{percent}%</span>}
      </div>
      {d > 0 && (
        <div className={styles.bar}>
          <div className={styles.barFill} style={barStyle} />
        </div>
      )}
    </div>
  );
}
