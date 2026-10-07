import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { EpgChannel, EpgEvent } from '../types';
import { EpgTimelineEvent } from './EpgTimelineEvent';
import styles from '../EPG.module.css';

export interface EpgTimelineRowProps {
  channel: EpgChannel;
  events: EpgEvent[];
  isFavorite: boolean;
  currentTime: number;
  startTimestampMs: number;
  timelineWidth: number;
  pixelsPerHour: number;
  onPlay?: (channel: EpgChannel) => void;
  onToggleFavorite?: (serviceRef: string) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
  onEventClick?: (event: EpgEvent) => void;
}

export function EpgTimelineRow({
  channel,
  events,
  isFavorite,
  currentTime,
  startTimestampMs,
  timelineWidth,
  pixelsPerHour,
  onPlay,
  onToggleFavorite,
  onRecord,
  isRecorded,
  onEventClick,
}: EpgTimelineRowProps) {
  const { t } = useTranslation();
  const unknownChannelText = t('epg.unknownChannel', {});
  const displayName = channel
    ? `${channel.number ? `${channel.number} · ` : ''}${channel.name || channel.id || unknownChannelText}`
    : unknownChannelText;
  const ref = channel.serviceRef || channel.id || '';

  return (
    <div className={styles.timelineRow} style={{ '--xg2g-timeline-row-width': `${timelineWidth + 320}px` } as CSSProperties}>
      {/* Sticky Channel Card */}
      <div 
        className={styles.timelineChannelCard} 
        onClick={() => onPlay?.(channel)}
        role="button"
        tabIndex={0}
      >
        <div className={styles.timelineChannelContent}>
          <button 
            type="button"
            className={`${styles.timelineFavoriteButton} ${isFavorite ? styles.timelineFavoriteButtonActive : ''}`}
            onClick={(e) => {
              e.stopPropagation();
              onToggleFavorite?.(ref);
            }}
            title={t('epg.toggleFavorite', {})}
            aria-label={t('epg.toggleFavorite', {})}
          >
            {isFavorite ? '★' : '☆'}
          </button>
          {channel.logoUrl && (
            <img src={channel.logoUrl} alt="" className={styles.timelineChannelLogo} loading="lazy" />
          )}
          <span className={styles.timelineChannelName}>{displayName}</span>
        </div>
      </div>

      {/* Events Container */}
      <div className={styles.timelineEventsContainer} style={{ '--xg2g-timeline-width': `${timelineWidth}px` } as CSSProperties}>
        {events.map((event, idx) => {
          const eventStartMs = event.start * 1000;
          const eventEndMs = event.end * 1000;
          
          const timelineEndMs = startTimestampMs + (timelineWidth / pixelsPerHour) * 3600 * 1000;
          if (eventEndMs <= startTimestampMs || eventStartMs >= timelineEndMs) {
            return null;
          }

          const leftPx = Math.max(0, ((eventStartMs - startTimestampMs) / (3600 * 1000)) * pixelsPerHour);
          const rightPx = Math.min(timelineWidth, ((eventEndMs - startTimestampMs) / (3600 * 1000)) * pixelsPerHour);
          const rawWidth = rightPx - leftPx;
          // Subtract 4px for visual gap, unless width is very small
          const widthPx = Math.max(2, rawWidth - 4);

          return (
            <EpgTimelineEvent
              key={`${event.serviceRef}-${event.start}-${idx}`}
              event={event}
              leftPx={leftPx}
              widthPx={widthPx}
              currentTime={currentTime}
              onRecord={onRecord}
              isRecorded={isRecorded?.(event) ?? false}
              onClick={onEventClick}
            />
          );
        })}
      </div>
    </div>
  );
}
