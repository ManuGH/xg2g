// EPG Container - React integration and API orchestration
// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useReducer, useEffect, useCallback, useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { epgReducer, createInitialEpgState } from './epgModel';
import { fetchEpgEvents, fetchTimers } from './epgApi';
import { addTimer } from '../../client-ts';
import { throwOnClientResultError } from '../../services/clientWrapper';
import { useHouseholdProfiles } from '../../context/HouseholdProfilesContext';
import type { EpgChannel, EpgBouquet, Timer, EpgEvent, EpgFilters } from './types';
import { EPG_MAX_HORIZON_HOURS } from './types';
import { EpgToolbar } from './components/EpgToolbar';
import { EpgChannelList } from './components/EpgChannelList';
import { EpgTimelineGrid } from './components/EpgTimelineGrid';
import { EpgEventDialog } from './components/EpgEventDialog';
import ErrorPanel from '../../components/ErrorPanel';
import LoadingSkeleton from '../../components/LoadingSkeleton';
import SectionContextBar from '../../components/SectionContextBar';
import { toAppError } from '../../lib/appErrors';
import { isUnauthorizedError } from '../player/sessionEvents';
import { normalizeEpgText } from '../../utils/text';
import { debugError, debugLog, formatError } from '../../utils/logging';
import { useUiOverlay } from '../../context/UiOverlayContext';
import { useUiSurface } from '../../context/UiSurfaceContext';
import type { AppError } from '../../types/errors';
import Timers from '../../components/Timers';
import {
  buildEpgRoute,
  type EpgSection,
} from '../../routes';
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

function getEpgErrorKey(status?: number, fallbackKey: string = 'epg.loadError'): string {
  if (status === 403) {
    return 'player.forbidden';
  }
  return fallbackKey;
}

