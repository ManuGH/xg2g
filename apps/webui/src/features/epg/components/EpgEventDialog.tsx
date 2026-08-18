import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import type { EpgEvent, EpgChannel } from '../types';
import { normalizeEpgText } from '../../../utils/text';
import { formatClockLabelFromSeconds } from '../../../utils/onAir';
import { Button } from '../../../components/ui';
import styles from './EpgEventDialog.module.css';

interface EpgEventDialogProps {
  event: EpgEvent;
  onClose: () => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: boolean;
  onPlay?: (channel: EpgChannel) => void;
  channel?: EpgChannel;
  currentTime?: number;
}

function formatDateTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

export function EpgEventDialog({ event, onClose, onRecord, isRecorded, onPlay, channel, currentTime }: EpgEventDialogProps) {
  const { t } = useTranslation();
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

  const desc = event.desc ? normalizeEpgText(event.desc) : t('epg.noDescription', { defaultValue: 'No description available.' });
  const now = currentTime || Math.floor(Date.now() / 1000);
  const inProgress = now >= event.start && now < event.end;

  return createPortal(
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
            {event.title || t('epg.unknownTitle', { defaultValue: 'Unknown show' })}
          </h2>
          <div className={styles.time}>
            {channel?.name ? `${channel.name} · ` : ''}{formatDateTime(event.start)} – {formatClockLabelFromSeconds(event.end)}
          </div>
        </div>

        <div className={styles.content}>
          {desc}
        </div>

        <div className={styles.footer}>
          {inProgress && channel && onPlay && (
            <Button
              variant="primary"
              onClick={() => {
                onPlay(channel);
                onClose();
              }}
            >
              ▶ {t('epg.playChannel', { defaultValue: 'Sendung schauen' })}
            </Button>
          )}
          {onRecord && (
            <Button
              variant={inProgress && channel && onPlay ? 'secondary' : (isRecorded ? 'secondary' : 'primary')}
              onClick={() => {
                onRecord(event);
                onClose();
              }}
            >
              {isRecorded ? t('epg.recordingPlanned', { defaultValue: 'Aufnahme geplant' }) : t('epg.record', { defaultValue: 'Aufnehmen' })}
            </Button>
          )}
          <Button variant="secondary" onClick={onClose}>
            {t('common.close', { defaultValue: 'Schließen' })}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
