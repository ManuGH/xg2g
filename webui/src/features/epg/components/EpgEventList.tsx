// EPG Event List - Programme row rendering
// Zero API imports

import { useState } from 'react';
import type { EpgEvent } from '../types';

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

  return (
    <>
      {/* Date Header Insert */}
      {dateLabel && <div className="epg-date-label">{dateLabel}</div>}

      <div
        className={`epg-programme${highlight ? ' epg-programme-current' : ''}${expanded ? ' expanded' : ''}`}
        onClick={handleToggle}
        role="button"
        tabIndex={0}
      >
        <div className="epg-programme-time">
          {formatRange(event.start, event.end)}
          {onRecord &&
            (isRecorded ? (
              <span title="Aufnahme geplant" className="epg-record-indicator">
                🔴
              </span>
            ) : (
              <button
                className="epg-record-btn"
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
        <div className="epg-programme-body">
          <div className="epg-programme-title">{event.title || '—'}</div>
          {event.desc && (
            <div className={`epg-programme-desc${expanded ? ' expanded' : ''}`}>
              {event.desc}
            </div>
          )}
          {inProgress && (
            <div className="epg-progress-container">
              <div className="epg-progress">
                <div className="epg-progress-bar" style={{ width: `${pct}%` }} />
              </div>
              <div className="epg-progress-meta">
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

