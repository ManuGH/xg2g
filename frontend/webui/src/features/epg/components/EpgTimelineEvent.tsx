
import type { EpgEvent } from '../types';
import { normalizeEpgText } from '../../../utils/text';
import styles from '../EPG.module.css';

// Utility: Format Unix timestamp (seconds) to HH:MM
function formatTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export interface EpgTimelineEventProps {
  event: EpgEvent;
  leftPx: number;
  widthPx: number;
  currentTime: number;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: boolean;
  onClick?: (event: EpgEvent) => void;
}

export function EpgTimelineEvent({
  event,
  leftPx,
  widthPx,
  currentTime,
  onRecord,
  isRecorded = false,
  onClick,
}: EpgTimelineEventProps) {
  const inProgress = currentTime >= event.start && currentTime < event.end;
  const isPast = currentTime > event.end;

  // Show time and extra details only if the block is wide enough to avoid clutter
  const showTime = widthPx > 90;
  const showRecord = widthPx > 60;

  return (
    <div
      className={[
        styles.timelineEventBlock,
        inProgress ? styles.timelineEventBlockActive : null,
        isPast ? styles.timelineEventBlockPast : null,
      ].filter(Boolean).join(' ')}
      style={{ left: leftPx, width: widthPx }}
      role="button"
      tabIndex={0}
      title={event.desc ? normalizeEpgText(event.desc) : event.title}
      onClick={() => onClick?.(event)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onClick?.(event);
        }
      }}
    >
      <div className={styles.timelineEventTitleGroup}>
        <div className={styles.timelineEventTitle}>
          {event.title || '—'}
        </div>
        {showTime && (
          <div className={styles.timelineEventTime}>
            {formatTime(event.start)} – {formatTime(event.end)}
          </div>
        )}
        {onRecord && !isPast && showRecord && (
          <div className={styles.timelineEventRecordAction}>
            {isRecorded ? (
              <span className={styles.recordIndicatorDot} title="Aufnahme geplant" />
            ) : (
              <button
                className={styles.recordButton}
                onClick={(e) => {
                  e.stopPropagation();
                  onRecord(event);
                }}
                title="Aufnahme planen"
              >
                <span className={styles.recordButtonDot} />
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
