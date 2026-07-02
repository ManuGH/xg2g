import { useMemo, type CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ROUTE_MAP } from '../../routes';
import { Card } from '../../components/ui';
import { useContinueWatching } from './useContinueWatching';
import type { ContinueWatchingItem } from './api';
import styles from './ContinueWatchingRail.module.css';

const RAIL_LIMIT = 8;

function progressPercent(item: ContinueWatchingItem): number {
  const d = item.durationSeconds ?? 0;
  if (d <= 0) return 0;
  // display-only: progress-bar normalization for rendering.
  return Math.round(Math.max(0, Math.min(1, item.posSeconds / d)) * 100);
}

function formatRemaining(item: ContinueWatchingItem, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const d = item.durationSeconds ?? 0;
  if (d <= 0) return '';
  const remainingMin = Math.max(1, Math.round((d - item.posSeconds) / 60));
  return t('dashboard.continueWatching.remaining', { minutes: remainingMin });
}

export default function ContinueWatchingRail() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data } = useContinueWatching(RAIL_LIMIT);

  const items = useMemo(
    () => (data ?? []).filter((item) => item.posSeconds >= 15),
    [data],
  );

  if (items.length === 0) {
    // No section chrome for an empty rail: the dashboard stays quiet until
    // there is actually something to resume.
    return null;
  }

  const openRecording = (item: ContinueWatchingItem) => {
    // Title/duration ride along so the listing can synthesize a playable item
    // when the recording lives in a subfolder outside the loaded directory.
    const params = new URLSearchParams({ play: item.recordingId });
    if (item.posSeconds > 0) {
      params.set('pos', String(Math.floor(item.posSeconds)));
    }
    if (item.title) {
      params.set('title', item.title);
    }
    if (item.durationSeconds && item.durationSeconds > 0) {
      params.set('duration', String(Math.floor(item.durationSeconds)));
    }
    navigate(`${ROUTE_MAP.recordings}?${params.toString()}`);
  };

  return (
    <section className={styles.rail} aria-label={t('dashboard.continueWatching.title')}>
      <div className={styles.header}>
        <h3 className={styles.title}>{t('dashboard.continueWatching.title')}</h3>
      </div>
      <div className={styles.track} role="list">
        {items.map((item) => {
          const percent = progressPercent(item);
          return (
            <Card
              key={item.recordingId}
              className={styles.item}
              onClick={() => openRecording(item)}
            >
              <div className={styles.itemBody} data-testid="continue-watching-item">
                <span className={styles.itemTitle}>
                  {item.title || t('dashboard.continueWatching.untitled')}
                </span>
                <span className={styles.itemMeta}>
                  {[item.channel, formatRemaining(item, t)].filter(Boolean).join(' · ')}
                </span>
              </div>
              {percent > 0 && (
                <div className={styles.progressTrack} aria-hidden="true">
                  <div
                    className={styles.progressFill}
                    style={{ '--xg2g-resume-progress': `${percent}%` } as CSSProperties}
                  />
                </div>
              )}
            </Card>
          );
        })}
      </div>
    </section>
  );
}
