// Application Context - Centralized State Management with TypeScript

import { createContext, useContext, useState, useCallback, useLayoutEffect, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { getServices, getServicesBouquets } from '../client-ts';
import { setClientAuthToken, throwOnClientResultError } from '../services/clientWrapper';
import type { AppContextType } from '../types/app-context';
import type { Service, Bouquet } from '../client-ts';
import { debugError, debugLog, formatError } from '../utils/logging';
import { clearStoredToken, getStoredToken, setStoredToken } from '../utils/tokenStorage';

const AppContext = createContext<AppContextType | undefined>(undefined);

export function useAppContext(): AppContextType {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error('useAppContext must be used within AppProvider');
  }
  return context;
}

interface AppProviderProps {
  children: ReactNode;
}

export function AppProvider({ children }: AppProviderProps) {
  const [initialToken] = useState<string>(() => getStoredToken());

  // Auth State
  const [token, setTokenState] = useState<string>(initialToken);
  const [authReady, setAuthReady] = useState<boolean>(() => !initialToken);

  // Channel State
  const [bouquets, setBouquets] = useState<Bouquet[]>([]);
  const [selectedBouquet, setSelectedBouquet] = useState<string>('');
  const [channels, setChannels] = useState<Service[]>([]);
  const [loading, setLoading] = useState<boolean>(false);

  // Playback State
  const [playingChannel, setPlayingChannel] = useState<Service | null>(null);

  // UI State
  const [dataLoaded, setDataLoaded] = useState<boolean>(false);

  // Synchronize the client auth header before bootstrap queries run.
  // This keeps token hydration out of render while avoiding a 401 race on cold starts.
  useLayoutEffect(() => {
    if (authReady) {
      return;
    }
    setClientAuthToken(token);
    setAuthReady(true);
  }, [authReady, token]);

  // Actions
  const setToken = useCallback((newToken: string) => {
    const normalizedToken = newToken.trim();
    setTokenState(normalizedToken);
    if (normalizedToken) {
      setStoredToken(normalizedToken);
      setAuthReady(false);
    } else {
      clearStoredToken();
      setClientAuthToken('');
      setAuthReady(true);
    }
    setBouquets([]);
    setSelectedBouquet('');
    setChannels([]);
    setLoading(false);
    setDataLoaded(false);
  }, []);

  const fetchChannels = useCallback(async (bouquetName: string): Promise<Service[]> => {
    debugLog('[DEBUG] Fetching channels for:', bouquetName);
    const response = await getServices(
      bouquetName ? { query: { bouquet: bouquetName } } : undefined
    );
    throwOnClientResultError(response, { source: 'AppContext.fetchChannels' });
    return response.data || [];
  }, []);

  const loadChannels = useCallback(async (bouquetName: string): Promise<void> => {
    setLoading(true);
    try {
      const data = await fetchChannels(bouquetName);
      setChannels(data);
      setSelectedBouquet(bouquetName);
      debugLog('[DEBUG] Channels loaded. Count:', data.length);
    } catch (err) {
      debugError('[DEBUG] Failed to load channels:', formatError(err));
    } finally {
      setLoading(false);
    }
  }, [fetchChannels]);

  const loadBouquetsAndChannels = useCallback(async (): Promise<void> => {
    setLoading(true);
    try {
      debugLog('[DEBUG] Fetching bouquets...');
      const response = await getServicesBouquets();
      throwOnClientResultError(response, { source: 'AppContext.loadBouquetsAndChannels' });
      const bouquetData = response.data || [];
      setBouquets(bouquetData);
      debugLog('[DEBUG] Bouquets loaded. Count:', bouquetData.length);

      const channelData = await fetchChannels(selectedBouquet);
      setChannels(channelData);
      setSelectedBouquet(selectedBouquet);
      debugLog('[DEBUG] Channels loaded. Count:', channelData.length);
      setDataLoaded(true);
    } catch (err) {
      debugError('[DEBUG] Failed to load initial data:', formatError(err));
      const apiErr = err as { status?: number };
      debugLog('[DEBUG] Error status:', apiErr.status ?? 'unknown');
    } finally {
      setLoading(false);
    }
  }, [fetchChannels, selectedBouquet]);

  const handlePlay = useCallback((channel: Service) => {
    flushSync(() => setPlayingChannel(channel));
  }, []);

  const contextValue: AppContextType = {
    // State
    auth: {
      token,
      isAuthenticated: !!token,
      isReady: authReady,
    },
    channels: {
      bouquets,
      selectedBouquet,
      channels,
      loading
    },
    playback: {
      playingChannel
    },
    dataLoaded,

    // Actions
    setToken,
    setBouquets,
    setSelectedBouquet,
    setChannels,
    setChannelsLoading: setLoading,
    loadChannels,
    setPlayingChannel,
    handlePlay,
    loadBouquetsAndChannels
  };

  return <AppContext.Provider value={contextValue}>{children}</AppContext.Provider>;
}
