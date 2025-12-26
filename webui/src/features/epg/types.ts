// EPG Feature Types - Pure TypeScript, no React dependencies

export type EpgLoadState = 'idle' | 'loading' | 'ready' | 'error';

export interface EpgEvent {
  service_ref: string;
  start: number; // Unix timestamp
  end: number; // Unix timestamp
  title: string;
  desc?: string;
}

export interface EpgChannel {
  id: string;
  service_ref?: string;
  serviceRef?: string;
  name: string;
  number?: string;
  group?: string;
  logo_url?: string;
  logoUrl?: string;
  logo?: string;
}

export interface EpgBouquet {
  name: string;
  services?: string[];
}

export interface EpgFilters {
  query?: string;
  timeRange: number; // Hours from now
  bouquetId?: string;
  channelId?: string;
}

export interface EpgState {
  // Main EPG Data
  events: EpgEvent[];
  channels: EpgChannel[];
  bouquets: EpgBouquet[];

  // Search Results (separate from main events)
  searchEvents: EpgEvent[];

  // Filters
  filters: EpgFilters;

  // UI State
  loadState: EpgLoadState;
  searchLoadState: EpgLoadState;
  error: string | null;
  searchError: string | null;

  // Expansion state
  expandedChannels: Set<string>;
  expandedSearchChannels: Set<string>;

  // Current time (for progress bars)
  currentTime: number; // Unix timestamp
}

export interface Timer {
  timerId?: string;
  serviceRef?: string;
  serviceref?: string;
  service_ref?: string;
  begin: number;
  end: number;
  name?: string;
  description?: string;
}

export interface TimersData {
  items?: Timer[];
}

// EPG Actions (for reducer pattern)
export type EpgAction =
  | { type: 'LOAD_START' }
  | { type: 'LOAD_SUCCESS'; payload: { events: EpgEvent[] } }
  | { type: 'LOAD_ERROR'; payload: { error: string } }
  | { type: 'SEARCH_START' }
  | { type: 'SEARCH_SUCCESS'; payload: { events: EpgEvent[] } }
  | { type: 'SEARCH_ERROR'; payload: { error: string } }
  | { type: 'SEARCH_CLEAR' }
  | { type: 'SET_FILTER'; payload: Partial<EpgFilters> }
  | { type: 'SET_CHANNELS'; payload: { channels: EpgChannel[] } }
  | { type: 'SET_BOUQUETS'; payload: { bouquets: EpgBouquet[] } }
  | { type: 'TOGGLE_CHANNEL'; payload: { channelId: string } }
  | { type: 'TOGGLE_SEARCH_CHANNEL'; payload: { channelId: string } }
  | { type: 'UPDATE_TIME'; payload: { currentTime: number } };
