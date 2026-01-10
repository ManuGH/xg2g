// EPG Container - React integration and API orchestration
// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useReducer, useEffect, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { epgReducer, createInitialEpgState } from './epgModel';
import { fetchEpgEvents, fetchTimers } from './epgApi';
import { addTimer } from '../../client-ts';
import type { EpgChannel, EpgBouquet, Timer, EpgEvent } from './types';
import { EpgToolbar } from './components/EpgToolbar';
import { EpgChannelList } from './components/EpgChannelList';
import { normalizeEpgText } from '../../utils/text';
import './EPG.css';

const RECORD_SUPPORTED = true; // Feature flag

export interface EpgProps {
  // External dependencies (from AppContext or parent)
  channels: EpgChannel[];
  bouquets?: EpgBouquet[];
  selectedBouquet?: string;
  onSelectBouquet?: (bouquetId: string) => void;
  onPlay?: (channel: EpgChannel) => void;
}

export default function EPG({
  channels,
  bouquets = [],
  onSelectBouquet,
  onPlay,
}: EpgProps) {
  const { t } = useTranslation();
  const [state, dispatch] = useReducer(epgReducer, undefined, createInitialEpgState);
  const [timers, setTimers] = React.useState<Timer[]>([]);

  // ============================================================================
  // Timer Management (for recording feedback)
  // ============================================================================

  const loadTimers = useCallback(async () => {
    try {
      const data = await fetchTimers();
      setTimers(data);
    } catch (err) {
      console.error('Failed to fetch timers for EPG', err);
    }
  }, []);

  useEffect(() => {
    loadTimers();
    const interval = setInterval(loadTimers, 30000); // Poll every 30s
    return () => clearInterval(interval);
  }, [loadTimers]);

  const handleRecord = useCallback(
    async (event: EpgEvent) => {
      // Note: confirm/alert are blocking, but simple enough for this scope.
      // Ideally move to a proper modal or toast.
      if (!confirm(t('epg.confirmRecord', { title: event.title }))) return;

      try {
        await addTimer({
          body: {
            serviceRef: event.service_ref,
            begin: event.start,
            end: event.end,
            name: event.title,
            description: normalizeEpgText(event.desc) || '',
          },
        });
        alert(t('epg.recordSuccess'));
        loadTimers(); // Refresh feedback immediately
      } catch (err: any) {
        console.error(err);
        let msg = err.message || JSON.stringify(err);
        if (err.body?.title) {
          msg = err.body.title;
        } else if (err.body) {
          msg = JSON.stringify(err.body);
        }
        alert(t('epg.recordError', { error: msg }));
      }
    },
    [loadTimers, t]
  );

  const isRecorded = useCallback(
    (event: EpgEvent): boolean => {
      const progRef = event.service_ref;
      return timers.some((t) => {
        const tRef = t.serviceRef || t.serviceref || t.service_ref;
        if (tRef && progRef && tRef !== progRef) return false;
        return t.begin < event.end && t.end > event.start;
      });
    },
    [timers]
  );

  // ============================================================================
  // EPG Data Loading
  // ============================================================================

  const loadEpgEvents = useCallback(async () => {
    dispatch({ type: 'LOAD_START' });
    try {
      const now = Math.floor(Date.now() / 1000);
      const startFetch = now - 7 * 24 * 3600;
      const endFetch = now + 14 * 24 * 3600;

      const events = await fetchEpgEvents({
        from: startFetch,
        to: endFetch,
        bouquet: state.filters.bouquetId || undefined,
      });

      dispatch({ type: 'LOAD_SUCCESS', payload: { events } });
    } catch (err: any) {
      console.error(err);
      // We need to resolve language for error string here or just set a key if reducer supported it?
      // Reducer takes string. We should use a key or translate here?
      // Since reducer state.error is displayed directly, we should translate here OR store key.
      // Storing translated string is easier for now given existing typing.
      // But hooks can change language... if we store "EPG failed" and switch lang, it stays "EPG failed".
      // Ideally we store error CODE and translate in render.
      // For now, let's translate here, user will refresh on error anyway.
      // wait, useCallback dep needs 't'.
      // PROPER FIX: Pass error CODE/KEY to state, translate in render.
      // But invalidating typing... let's hack: use unique error keys and checking in render?
      // Or just ignore hot-swapping language for errors.
      dispatch({ type: 'LOAD_ERROR', payload: { error: 'epg.loadError' } });
    }
  }, [state.filters.timeRange, state.filters.bouquetId]);

  // Initial load + auto-refresh every 5 minutes
  useEffect(() => {
    loadEpgEvents();
    const interval = setInterval(loadEpgEvents, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, [loadEpgEvents]);

  // ============================================================================
  // Search Logic
  // ============================================================================

  const runSearch = useCallback(async () => {
    const query = state.filters.query?.trim();
    if (!query) return;

    dispatch({ type: 'SEARCH_START' });
    try {
      const events = await fetchEpgEvents({
        bouquet: state.filters.bouquetId || undefined,
        query,
      });

      dispatch({ type: 'SEARCH_SUCCESS', payload: { events } });
    } catch (err: any) {
      console.error(err);
      dispatch({ type: 'SEARCH_ERROR', payload: { error: 'epg.searchError' } });
    }
  }, [state.filters.query, state.filters.bouquetId]);

  // Clear search when query is emptied
  useEffect(() => {
    if (!state.filters.query?.trim()) {
      dispatch({ type: 'SEARCH_CLEAR' });
    }
  }, [state.filters.query]);

  // ============================================================================
  // Current Time Ticker (for progress bars)
  // ============================================================================

  useEffect(() => {
    const interval = setInterval(() => {
      dispatch({ type: 'UPDATE_TIME', payload: { currentTime: Math.floor(Date.now() / 1000) } });
    }, 60_000); // Update every minute
    return () => clearInterval(interval);
  }, []);

  // ============================================================================
  // Data Preparation for UI Components
  // ============================================================================

  // Group events by service_ref for efficient lookup
  const mainEventsByServiceRef = useMemo(() => {
    const map = new Map<string, EpgEvent[]>();
    state.events.forEach((event) => {
      const ref = event.service_ref;
      if (!map.has(ref)) map.set(ref, []);
      map.get(ref)!.push(event);
    });

    // Sort events within each channel chronologically
    for (const events of map.values()) {
      events.sort((a, b) => a.start - b.start);
    }

    return map;
  }, [state.events]);

  const searchEventsByServiceRef = useMemo(() => {
    const map = new Map<string, EpgEvent[]>();
    state.searchEvents.forEach((event) => {
      const ref = event.service_ref;
      if (!map.has(ref)) map.set(ref, []);
      map.get(ref)!.push(event);
    });
    return map;
  }, [state.searchEvents]);

  // ============================================================================
  // Render
  // ============================================================================

  const handleFilterChange = useCallback((updates: Partial<typeof state.filters>) => {
    dispatch({ type: 'SET_FILTER', payload: updates });

    // Sync bouquet selection with parent (if provided)
    if (updates.bouquetId !== undefined && onSelectBouquet) {
      onSelectBouquet(updates.bouquetId);
    }
  }, [onSelectBouquet]);

  const handleToggleChannel = useCallback((serviceRef: string) => {
    dispatch({ type: 'TOGGLE_CHANNEL', payload: { channelId: serviceRef } });
  }, []);

  const handleToggleSearchChannel = useCallback((serviceRef: string) => {
    dispatch({ type: 'TOGGLE_SEARCH_CHANNEL', payload: { channelId: serviceRef } });
  }, []);

  const showSearchResults = state.searchLoadState === 'ready' && state.searchEvents.length > 0;
  const showMainList = state.loadState === 'ready' && !showSearchResults;

  return (
    <div className="epg-page">
      {/* Toolbar */}
      <EpgToolbar
        channelCount={channels.length}
        filters={state.filters}
        bouquets={bouquets}
        loadState={state.loadState}
        searchLoadState={state.searchLoadState}
        onFilterChange={handleFilterChange}
        onRefresh={loadEpgEvents}
        onSearch={runSearch}
      />

      {/* Search Error */}
      {state.searchError && <div className="epg-card epg-error">{t(state.searchError)}</div>}

      {/* Search No Results */}
      {state.searchLoadState === 'ready' &&
        state.searchEvents.length === 0 &&
        !state.searchError &&
        state.filters.query?.trim() && (
          <div className="epg-card">
            {t('epg.noResults', { query: state.filters.query.trim() })}
          </div>
        )}

      {/* Search Results */}
      {showSearchResults && (
        <div className="epg-card">
          <div className="epg-channel">
            <div className="epg-channel-meta">
              <div className="epg-channel-name">
                {t('epg.searchResults', { query: state.filters.query?.trim() })}
              </div>
            </div>
          </div>
          <div className="epg-programmes">
            <EpgChannelList
              mode="search"
              channels={channels}
              eventsByServiceRef={searchEventsByServiceRef}
              currentTime={state.currentTime}
              expandedChannels={state.expandedSearchChannels}
              onToggleExpand={handleToggleSearchChannel}
              onPlay={onPlay}
              onRecord={RECORD_SUPPORTED ? handleRecord : undefined}
              isRecorded={RECORD_SUPPORTED ? isRecorded : undefined}
            />
          </div>
        </div>
      )}

      {/* Main View Loading/Error */}
      {state.loadState === 'loading' && <div className="epg-card">{t('epg.loading')}</div>}
      {state.error && <div className="epg-card epg-error">{t(state.error)}</div>}

      {/* Main Channel List */}
      {showMainList && (
        <EpgChannelList
          mode="main"
          channels={channels}
          eventsByServiceRef={mainEventsByServiceRef}
          currentTime={state.currentTime}
          expandedChannels={state.expandedChannels}
          onToggleExpand={handleToggleChannel}
          onPlay={onPlay}
          onRecord={RECORD_SUPPORTED ? handleRecord : undefined}
          isRecorded={RECORD_SUPPORTED ? isRecorded : undefined}
        />
      )}
    </div>
  );
}
