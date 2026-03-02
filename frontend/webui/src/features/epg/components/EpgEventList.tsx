// EPG Event List - Programme row rendering
// Zero API imports

import { useState } from 'react';
import type { EpgEvent } from '../types';
import { normalizeEpgText } from '../../../utils/text';
import styles from '../EPG.module.css';

// Utility: Format Unix timestamp (seconds) to HH:MM
function formatTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function formatRange(start: number, end: number): string {
  if (!start || !end) return '';
  return `${formatTime(start)} – ${formatTime(end)}`;
}

export interface EpgEventRowProps {
  event: EpgEvent;
  currentTime: number; // Unix timestamp (seconds)
  highlight?: boolean;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: boolean;
  dateLabel?: string | null; // Optional date header (e.g. "Do 28.12.")
}

export function EpgEventRow({
  event,
  currentTime,
  highlight = false,
  onRecord,
  isRecorded = false,
  dateLabel,
}: EpgEventRowProps) {
  const [expanded, setExpanded] = useState(false);

  const inProgress = currentTime >= event.start && currentTime < event.end;
  const total = event.end - event.start;
  const elapsed = Math.max(0, Math.min(total, currentTime - event.start));
  const pct = total > 0 ? Math.round((elapsed / total) * 100) : 0;

  const handleToggle = (e: React.MouseEvent) => {
    // Prevent toggle if clicking buttons
    if ((e.target as HTMLElement).closest('button')) return;
    setExpanded(!expanded);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      setExpanded(prev => !prev);
    }
  };

  return (
    <>
      {/* Date Header Insert */}
      {dateLabel && <div className={styles.dateLabel}>{dateLabel}</div>}

      <div
        className={[styles.programme, highlight ? styles.programmeCurrent : null].filter(Boolean).join(' ')}
        onClick={handleToggle}
        onKeyDown={handleKeyDown}
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
      >
        <div className={styles.programmeTime}>
          {formatRange(event.start, event.end)}
          {onRecord &&
            (isRecorded ? (
              <span title="Aufnahme geplant" className={styles.recordIndicator}>
                🔴
              </span>
            ) : (
              <button
                className={styles.recordButton}
                onClick={(e) => {
                  e.stopPropagation();
                  onRecord(event);
                }}
                title="Aufnahme planen"
              >
                ⏺
              </button>
            ))}
        </div>
        <div className={styles.programmeBody}>
          <div className={styles.programmeTitle}>{event.title || '—'}</div>
          {event.desc && (
            <div className={[styles.programmeDesc, expanded ? styles.programmeDescExpanded : null].filter(Boolean).join(' ')}>
              {normalizeEpgText(event.desc)}
            </div>
          )}
          {inProgress && (
            <div className={styles.progressContainer}>
              <div className={styles.progress}>
                <div className={styles.progressBar} style={{ width: `${pct}%` }} />
              </div>
              <div className={styles.progressMeta}>
                <span>{formatTime(event.start)}</span>
                <span>{pct}%</span>
                <span>{formatTime(event.end)}</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
