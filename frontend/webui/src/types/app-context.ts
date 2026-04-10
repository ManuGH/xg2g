// Type definitions for Application Context

import type { Service, Bouquet } from '../client-ts';

export interface AuthState {
  token: string | null;
  hasServerSession?: boolean;
  isAuthenticated: boolean;
  isReady?: boolean;
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
  // Authentication
  auth: AuthState;

  // Channel Data
  channels: ChannelState;

  // Playback
  playback: PlaybackState;

  // UI State
  dataLoaded: boolean;
}

export interface AppActions {
  // Authentication
  setToken: (token: string) => void;
  setServerSessionAuthenticated: (authenticated: boolean) => void;

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
  loadBouquetsAndChannels: () => Promise<void>;
}

export interface AppContextType extends AppState, AppActions { }
