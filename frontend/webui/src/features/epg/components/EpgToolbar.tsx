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
  searchLoadState,
  onFilterChange,
  onRefresh,
  onToggleFavorites,
  onSearch,
}: EpgToolbarProps) {
  const { t } = useTranslation();
  const isTvHost = React.useMemo(() => resolveHostEnvironment().isTv, []);
  const searchInputRef = React.useRef<HTMLInputElement | null>(null);
  const [tvSearchEditing, setTvSearchEditing] = React.useState(false);
  const loading = loadState === 'loading';
  const searchLoading = searchLoadState === 'loading';
  const rangeLabel = t('epg.rangeNowTo' + filters.timeRange + 'h', {
    defaultValue: 'now to +' + filters.timeRange + 'h'
  });
  const activeBouquet = bouquets.find((b) => b.name === filters.bouquetId)?.name || t('epg.allServices');
  const dateLabel = new Intl.DateTimeFormat(undefined, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
  }).format(new Date());

  React.useEffect(() => {
    if (!isTvHost || !tvSearchEditing) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      searchInputRef.current?.focus();
    });

    return () => window.cancelAnimationFrame(frame);
  }, [isTvHost, tvSearchEditing]);

  return (
    <section className={styles.toolbar}>
      <div className={styles.toolbarHero}>
        <div className={styles.toolbarLeft}>
          <p className={styles.toolbarEyebrow}>{t('epg.pageTitleEyebrow', { defaultValue: 'Live guide' })}</p>
          <h3 className={styles.toolbarTitle}>{t('epg.pageTitle', { count: channelCount })}</h3>
          <p className={styles.toolbarSummary}>
            {t('epg.timeRange')}: {rangeLabel} · {dateLabel}
          </p>
        </div>
        <div className={styles.toolbarRight}>
          <button
            onClick={onToggleFavorites}
            disabled={favoriteCount === 0}
            aria-pressed={showFavoritesOnly}
          >
            <span className={styles.actionIcon} aria-hidden="true">{showFavoritesOnly ? '★' : '☆'}</span>
            <span className={styles.actionLabel}>
              {showFavoritesOnly
                ? t('epg.favoritesOn', { defaultValue: 'Favoriten' })
                : t('epg.favoritesOff', { defaultValue: 'Favoritenfilter' })}
            </span>
          </button>
          <button onClick={onRefresh} disabled={loading} aria-label={t('epg.reload')}>
            <span className={styles.actionIcon} aria-hidden="true">↻</span>
            <span className={styles.actionLabel}>{t('epg.reload')}</span>
          </button>
        </div>
      </div>

      <div className={styles.toolbarMeta}>
        <div className={styles.metaPill}>
          <span className={styles.metaLabel}>{t('epg.allServices')}</span>
          <span className={styles.metaValue}>{channelCount}</span>
        </div>
        <div className={styles.metaPill}>
          <span className={styles.metaLabel}>{t('epg.bouquet')}</span>
          <span className={styles.metaValue}>{activeBouquet}</span>
        </div>
        <div className={styles.metaPill}>
          <span className={styles.metaLabel}>{t('epg.timeRange')}</span>
          <span className={styles.metaValue}>{rangeLabel}</span>
        </div>
        <div className={styles.metaPill}>
          <span className={styles.metaLabel}>{t('epg.favorites', { defaultValue: 'Favoriten' })}</span>
          <span className={styles.metaValue}>{favoriteCount}</span>
        </div>
      </div>

      <div className={styles.search}>
        <div className={styles.searchLeft}>
          {isTvHost ? (
            tvSearchEditing ? (
              <>
                <div className={styles.searchIcon}>⌕</div>
                <input
                  ref={searchInputRef}
                  type="text"
                  value={filters.query || ''}
                  onChange={(e) => {
                    onFilterChange({ query: e.target.value });
                  }}
                  placeholder={t('epg.searchServices')}
                  onBlur={() => setTvSearchEditing(false)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      if (filters.query?.trim() && onSearch) {
                        onSearch();
                      }
                      setTvSearchEditing(false);
                      e.currentTarget.blur();
                    }
                    if (e.key === 'Escape') {
                      setTvSearchEditing(false);
                      e.currentTarget.blur();
                    }
                  }}
                />
              </>
            ) : (
              <button
                type="button"
                className={styles.tvSearchLauncher}
                onClick={() => setTvSearchEditing(true)}
                aria-label={t('epg.searchServices')}
              >
                <span className={styles.searchIcon} aria-hidden="true">⌕</span>
                <span className={styles.actionLabel}>
                  {filters.query?.trim() || t('epg.searchServices')}
                </span>
              </button>
            )
          ) : (
            <>
              <div className={styles.searchIcon}>⌕</div>
              <input
                type="text"
                value={filters.query || ''}
                onChange={(e) => {
                  const val = e.target.value;
                  onFilterChange({ query: val });
                }}
                placeholder={t('epg.searchServices')}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && filters.query?.trim() && onSearch) {
                    onSearch();
                  }
                }}
              />
            </>
          )}
        </div>
        <div className={styles.searchRight}>
          <button
            onClick={onSearch}
            disabled={searchLoading || !filters.query?.trim()}
            aria-label={t('epg.search')}
          >
            <span className={styles.actionIcon} aria-hidden="true">{searchLoading ? '…' : '⌕'}</span>
            <span className={styles.actionLabel}>{searchLoading ? t('common.loading') : t('epg.search')}</span>
          </button>
        </div>
      </div>

      <div className={styles.controls}>
        {bouquets.length > 0 && (
          <label className={styles.filterField}>
            <span>{t('epg.bouquet')}</span>
            <select
              value={filters.bouquetId || ''}
              onChange={(e) => onFilterChange({ bouquetId: e.target.value })}
            >
              <option value="">{t('epg.allServices')}</option>
              {bouquets.map((b) => (
                <option key={b.name} value={b.name}>
                  {b.name}
                </option>
              ))}
            </select>
          </label>
        )}

        <div className={styles.rangeGroup}>
          <label>{t('epg.timeRange')}</label>
          <div className={styles.pills}>
            {[
              { label: t('epg.rangeNow', { defaultValue: 'Now' }), value: 6 },
              { label: t('epg.rangeEvening', { defaultValue: 'Evening' }), value: 12 },
              { label: t('epg.rangeDay', { defaultValue: 'Day' }), value: 24 },
              { label: t('epg.rangeWeek', { defaultValue: 'Week' }), value: 168 },
              { label: t('epg.rangeAll', { defaultValue: 'All' }), value: EPG_MAX_HORIZON_HOURS },
            ].map((opt) => (
              <button
                key={opt.value}
                className={[styles.pill, filters.timeRange === opt.value ? styles.pillActive : null].filter(Boolean).join(' ')}
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
