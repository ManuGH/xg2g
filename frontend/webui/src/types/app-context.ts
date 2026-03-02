// Type definitions for Application Context

import type { Service, Bouquet } from '../client-ts';

export type AppView =
  | 'dashboard'
  | 'epg'
  | 'timers'
  | 'recordings'
  | 'series'
  | 'files'
  | 'logs'
  | 'settings'
  | 'system';

export interface AuthState {
  token: string | null;
  isAuthenticated: boolean;
}

export interface ChannelState {
  bouquets: Bouquet[];
  selectedBouquet: string;
  channels: Service[];
  loading: boolean;
}

export interface PlaybackState {
  playingChannel: Service | null;
}

export interface AppState {
  // Navigation
  view: AppView;

  // Authentication
  auth: AuthState;

  // Channel Data
  channels: ChannelState;

  // Playback
  playback: PlaybackState;

  // UI State
  showAuth: boolean;
  initializing: boolean;
  dataLoaded: boolean;
}

export interface AppActions {
  // Navigation
  setView: (view: AppView) => void;

  // Authentication
  setToken: (token: string) => void;
  setShowAuth: (show: boolean) => void;

  // Channel Operations
  setBouquets: (bouquets: Bouquet[]) => void;
  setSelectedBouquet: (bouquet: string) => void;
  setChannels: (channels: Service[]) => void;
  setChannelsLoading: (loading: boolean) => void;
  loadChannels: (bouquetName: string) => Promise<void>;

  // Playback
  setPlayingChannel: (channel: Service | null) => void;
  handlePlay: (channel: Service) => void;

  // App Lifecycle
  setInitializing: (init: boolean) => void;
  setDataLoaded: (loaded: boolean) => void;
  checkConfigAndLoad: () => Promise<void>;
  loadBouquetsAndChannels: () => Promise<void>;
}

export interface AppContextType extends AppState, AppActions { }
