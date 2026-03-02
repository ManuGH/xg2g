// EPG Toolbar - Filter controls and actions
// Zero API imports

import { useTranslation } from 'react-i18next';
import type { EpgFilters, EpgBouquet, EpgLoadState } from '../types';
import { EPG_MAX_HORIZON_HOURS } from '../types';
import styles from '../EPG.module.css';

export interface EpgToolbarProps {
  channelCount: number;
  filters: EpgFilters;
  bouquets: EpgBouquet[];
  loadState: EpgLoadState;
  searchLoadState: EpgLoadState;

  onFilterChange: (updates: Partial<EpgFilters>) => void;
  onRefresh: () => void;
  onSearch?: () => void;
}

export function EpgToolbar({
  channelCount,
  filters,
  bouquets,
  loadState,
  searchLoadState,
  onFilterChange,
  onRefresh,
  onSearch,
}: EpgToolbarProps) {
  const { t } = useTranslation();
  const loading = loadState === 'loading';
  const searchLoading = searchLoadState === 'loading';

  return (
    <>
      {/* Header Bar */}
      <div className={styles.toolbar}>
        <div className={styles.toolbarLeft}>
          <h3>{t('epg.pageTitle', { count: channelCount })}</h3>
          <p>{t('epg.timeRange')}: {t('epg.rangeNowTo' + filters.timeRange + 'h', { defaultValue: 'now to +' + filters.timeRange + 'h' })}</p>
        </div>
        <div className={styles.toolbarRight}>
          <button onClick={onRefresh} disabled={loading}>
            {t('epg.reload')}
          </button>
        </div>
      </div>

      {/* Filter Controls */}
      <div className={styles.controls}>
        {bouquets.length > 0 && (
          <label>
            {t('epg.bouquet')}:
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

        <label>{t('epg.timeRange')}:</label>
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

      {/* Search Bar */}
      <div className={styles.search}>
        <div className={styles.searchLeft}>
          <div className={styles.searchIcon}>⏎</div>
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
          {bouquets.length > 0 && (
            <select
              value={filters.bouquetId || ''}
              onChange={(e) => onFilterChange({ bouquetId: e.target.value })}
            >
              <option value="">{t('epg.allBouquets')}</option>
              {bouquets.map((b) => (
                <option key={b.name} value={b.name}>
                  {b.name}
                </option>
              ))}
            </select>
          )}
        </div>
        <div className={styles.searchRight}>
          <button
            onClick={onSearch}
            disabled={searchLoading || !filters.query?.trim()}
          >
            {searchLoading ? t('common.loading') : t('epg.search')}
          </button>
        </div>
      </div>
    </>
  );
}
