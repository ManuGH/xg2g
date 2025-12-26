// EPG State Machine - Pure TypeScript reducer
// Zero React dependencies, zero API imports

import type { EpgAction, EpgFilters, EpgState } from './types';
import { DEFAULT_TIME_RANGE } from './types';

// All timestamps in seconds (not ms)
function nowSeconds(): number {
  return Math.floor(Date.now() / 1000);
}

// Create a fully-initialized state with no React/API dependencies.
export function createInitialEpgState(currentTime: number = nowSeconds()): EpgState {
  // Keep defaults centralized to avoid "magic numbers" across UI.
  const defaultFilters: EpgFilters = {
    query: '',
    timeRange: DEFAULT_TIME_RANGE,
    bouquetId: '',
    channelId: '',
  };

  return {
    // Main EPG Data
    events: [],
    channels: [],
    bouquets: [],

    // Search Results (isolated)
    searchEvents: [],

    // Filters
    filters: defaultFilters,

    // Load States
    loadState: 'idle',
    searchLoadState: 'idle',
    error: null,
    searchError: null,

    // Expansion
    expandedChannels: new Set<string>(),
    expandedSearchChannels: new Set<string>(),

    // Time
    currentTime,
  };
}

function toggleSetValue(set: Set<string>, id: string): Set<string> {
  const next = new Set(set);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

export function epgReducer(state: EpgState, action: EpgAction): EpgState {
  switch (action.type) {
    // -------- Main Load --------
    case 'LOAD_START':
      return {
        ...state,
        loadState: 'loading',
        error: null,
      };

    case 'LOAD_SUCCESS':
      return {
        ...state,
        events: action.payload.events,
        loadState: 'ready',
        error: null,
      };

    case 'LOAD_ERROR':
      return {
        ...state,
        loadState: 'error',
        error: action.payload.error,
      };

    // -------- Search Load (isolated results) --------
    case 'SEARCH_START':
      return {
        ...state,
        searchLoadState: 'loading',
        searchError: null,
      };

    case 'SEARCH_SUCCESS':
      return {
        ...state,
        searchEvents: action.payload.events, // NOT main events
        searchLoadState: 'ready',
        searchError: null,
      };

    case 'SEARCH_ERROR':
      return {
        ...state,
        searchLoadState: 'error',
        searchError: action.payload.error,
      };

    case 'SEARCH_CLEAR':
      return {
        ...state,
        searchEvents: [],
        searchLoadState: 'idle',
        searchError: null,
        expandedSearchChannels: new Set<string>(), // new instance, no mutation
      };

    // -------- Data Injection (channels/bouquets) --------
    case 'SET_CHANNELS':
      return {
        ...state,
        channels: action.payload.channels,
      };

    case 'SET_BOUQUETS':
      return {
        ...state,
        bouquets: action.payload.bouquets,
      };

    // -------- Filters --------
    case 'SET_FILTER':
      return {
        ...state,
        filters: {
          ...state.filters,
          ...action.payload,
        },
      };

    // -------- Expansion Toggles --------
    case 'TOGGLE_CHANNEL':
      return {
        ...state,
        expandedChannels: toggleSetValue(state.expandedChannels, action.payload.channelId),
      };

    case 'TOGGLE_SEARCH_CHANNEL':
      return {
        ...state,
        expandedSearchChannels: toggleSetValue(
          state.expandedSearchChannels,
          action.payload.channelId
        ),
      };

    // -------- Time --------
    case 'UPDATE_TIME':
      return {
        ...state,
        currentTime: action.payload.currentTime, // seconds
      };

    default:
      // Exhaustiveness check handled by discriminated union
      return state;
  }
}
