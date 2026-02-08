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
import { EPG_MAX_HORIZON_HOURS } from './types';
import { EpgToolbar } from './components/EpgToolbar';
import { EpgChannelList } from './components/EpgChannelList';
import { normalizeEpgText } from '../../utils/text';
import { debugError, debugLog, formatError } from '../../utils/logging';
import { useUiOverlay } from '../../context/UiOverlayContext';
import styles from './EPG.module.css';

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
  const { confirm, toast } = useUiOverlay();
  const [state, dispatch] = useReducer(epgReducer, undefined, createInitialEpgState);
  const [timers, setTimers] = React.useState<Timer[]>([]);
  const abortControllerRef = React.useRef<AbortController | null>(null);

  // ============================================================================
  // Timer Management (for recording feedback)
  // ============================================================================

  const loadTimers = useCallback(async () => {
    try {
      const data = await fetchTimers();
      setTimers(data);
    } catch (err) {
      debugError('Failed to fetch timers for EPG', formatError(err));
    }
  }, []);

  useEffect(() => {
    loadTimers();
    const interval = setInterval(loadTimers, 30000); // Poll every 30s
    return () => clearInterval(interval);
  }, [loadTimers]);

  const handleRecord = useCallback(
    async (event: EpgEvent) => {
      const ok = await confirm({
        title: 'Schedule Recording',
        message: t('epg.confirmRecord', { title: event.title }),
        confirmLabel: 'Record',
        cancelLabel: 'Cancel',
        tone: 'action',
      });
      if (!ok) return;

      try {
        await addTimer({
          body: {
            serviceRef: event.serviceRef,
            begin: event.start,
            end: event.end,
            name: event.title,
            description: normalizeEpgText(event.desc) || '',
          },
        });
        toast({ kind: 'success', message: t('epg.recordSuccess') });
        loadTimers(); // Refresh feedback immediately
      } catch (err: any) {
        debugError(formatError(err));
        let msg = err.message || JSON.stringify(err);
        if (err.body?.title) {
          msg = err.body.title;
        } else if (err.body) {
          msg = JSON.stringify(err.body);
        }
        toast({ kind: 'error', message: t('epg.recordError', { error: msg }) });
      }
    },
    [confirm, loadTimers, t, toast]
  );

  const isRecorded = useCallback(
    (event: EpgEvent): boolean => {
      const progRef = event.serviceRef;
      return timers.some((t) => {
        const tRef = t.serviceRef;
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
    // Race Control: Abort previous request
    abortControllerRef.current?.abort();
    abortControllerRef.current = new AbortController();
    const { signal } = abortControllerRef.current;

    dispatch({ type: 'LOAD_START' });
    try {
      // Pin now per load cycle
      const now = Math.floor(Date.now() / 1000);

      // Compute range (Force cap for safety)
      const rangeHours = Math.min(state.filters.timeRange, EPG_MAX_HORIZON_HOURS);
      const fetchFrom = now - 4 * 3600;
      const fetchTo = now + rangeHours * 3600;

      const events = await fetchEpgEvents({
        from: fetchFrom,
        to: fetchTo,
        bouquet: state.filters.bouquetId || undefined,
        signal,
      });

      if (signal.aborted) return;

      // Observability (DEV only)
      const isDev = (import.meta as any).env?.DEV;
      if (isDev) {
        debugLog(
          'EPG Load [%s]',
          state.filters.timeRange === 336 ? 'All' : `${state.filters.timeRange}h`
        );
        debugLog(
          'Window: %s to %s',
          new Date(fetchFrom * 1000).toISOString(),
          new Date(fetchTo * 1000).toISOString()
        );
        debugLog('Events received: %d', events.length);
      }

      dispatch({ type: 'LOAD_SUCCESS', payload: { events } });
    } catch (err: any) {
      if (err.name === 'AbortError') return;
      debugError('EPG load failed:', formatError(err));
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
      debugError(formatError(err));
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

  // Group events by serviceRef for efficient lookup
  const mainEventsByServiceRef = useMemo(() => {
    const map = new Map<string, EpgEvent[]>();
    state.events.forEach((event) => {
      const ref = event.serviceRef;
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
      const ref = event.serviceRef;
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
    <div className={[styles.page, 'animate-enter'].join(' ')}>
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
      {state.searchError && <div className={[styles.card, styles.cardError].join(' ')}>{t(state.searchError)}</div>}

      {/* Search No Results */}
      {state.searchLoadState === 'ready' &&
        state.searchEvents.length === 0 &&
        !state.searchError &&
        state.filters.query?.trim() && (
          <div className={styles.card}>
            {t('epg.noResults', { query: state.filters.query.trim() })}
          </div>
        )}

      {/* Search Results */}
      {showSearchResults && (
        <div className={styles.card}>
          <div className={styles.channel}>
            <div className={styles.channelMeta}>
              <div className={styles.channelName}>
                {t('epg.searchResults', { query: state.filters.query?.trim() })}
              </div>
            </div>
          </div>
          <div className={styles.programmes}>
            <EpgChannelList
              mode="search"
              channels={channels}
              eventsByServiceRef={searchEventsByServiceRef}
              currentTime={state.currentTime}
              timeRangeHours={state.filters.timeRange}
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
      {state.loadState === 'loading' && <div className={styles.card}>{t('epg.loading')}</div>}
      {state.error && <div className={[styles.card, styles.cardError].join(' ')}>{t(state.error)}</div>}

      {/* Main Channel List */}
      {showMainList && (
        <EpgChannelList
          mode="main"
          channels={channels}
          eventsByServiceRef={mainEventsByServiceRef}
          currentTime={state.currentTime}
          timeRangeHours={state.filters.timeRange}
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
