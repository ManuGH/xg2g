// EPG Event List - Programme row rendering
// Zero API imports

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
}

export function EpgEventRow({
  event,
  currentTime,
  highlight = false,
  onRecord,
  isRecorded = false,
}: EpgEventRowProps) {
  const inProgress = currentTime >= event.start && currentTime < event.end;
  const total = event.end - event.start;
  const elapsed = Math.max(0, Math.min(total, currentTime - event.start));
  const pct = total > 0 ? Math.round((elapsed / total) * 100) : 0;

  return (
    <div className={`epg-programme${highlight ? ' epg-programme-current' : ''}`}>
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
        {event.desc && <div className="epg-programme-desc">{event.desc}</div>}
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
  );
}

export interface EpgEventListProps {
  events: EpgEvent[];
  currentTime: number;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
}

export function EpgEventList({
  events,
  currentTime,
  onRecord,
  isRecorded,
}: EpgEventListProps) {
  return (
    <>
      {events.map((event) => (
        <EpgEventRow
          key={`${event.service_ref}-${event.start}`}
          event={event}
          currentTime={currentTime}
          highlight={currentTime >= event.start && currentTime < event.end}
          onRecord={onRecord}
          isRecorded={isRecorded ? isRecorded(event) : false}
        />
      ))}
    </>
  );
}
