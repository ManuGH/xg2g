// Application Context - Centralized State Management with TypeScript

import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { getServices, getServicesBouquets, getSystemConfig } from '../client-ts';
import { client } from '../client-ts/client.gen';
import type { AppContextType, AppView } from '../types/app-context';
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
  // Navigation State
  const [view, setView] = useState<AppView>('epg');

  // Auth State
  const [token, setTokenState] = useState<string>(getStoredToken());
  const [showAuth, setShowAuth] = useState<boolean>(false);

  // Channel State
  const [bouquets, setBouquets] = useState<Bouquet[]>([]);
  const [selectedBouquet, setSelectedBouquet] = useState<string>('');
  const [channels, setChannels] = useState<Service[]>([]);
  const [loading, setLoading] = useState<boolean>(false);

  // Playback State
  const [playingChannel, setPlayingChannel] = useState<Service | null>(null);

  // UI State
  const [initializing, setInitializing] = useState<boolean>(true);
  const [dataLoaded, setDataLoaded] = useState<boolean>(false);

  // Actions
  const setToken = useCallback((newToken: string) => {
    setTokenState(newToken);
    if (newToken) {
      setStoredToken(newToken);
    } else {
      clearStoredToken();
    }
    client.setConfig({
      headers: {
        Authorization: newToken ? `Bearer ${newToken}` : null
      }
    });
  }, []);

  const loadChannels = useCallback(async (bouquetName: string): Promise<void> => {
    setLoading(true);
    try {
      debugLog('[DEBUG] Fetching channels for:', bouquetName);
      const response = await getServices(
        bouquetName ? { query: { bouquet: bouquetName } } : undefined
      );
      const data = response.data || [];
      setChannels(data);
      setSelectedBouquet(bouquetName);
      debugLog('[DEBUG] Channels loaded. Count:', data.length);
    } catch (err) {
      debugError('[DEBUG] Failed to load channels:', formatError(err));
      if ((err as { status?: number }).status === 401) {
        debugLog('[DEBUG] 401 detected in loadChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const loadBouquetsAndChannels = useCallback(async (): Promise<void> => {
    setLoading(true);
    try {
      debugLog('[DEBUG] Fetching bouquets...');
      const response = await getServicesBouquets();
      const bouquetData = response.data || [];
      setBouquets(bouquetData);
      debugLog('[DEBUG] Bouquets loaded. Count:', bouquetData.length);

      await loadChannels(selectedBouquet);
      setDataLoaded(true);
    } catch (err) {
      debugError('[DEBUG] Failed to load initial data:', formatError(err));
      const apiErr = err as { status?: number };
      debugLog('[DEBUG] Error status:', apiErr.status ?? 'unknown');
      if ((err as { status?: number }).status === 401) {
        debugLog('[DEBUG] 401 detected in loadBouquetsAndChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  }, [selectedBouquet, loadChannels]);

  const checkConfigAndLoad = useCallback(async (): Promise<void> => {
    try {
      const response = await getSystemConfig();
      const config = response.data;
      debugLog('[DEBUG] System config loaded');

      if (!config?.openWebIF?.baseUrl) {
        debugLog('[DEBUG] No Base URL configured. Switching to Setup Mode.');
        setView('settings');
        return;
      }

      await loadBouquetsAndChannels();
    } catch (err) {
      debugError('[DEBUG] Failed to check config:', formatError(err));
      debugLog('[DEBUG] Config check failed. Defaulting to Setup Mode.');
      setView('settings');

      if ((err as { status?: number }).status === 401) {
        setShowAuth(true);
      }
    } finally {
      setInitializing(false);
    }
  }, [loadBouquetsAndChannels]);

  const handlePlay = useCallback((channel: Service) => {
    flushSync(() => setPlayingChannel(channel));
  }, []);

  const contextValue: AppContextType = {
    // State
    view,
    auth: {
      token,
      isAuthenticated: !!token
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
    showAuth,
    initializing,
    dataLoaded,

    // Actions
    setView,
    setToken,
    setShowAuth,
    setBouquets,
    setSelectedBouquet,
    setChannels,
    setChannelsLoading: setLoading,
    loadChannels,
    setPlayingChannel,
    handlePlay,
    setInitializing,
    setDataLoaded,
    checkConfigAndLoad,
    loadBouquetsAndChannels
  };

  return <AppContext.Provider value={contextValue}>{children}</AppContext.Provider>;
}
