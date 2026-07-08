import { useRef } from 'react';
import type { EpgChannel, EpgEvent } from '../types';
import { EpgTimelineRow } from './EpgTimelineRow';
import styles from '../EPG.module.css';

export interface EpgTimelineGridProps {
  channels: EpgChannel[];
  eventsByServiceRef: Map<string, EpgEvent[]>;
  favoriteServiceRefs?: Set<string>;
  currentTime: number;
  timeRangeHours: number;
  onToggleFavorite?: (serviceRef: string) => void;
  onRecord?: (event: EpgEvent) => void;
  isRecorded?: (event: EpgEvent) => boolean;
  onPlay?: (channel: EpgChannel) => void;
  onEventClick?: (event: EpgEvent) => void;
}

const PIXELS_PER_HOUR = 900;

export function EpgTimelineGrid({
  channels,
  eventsByServiceRef,
  favoriteServiceRefs,
  currentTime,
  timeRangeHours,
  onToggleFavorite,
  onRecord,
  isRecorded,
  onPlay,
  onEventClick,
}: EpgTimelineGridProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  
  const timelineWidth = timeRangeHours * PIXELS_PER_HOUR;
  const startTimestampMs = Math.floor(currentTime * 1000 / (30 * 60 * 1000)) * (30 * 60 * 1000);
  
  const ticks = [];
  for (let i = 0; i <= timeRangeHours * 2; i++) {
    const tickTimeMs = startTimestampMs + (i * 30 * 60 * 1000);
    const d = new Date(tickTimeMs);
    const label = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const leftPx = (i * 0.5) * PIXELS_PER_HOUR;
    ticks.push({ label, leftPx });
  }

  const nowLeftPx = ((currentTime * 1000 - startTimestampMs) / (60 * 60 * 1000)) * PIXELS_PER_HOUR;

  return (
    <div className={styles.timelineContainer} ref={containerRef} role="grid" aria-label="EPG Timeline">
      <div className={styles.timelineHeader} style={{ minWidth: timelineWidth + 250 }}>
        <div className={styles.timelineCorner}></div>
        <div className={styles.timelineTimeAxis} style={{ width: timelineWidth }}>
          {ticks.map((tick, i) => (
            <div key={i} className={styles.timelineTimeTick} style={{ left: tick.leftPx }}>
              {tick.label}
            </div>
          ))}
          {/* Current Time Indicator Line */}
          {nowLeftPx >= 0 && nowLeftPx <= timelineWidth && (
            <div className={styles.timelineCurrentTimeIndicator} style={{ left: nowLeftPx }} />
          )}
        </div>
      </div>
      
      <div className={styles.timelineBody}>
        {channels.map((channel) => {
          const ref = channel.serviceRef || channel.id || '';
          const events = eventsByServiceRef.get(ref) || [];
          const isFavorite = favoriteServiceRefs?.has(ref.toLowerCase()) ?? false;
          
          return (
            <EpgTimelineRow
              key={ref}
              channel={channel}
              events={events}
              isFavorite={isFavorite}
              currentTime={currentTime}
              startTimestampMs={startTimestampMs}
              timelineWidth={timelineWidth}
              pixelsPerHour={PIXELS_PER_HOUR}
              onPlay={onPlay}
              onToggleFavorite={onToggleFavorite}
              onRecord={onRecord}
              isRecorded={isRecorded}
              onEventClick={onEventClick}
            />
          );
        })}
      </div>
    </div>
  );
}
