import { useMemo, type MouseEvent } from 'react';
import { useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { ROUTE_MAP } from '../../routes';
import { useAppContext } from '../../context/AppContext';
import { useUiOverlay } from '../../context/UiOverlayContext';
import { useContinueWatching, continueWatchingQueryKey } from './useContinueWatching';
import { saveResume, type ContinueWatchingItem } from './api';
import { filterAndDeduplicateContinueWatching, progressPercent } from './normalizeResume';
import ContinueWatchingThumbnail from './ContinueWatchingThumbnail';
import styles from './ContinueWatchingRail.module.css';

const MAX_DISPLAY_ITEMS = 4;
const FETCH_LIMIT = 12;

function formatRemaining(item: ContinueWatchingItem, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const d = item.durationSeconds ?? 0;
  if (d <= 0) return '';
  const remainingMin = Math.max(1, Math.round((d - item.posSeconds) / 60));
  return remainingMin === 1
    ? t('dashboard.continueWatching.remainingOne', { defaultValue: 'noch 1 Minute' })
    : t('dashboard.continueWatching.remaining', { count: remainingMin, minutes: remainingMin, defaultValue: `noch ${remainingMin} Minuten` });
}

export default function ContinueWatchingRail() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { auth } = useAppContext();
  const queryClient = useQueryClient();
  const { toast } = useUiOverlay();

  const { data } = useContinueWatching(FETCH_LIMIT);

  // Deduplicate series episodes, drop finished items, cap to max 4 cards
  const items = useMemo(
    () => filterAndDeduplicateContinueWatching(data ?? [], MAX_DISPLAY_ITEMS),
    [data],
  );

  if (items.length === 0) {
    return null;
  }

  const openRecording = (item: ContinueWatchingItem) => {
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

  const handleDismiss = async (e: MouseEvent, item: ContinueWatchingItem) => {
    e.preventDefault();
    e.stopPropagation();

    // Optimistically update query cache
    queryClient.setQueriesData(
      { queryKey: continueWatchingQueryKey },
      (old: ContinueWatchingItem[] | undefined) =>
        (old ?? []).filter((i) => i.recordingId !== item.recordingId),
    );

    try {
      await saveResume(item.recordingId, { position: 0, finished: true });
      toast({
        kind: 'info',
        message: t('dashboard.continueWatching.dismissed', {
          title: item.title || t('dashboard.continueWatching.untitled'),
          defaultValue: 'Aus „Weiter schauen“ entfernt (Aufnahme bleibt gespeichert)',
        }),
      });
    } catch {
      void queryClient.invalidateQueries({ queryKey: continueWatchingQueryKey });
    }
  };

  return (
    <section className={styles.rail} aria-label={t('dashboard.continueWatching.title')}>
      <div className={styles.header}>
        <h2 className={styles.title}>{t('dashboard.continueWatching.title')}</h2>
        <button
          type="button"
          className={styles.allLink}
          onClick={() => navigate(ROUTE_MAP.recordings)}
        >
          {t('dashboard.continueWatching.allRecordings', { defaultValue: 'Alle Aufnahmen' })} &rarr;
        </button>
      </div>
      <div className={styles.grid} role="list">
        {items.map((item) => {
          const percent = progressPercent(item);
          const remainingText = formatRemaining(item, t);

          return (
            <article
              key={item.recordingId}
              className={styles.item}
              role="listitem"
              tabIndex={0}
              onClick={() => openRecording(item)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  openRecording(item);
                }
              }}
              data-testid="continue-watching-item"
            >
              <div className={styles.previewStage}>
                <ContinueWatchingThumbnail
                  recordingId={item.recordingId}
                  title={item.title}
                  authToken={auth.token}
                />
                <div className={styles.stageOverlay} aria-hidden="true" />

                {/* Hover play icon */}
                <div className={styles.playOverlay} aria-hidden="true">
                  <div className={styles.playCircle}>
                    <svg viewBox="0 0 24 24" fill="currentColor" className={styles.playIcon}>
                      <polygon points="6 3 20 12 6 21 6 3" />
                    </svg>
                  </div>
                </div>

                {/* Dismiss button */}
                <button
                  type="button"
                  className={styles.dismissButton}
                  onClick={(e) => { void handleDismiss(e, item); }}
                  title={t('dashboard.continueWatching.dismissTooltip', { defaultValue: 'Aus „Weiter schauen“ entfernen (Aufnahme bleibt erhalten)' })}
                  aria-label={t('dashboard.continueWatching.dismissTooltip', { defaultValue: 'Aus „Weiter schauen“ entfernen (Aufnahme bleibt erhalten)' })}
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={styles.dismissIcon}>
                    <line x1="18" y1="6" x2="6" y2="18" />
                    <line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                </button>

                {/* Progress bar */}
                {percent > 0 && (
                  <div className={styles.progressTrack} aria-hidden="true">
                    <div
                      className={styles.progressFill}
                      style={{ width: `${percent}%` }}
                    />
                  </div>
                )}
              </div>

              <div className={styles.cardContent}>
                <h3 className={styles.itemTitle}>
                  {item.title || t('dashboard.continueWatching.untitled')}
                </h3>
                <div className={styles.itemMeta}>
                  {[item.channel, remainingText].filter(Boolean).join(' · ')}
                </div>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}