function createEpgError(
  error: unknown,
  t: (key: string, options?: Record<string, unknown>) => string,
  fallbackKey: 'epg.loadError' | 'epg.searchError'
): AppError {
  const status =
    typeof error === 'object' && error !== null && 'status' in error && typeof (error as { status?: unknown }).status === 'number'
      ? (error as { status: number }).status
      : undefined;

  if (status === 403) {
    return toAppError(error, {
      fallbackTitle: t(getEpgErrorKey(status, fallbackKey)),
      retryable: false,
    });
  }

  return toAppError(error, {
    fallbackTitle: t(fallbackKey),
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function stringifyUnknown(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value instanceof Error && value.message) return value.message;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function formatTimerCreateError(error: unknown): string {
  const body = isRecord(error) ? error.body : undefined;
  if (isRecord(body) && typeof body.title === 'string' && body.title) {
    return body.title;
  }
  if (body !== undefined) {
    return stringifyUnknown(body);
  }
  return stringifyUnknown(error);
}

function isAbortError(error: unknown): boolean {
  return isRecord(error) && error.name === 'AbortError';
}

const EPG_VIEW_MODE_STORAGE_KEY = 'xg2g:epg:viewMode';
const EPG_GRID_MAX_RANGE_HOURS = 48;

function readStoredEpgViewMode(): 'list' | 'grid' | null {
  try {
    const stored = window.localStorage.getItem(EPG_VIEW_MODE_STORAGE_KEY);
    return stored === 'list' || stored === 'grid' ? stored : null;
  } catch {
    return null;
  }
}

function writeStoredEpgViewMode(mode: 'list' | 'grid'): void {
  try {
    window.localStorage.setItem(EPG_VIEW_MODE_STORAGE_KEY, mode);
  } catch {
    // ignore (private browsing / storage quota)
  }
}

export default function EPG({
  channels,
  bouquets = [],
  onSelectBouquet,
  onPlay,
}: EpgProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { search } = useLocation();
  const { confirm, toast } = useUiOverlay();
  const uiSurface = useUiSurface();
  const [viewMode, setViewMode] = useState<'list' | 'grid'>(() => {
    const stored = readStoredEpgViewMode();
    if (stored) return stored;
    return import.meta.env.MODE === 'test' || uiSurface.width < 768 ? 'list' : 'grid';
  });
  useEffect(() => {
    writeStoredEpgViewMode(viewMode);
  }, [viewMode]);
  const [selectedEvent, setSelectedEvent] = useState<EpgEvent | null>(null);
  const {
    selectedProfile,
    isReady,
    isFavoriteService,
    toggleFavoriteService,
    canManageDvr,
  } = useHouseholdProfiles();
  const searchParams = useMemo(() => new URLSearchParams(search), [search]);
  const requestedSection = searchParams.get('section');
  const activeSection: EpgSection = requestedSection === 'timers' && canManageDvr
    ? 'timers'
    : 'guide';
  const [state, dispatch] = useReducer(epgReducer, undefined, createInitialEpgState);
  const [timers, setTimers] = React.useState<Timer[]>([]);
  const [showFavoritesOnly, setShowFavoritesOnly] = React.useState(false);
  const abortControllerRef = React.useRef<AbortController | null>(null);

  // ============================================================================
  // Timer Management (for recording feedback)
  // ============================================================================

  const loadTimers = useCallback(async () => {
    if (!isReady || !canManageDvr) {
      return;
    }

    try {
      const data = await fetchTimers();
      setTimers(data);
    } catch (err) {
      debugError('Failed to fetch timers for EPG', formatError(err));
    }
  }, [canManageDvr, isReady]);

  useEffect(() => {
    if (!isReady || activeSection !== 'guide' || !canManageDvr) {
      return;
    }

    loadTimers();
    const interval = setInterval(loadTimers, 30000); // Poll every 30s
    return () => clearInterval(interval);
  }, [activeSection, canManageDvr, isReady, loadTimers]);

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
        // The SDK resolves with { error } instead of throwing on HTTP/network failures;
        // without this check a rejected create (conflict, invalid serviceRef, DVR
        // unavailable, expired token) would still show the green "recording scheduled"
        // toast for a recording that was never created.
        const result = await addTimer({
          body: {
            serviceRef: event.serviceRef,
            begin: event.start,
            end: event.end,
            name: event.title,
            description: normalizeEpgText(event.desc) || '',
          },
        });
        throwOnClientResultError(result, { source: 'EPG.handleRecord' });
        toast({ kind: 'success', message: t('epg.recordSuccess') });
        loadTimers(); // Refresh feedback immediately
      } catch (err) {
        debugError(formatError(err));
        const msg = formatTimerCreateError(err);
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
    if (!isReady) {
      return;
    }

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
      if (import.meta.env.DEV) {
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
    } catch (err) {
      // When a newer load aborts this one (filter change / refresh), fetchEpgEvents wraps
      // the DOMException('AbortError') into a ClientRequestError, so isAbortError(err) (which
      // matches err.name === 'AbortError') no longer fires. Without the signal check the
      // stale invocation dispatches LOAD_ERROR over the new invocation's LOAD_START, showing
      // the error panel instead of the loading skeleton. The signal is authoritative.
      if (signal.aborted || isAbortError(err)) return;
      debugError('EPG load failed:', formatError(err));
      if (isUnauthorizedError(err)) {
        return;
      }
      dispatch({
        type: 'LOAD_ERROR',
        payload: { error: createEpgError(err, t, 'epg.loadError') }
      });
    }
  }, [isReady, state.filters.timeRange, state.filters.bouquetId, t]);

  // Initial load + auto-refresh every 5 minutes
  useEffect(() => {
    if (!isReady || activeSection !== 'guide') {
      return;
    }

    loadEpgEvents();
    const interval = setInterval(loadEpgEvents, 5 * 60 * 1000);
    return () => clearInterval(interval);
  }, [activeSection, isReady, loadEpgEvents]);

  // ============================================================================
  // Search Logic
  // ============================================================================

  const runSearch = useCallback(async () => {
    if (!isReady) {
      return;
    }

    const query = state.filters.query?.trim();
    if (!query) return;

    dispatch({ type: 'SEARCH_START' });
    try {
      const events = await fetchEpgEvents({
        bouquet: state.filters.bouquetId || undefined,
        query,
      });

      dispatch({ type: 'SEARCH_SUCCESS', payload: { events } });
    } catch (err) {
      debugError(formatError(err));
      if (isUnauthorizedError(err)) {
        return;
      }
      dispatch({
        type: 'SEARCH_ERROR',
        payload: { error: createEpgError(err, t, 'epg.searchError') }
      });
    }
  }, [isReady, state.filters.query, state.filters.bouquetId, t]);

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

  const favoriteServiceRefs = useMemo(
    () => new Set(selectedProfile.favoriteServiceRefs),
    [selectedProfile.favoriteServiceRefs]
  );

  const visibleChannels = useMemo(() => {
    if (!showFavoritesOnly) {
      return channels;
    }

    return channels.filter((channel) => isFavoriteService(channel.serviceRef || channel.id || ''));
  }, [channels, isFavoriteService, showFavoritesOnly]);

  const visibleFavoriteCount = useMemo(() => (
    channels.reduce((count, channel) => (
      isFavoriteService(channel.serviceRef || channel.id || '') ? count + 1 : count
    ), 0)
  ), [channels, isFavoriteService]);

  const visibleServiceRefs = useMemo(() => new Set(
    visibleChannels
      .map((channel) => (channel.serviceRef || channel.id || '').toLowerCase())
      .filter(Boolean)
  ), [visibleChannels]);

  const visibleMainEventsByServiceRef = useMemo(() => {
    const map = new Map<string, EpgEvent[]>();
    mainEventsByServiceRef.forEach((events, serviceRef) => {
      if (visibleServiceRefs.has(serviceRef.toLowerCase())) {
        map.set(serviceRef, events);
      }
    });
    return map;
  }, [mainEventsByServiceRef, visibleServiceRefs]);

  const visibleSearchEventsByServiceRef = useMemo(() => {
    const map = new Map<string, EpgEvent[]>();
    searchEventsByServiceRef.forEach((events, serviceRef) => {
      if (visibleServiceRefs.has(serviceRef.toLowerCase())) {
        map.set(serviceRef, events);
      }
    });
    return map;
  }, [searchEventsByServiceRef, visibleServiceRefs]);

  const visibleSearchEventCount = useMemo(() => {
    let count = 0;
    visibleSearchEventsByServiceRef.forEach((events) => {
      count += events.length;
    });
    return count;
  }, [visibleSearchEventsByServiceRef]);

  // ============================================================================
  // Render
  // ============================================================================

  const handleFilterChange = useCallback((updates: Partial<EpgFilters>) => {
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

  const handleSectionChange = useCallback((nextSection: EpgSection) => {
    if (activeSection === nextSection) {
      return;
    }

    navigate(buildEpgRoute(nextSection));
  }, [activeSection, navigate]);

  const showSearchResults = state.searchLoadState === 'ready' && visibleSearchEventCount > 0;
  const showMainList = state.loadState === 'ready' && !showSearchResults;
  const useStackedSurface = uiSurface.width < 1100;
  const useCompactSurface =
    uiSurface.width < 768 ||
    (uiSurface.inputMode === 'coarse' && uiSurface.heightClass !== 'comfortable');
  const useCompactLandscapeSurface =
    uiSurface.inputMode === 'coarse' &&
    uiSurface.orientation === 'landscape' &&
    uiSurface.width < 932;

  return (
    <div
      className={[
        styles.page,
        'animate-enter',
        useStackedSurface ? styles.surfaceStacked : null,
        useCompactSurface ? styles.surfaceCompact : null,
        useCompactLandscapeSurface ? styles.surfaceCompactLandscape : null,
      ].filter(Boolean).join(' ')}
    >
      {canManageDvr ? (
        <div className={styles.surfaceTabs} role="tablist" aria-label={t('epg.sectionNavLabel', { defaultValue: 'Live TV sections' })}>
          <button
            type="button"
            className={[styles.surfaceTab, styles.sidebarToggle].join(' ')}
            onClick={() => window.dispatchEvent(new Event('xg2g:toggle-sidebar'))}
            title="Seitenleiste einblenden (⌘B)"
            aria-label="Seitenleiste einblenden"
          >
            <svg className={styles.sidebarToggleIcon} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <rect x="3" y="3" width="18" height="18" rx="3" />
              <line x1="9" y1="3" x2="9" y2="21" />
            </svg>
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeSection === 'guide'}
            className={[
              styles.surfaceTab,
              activeSection === 'guide' ? styles.surfaceTabActive : null,
            ].filter(Boolean).join(' ')}
            onClick={() => handleSectionChange('guide')}
          >
            {t('nav.epg')}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeSection === 'timers'}
            className={[
              styles.surfaceTab,
              activeSection === 'timers' ? styles.surfaceTabActive : null,
            ].filter(Boolean).join(' ')}
            onClick={() => handleSectionChange('timers')}
          >
            {t('nav.timers')}
          </button>
        </div>
      ) : null}

      {activeSection === 'timers' ? (
        <SectionContextBar
          segments={[
            {
              label: t('nav.epg'),
              onClick: () => handleSectionChange('guide'),
            },
            {
              label: t('nav.timers'),
            },
          ]}
          actionLabel={t('epg.backToGuide', { defaultValue: 'Back to Live TV' })}
          onAction={() => handleSectionChange('guide')}
        />
      ) : null}

      {activeSection === 'guide' ? (
        <>
          <EpgToolbar
            channelCount={visibleChannels.length}
            favoriteCount={visibleFavoriteCount}
            showFavoritesOnly={showFavoritesOnly}
            filters={state.filters}
            bouquets={bouquets}
            loadState={state.loadState}
            searchLoadState={state.searchLoadState}
            onFilterChange={handleFilterChange}
            onRefresh={loadEpgEvents}
            onToggleFavorites={() => setShowFavoritesOnly((current) => !current)}
            onSearch={runSearch}
            extraActions={
              <>
                <button type="button" onClick={() => setViewMode(prev => prev === 'grid' ? 'list' : 'grid')}>
                  <span className={styles.actionIcon} aria-hidden="true">{viewMode === 'grid' ? '☰' : '▦'}</span>
                  <span className={styles.actionLabel}>{viewMode === 'grid' ? 'List' : 'Timeline'}</span>
                </button>
                {canManageDvr ? (
                  <button type="button" onClick={() => handleSectionChange('timers')}>
                    <span className={styles.actionIcon} aria-hidden="true">⏱</span>
                    <span className={styles.actionLabel}>{t('nav.timers')}</span>
                  </button>
                ) : null}
              </>
            }
          />

          {showFavoritesOnly && visibleChannels.length === 0 && (
            <div className={styles.card}>
              {t('epg.noFavorites', { defaultValue: 'Dieses Profil hat noch keine Senderfavoriten.' })}
            </div>
          )}

          {state.searchError && (
            <ErrorPanel
              error={state.searchError}
              onRetry={() => { void runSearch(); }}
              titleAs="h3"
            />
          )}

          {state.searchLoadState === 'loading' && state.filters.query?.trim() && (
            <LoadingSkeleton
              variant="section"
              label={t('common.loading', { defaultValue: 'Loading...' })}
            />
          )}

          {state.searchLoadState === 'ready' &&
            visibleSearchEventCount === 0 &&
            !state.searchError &&
            state.filters.query?.trim() && (
              <div className={styles.card}>
                {t('epg.noResults', { query: state.filters.query.trim() })}
              </div>
            )}

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
                  channels={visibleChannels}
                  eventsByServiceRef={visibleSearchEventsByServiceRef}
                  favoriteServiceRefs={favoriteServiceRefs}
                  currentTime={state.currentTime}
                  timeRangeHours={state.filters.timeRange}
                  expandedChannels={state.expandedSearchChannels}
                  onToggleExpand={handleToggleSearchChannel}
                  onPlay={onPlay}
                  onToggleFavorite={toggleFavoriteService}
                  onRecord={RECORD_SUPPORTED && canManageDvr ? handleRecord : undefined}
                  isRecorded={RECORD_SUPPORTED ? isRecorded : undefined}
                  onEventClick={(evt) => setSelectedEvent(evt)}
                />
              </div>
            </div>
          )}

          {state.loadState === 'loading' && (
            <LoadingSkeleton
              variant="section"
              label={t('epg.loading')}
            />
          )}
          {state.error && (
            <ErrorPanel
              error={state.error}
              onRetry={() => { void loadEpgEvents(); }}
              titleAs="h3"
            />
          )}

          {showMainList && (
            viewMode === 'list' || state.filters.timeRange > EPG_GRID_MAX_RANGE_HOURS ? (
              <div className={styles.programmes}>
                <EpgChannelList
                  mode="main"
                  channels={visibleChannels}
                  eventsByServiceRef={visibleMainEventsByServiceRef}
                  favoriteServiceRefs={favoriteServiceRefs}
                  currentTime={state.currentTime}
                  timeRangeHours={state.filters.timeRange}
                  expandedChannels={state.expandedChannels}
                  onToggleExpand={handleToggleChannel}
                  onPlay={onPlay}
                  onToggleFavorite={toggleFavoriteService}
                  onRecord={RECORD_SUPPORTED && canManageDvr ? handleRecord : undefined}
                  isRecorded={RECORD_SUPPORTED ? isRecorded : undefined}
                  onEventClick={(evt) => setSelectedEvent(evt)}
                />
              </div>
            ) : (
              <EpgTimelineGrid
                channels={visibleChannels}
                eventsByServiceRef={visibleMainEventsByServiceRef}
                favoriteServiceRefs={favoriteServiceRefs}
                currentTime={state.currentTime}
                timeRangeHours={state.filters.timeRange}
                onToggleFavorite={toggleFavoriteService}
                onRecord={RECORD_SUPPORTED && canManageDvr ? handleRecord : undefined}
                isRecorded={RECORD_SUPPORTED ? isRecorded : undefined}
                onPlay={onPlay}
                onEventClick={(evt) => setSelectedEvent(evt)}
              />
            )
          )}
        </>
      ) : (
        <Timers showLegacyNotice={false} />
      )}
      {selectedEvent && (
        <EpgEventDialog
          event={selectedEvent}
          onClose={() => setSelectedEvent(null)}
          onRecord={canManageDvr ? handleRecord : undefined}
          isRecorded={selectedEvent ? isRecorded(selectedEvent) : false}
        />
      )}
    </div>
  );
}
