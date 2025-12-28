// EPG Toolbar - Filter controls and actions
// Zero API imports

import { useTranslation } from 'react-i18next';
import type { EpgFilters, EpgBouquet, EpgLoadState } from '../types';

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
      <div className="epg-toolbar">
        <div className="epg-toolbar-left">
          <h3>{t('epg.pageTitle', { count: channelCount })}</h3>
          <p>{t('epg.timeRange')}: {t('epg.rangeNowTo' + filters.timeRange + 'h', { defaultValue: 'now to +' + filters.timeRange + 'h' })}</p>
        </div>
        <div className="epg-toolbar-right">
          <button onClick={onRefresh} disabled={loading}>
            {t('epg.reload')}
          </button>
        </div>
      </div>

      {/* Filter Controls */}
      <div className="epg-controls">
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

        <label>
          {t('epg.timeRange')}:
          <select
            value={filters.timeRange}
            onChange={(e) =>
              onFilterChange({ timeRange: parseInt(e.target.value, 10) })
            }
          >
            <option value={6}>6 {t('epg.hours', { count: 6, defaultValue: 'Hours' })}</option>
            <option value={12}>12 {t('epg.hours', { count: 12, defaultValue: 'Hours' })}</option>
            <option value={24}>24 {t('epg.hours', { count: 24, defaultValue: 'Hours' })}</option>
            <option value={72}>3 {t('epg.hours', { count: 72, defaultValue: 'Days' }).replace('Hours', 'Days').replace('Stunden', 'Tage')}</option>
            <option value={168}>7 {t('epg.hours', { count: 168, defaultValue: 'Days' }).replace('Hours', 'Days').replace('Stunden', 'Tage')}</option>
          </select>
        </label>
      </div>

      {/* Search Bar */}
      <div className="epg-search">
        <div className="epg-search-left">
          <div className="epg-search-icon">⏎</div>
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
        <div className="epg-search-right">
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
