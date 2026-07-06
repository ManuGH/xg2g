import { useEffect } from 'react';
import type { EpgEvent } from '../types';
import { normalizeEpgText } from '../../../utils/text';
import { Button } from '../../../components/ui';
import styles from './EpgEventDialog.module.css';

interface EpgEventDialogProps {
  event: EpgEvent;
  onClose: () => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: boolean;
}

function formatDateTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function EpgEventDialog({ event, onClose, onRecord, isRecorded }: EpgEventDialogProps) {
  useEffect(() => {
    // Lock body scroll
    const originalOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = originalOverflow;
    };
  }, [onClose]);

  const desc = event.desc ? normalizeEpgText(event.desc) : 'Keine Beschreibung verfügbar.';

  return (
    <div
      className={styles.overlay}
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className={styles.card} role="dialog" aria-modal="true" aria-labelledby="epg-event-title">
        <div className={styles.header}>
          <h2 id="epg-event-title" className={styles.title}>
            {event.title || 'Unbekannte Sendung'}
          </h2>
          <div className={styles.time}>
            {formatDateTime(event.start)} – {formatTime(event.end)}
          </div>
        </div>

        <div className={styles.content}>
          {desc}
        </div>

        <div className={styles.footer}>
          {onRecord && (
            <Button
              variant={isRecorded ? 'secondary' : 'primary'}
              onClick={() => {
                onRecord(event);
                onClose();
              }}
            >
              {isRecorded ? 'Aufnahme geplant' : 'Aufnehmen'}
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>
            Schließen
          </Button>
        </div>
      </div>
    </div>
  );
}
