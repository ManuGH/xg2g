import { useMemo } from 'react';
import type { ResumeSummary } from './types';
import './resume.css';

function clamp(n: number, min: number, max: number) {
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
  if (resume.pos_seconds < 15) return false;

  const d = (resume.duration_seconds ?? durationSeconds ?? 0);

  // PRODUCT DECISION:
  // If duration is missing (d <= 0) but we have significant progress (> 15s), show the "Resume at..." label.
  // The progress bar itself will be hidden (percent calculation requires valid duration).
  // This avoids hiding useful resume context just because duration probe failed.
  if (d > 0 && resume.pos_seconds >= d - 10) return false;

  return true;
}

export default function RecordingResumeBar(props: {
  resume: ResumeSummary;
  durationSeconds?: number | null;
}) {
  const { resume, durationSeconds } = props;
  const d = resume.duration_seconds ?? durationSeconds ?? 0;

  const percent = useMemo(() => {
    if (!d || d <= 0) return 0;
    return Math.round(clamp(resume.pos_seconds / d, 0, 1) * 100);
  }, [resume.pos_seconds, d]);

  return (
    <div className="resume-summary">
      <div className="resume-meta">
        <span className="resume-label">Resume at {formatClock(resume.pos_seconds)}</span>
        {d > 0 && <span className="resume-percent">{percent}%</span>}
      </div>
      {d > 0 && (
        <div className="resume-bar">
          <div className="resume-bar-fill" style={{ width: `${percent}%` }} />
        </div>
      )}
    </div>
  );
}
