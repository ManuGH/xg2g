// EPG Toolbar - Filter controls and actions
// Zero API imports

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
  const loading = loadState === 'loading';
  const searchLoading = searchLoadState === 'loading';

  return (
    <>
      {/* Header Bar */}
      <div className="epg-toolbar">
        <div className="epg-toolbar-left">
          <h3>EPG Übersicht ({channelCount})</h3>
          <p>Zeitraum: jetzt bis +{filters.timeRange}h</p>
        </div>
        <div className="epg-toolbar-right">
          <button onClick={onRefresh} disabled={loading}>
            Neu laden
          </button>
        </div>
      </div>

      {/* Filter Controls */}
      <div className="epg-controls">
        {bouquets.length > 0 && (
          <label>
            Bouquet:
            <select
              value={filters.bouquetId || ''}
              onChange={(e) => onFilterChange({ bouquetId: e.target.value })}
            >
              <option value="">Alle Sender</option>
              {bouquets.map((b) => (
                <option key={b.name} value={b.name}>
                  {b.name}
                </option>
              ))}
            </select>
          </label>
        )}

        <label>
          Zeitraum:
          <select
            value={filters.timeRange}
            onChange={(e) =>
              onFilterChange({ timeRange: parseInt(e.target.value, 10) })
            }
          >
            <option value={6}>6 Stunden</option>
            <option value={12}>12 Stunden</option>
            <option value={24}>24 Stunden</option>
            <option value={72}>3 Tage</option>
            <option value={168}>7 Tage</option>
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
            placeholder="Suche nach Sendungen (z.B. ZIB)"
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
              <option value="">Alle Bouquets</option>
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
            {searchLoading ? 'Suche …' : 'Suche'}
          </button>
        </div>
      </div>
    </>
  );
}
