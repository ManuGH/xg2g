// EPG Toolbar - Filter controls and actions
// Zero API imports

import React from 'react';
import { useTranslation } from 'react-i18next';
import type { EpgFilters, EpgBouquet, EpgLoadState } from '../types';
import { EPG_MAX_HORIZON_HOURS } from '../types';
import { resolveHostEnvironment } from '../../../lib/hostBridge';
import styles from '../EPG.module.css';

export interface EpgToolbarProps {
  channelCount: number;
  favoriteCount: number;
  showFavoritesOnly: boolean;
  filters: EpgFilters;
  bouquets: EpgBouquet[];
  loadState: EpgLoadState;
  searchLoadState: EpgLoadState;
  extraActions?: React.ReactNode;

  onFilterChange: (updates: Partial<EpgFilters>) => void;
  onRefresh: () => void;
  onToggleFavorites: () => void;
  onSearch?: () => void;
}

export function EpgToolbar({
  channelCount,
  favoriteCount,
  showFavoritesOnly,
  filters,
  bouquets,
  loadState,
  extraActions,
  onFilterChange,
  onRefresh,
  onToggleFavorites,
  onSearch,
}: EpgToolbarProps) {
  const { t } = useTranslation();
  const isTvHost = React.useMemo(() => resolveHostEnvironment().isTv, []);
  const loading = loadState === 'loading';
  const dateLabel = new Intl.DateTimeFormat(undefined, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
  }).format(new Date());

  React.useEffect(() => {
    if (!isTvHost) {
      return;
    }
  }, [isTvHost]);

  return (
    <section className={styles.toolbarCompact}>
      {/* Row 1: Studio Header & Primary Action Buttons */}
      <div className={styles.toolbarHeaderRow}>
        <div className={styles.toolbarTitleGroup}>
          <div className={styles.toolbarTitleText}>
            <span className={styles.toolbarEyebrow}>{t('epg.pageTitleEyebrow', { defaultValue: 'LIVE GUIDE' })}</span>
            <h3 className={styles.toolbarTitle}>{t('epg.pageTitle', { count: channelCount })}</h3>
          </div>
          <div className={styles.toolbarStatsStrip}>
            <span className={styles.statBadge}>
              <strong>{channelCount}</strong> {t('epg.allServices', { defaultValue: 'Sender' })}
            </span>
            <span className={styles.statBadge}>
              ★ <strong>{favoriteCount}</strong> {t('epg.favorites', { defaultValue: 'Favoriten' })}
            </span>
            <span className={styles.statBadgeDate}>{dateLabel}</span>
          </div>
        </div>

        <div className={styles.toolbarActionsGroup}>
          {extraActions}
          <button
            type="button"
            className={[
              styles.toolbarActionBtn,
              showFavoritesOnly ? styles.toolbarActionBtnActive : null,
            ].filter(Boolean).join(' ')}
            onClick={onToggleFavorites}
            disabled={favoriteCount === 0}
            aria-pressed={showFavoritesOnly}
          >
            <span className={styles.actionIcon} aria-hidden="true">{showFavoritesOnly ? '★' : '☆'}</span>
            <span className={styles.actionLabel}>
              {showFavoritesOnly
                ? t('epg.favoritesOn', { defaultValue: 'Favoriten aktiv' })
                : t('epg.favoritesOff', { defaultValue: 'Favoritenfilter' })}
            </span>
          </button>
          <button
            type="button"
            className={styles.toolbarActionBtn}
            onClick={onRefresh}
            disabled={loading}
            aria-label={t('epg.reload')}
          >
            <span className={styles.actionIcon} aria-hidden="true">↻</span>
            <span className={styles.actionLabel}>{t('epg.reload', { defaultValue: 'Neu laden' })}</span>
          </button>
        </div>
      </div>

      {/* Row 2: Unified Frosted Glass Filter & Search Strip */}
      <div className={styles.toolbarFilterStrip}>
        <div className={styles.filterSearchBox}>
          <span className={styles.filterSearchIcon}>⌕</span>
          <input
            type="text"
            className={styles.filterSearchInput}
            value={filters.query || ''}
            onChange={(e) => onFilterChange({ query: e.target.value })}
            placeholder={t('epg.searchServices', { defaultValue: 'Suche nach Sendungen (z.B. ZIB)...' })}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && filters.query?.trim() && onSearch) {
                onSearch();
              }
            }}
          />
          {filters.query && (
            <button
              type="button"
              className={styles.filterSearchClear}
              onClick={() => onFilterChange({ query: '' })}
              aria-label="Löschen"
            >
              ✕
            </button>
          )}
        </div>

        <div className={styles.filterControlsRight}>
          {bouquets.length > 0 && (
            <div className={styles.filterSelectGroup}>
              <span className={styles.filterSelectIcon}>📁</span>
              <select
                className={styles.filterSelect}
                value={filters.bouquetId || ''}
                onChange={(e) => onFilterChange({ bouquetId: e.target.value })}
              >
                <option value="">{t('epg.allServices', { defaultValue: 'Alle Sender' })}</option>
                {bouquets.map((b) => (
                  <option key={b.name} value={b.name}>
                    {b.name}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div className={styles.filterTimePills}>
            {[
              { label: t('epg.rangeNow', { defaultValue: 'Jetzt' }), value: 6 },
              { label: t('epg.rangeDay', { defaultValue: '24h' }), value: 24 },
              { label: t('epg.rangeWeek', { defaultValue: '7d' }), value: 168 },
              { label: t('epg.rangeAll', { defaultValue: 'Alle' }), value: EPG_MAX_HORIZON_HOURS },
            ].map((opt) => (
              <button
                key={opt.value}
                type="button"
                className={[
                  styles.timePill,
                  filters.timeRange === opt.value ? styles.timePillActive : null,
                ].filter(Boolean).join(' ')}
                onClick={() => onFilterChange({ timeRange: opt.value })}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
