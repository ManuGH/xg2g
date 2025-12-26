// Application Context - Centralized State Management with TypeScript

import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { OpenAPI } from '../client/core/OpenAPI';
import { DefaultService } from '../client/services/DefaultService';
import { ServicesService } from '../client/services/ServicesService';
import type { AppContextType, AppView } from '../types/app-context';
import type { Service, Bouquet } from '../client';

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
  const [token, setTokenState] = useState<string>(
    localStorage.getItem('XG2G_API_TOKEN') || ''
  );
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
    localStorage.setItem('XG2G_API_TOKEN', newToken);
    OpenAPI.TOKEN = newToken;
  }, []);

  const loadChannels = useCallback(async (bouquetName: string): Promise<void> => {
    setLoading(true);
    try {
      console.log('[DEBUG] Fetching channels for:', bouquetName);
      const response = await DefaultService.getServices(
        bouquetName ? { query: { bouquet: bouquetName } } : undefined
      );
      const data = response.data || [];
      setChannels(data);
      setSelectedBouquet(bouquetName);
      console.log('[DEBUG] Channels loaded. Count:', data.length);
    } catch (err) {
      console.error('[DEBUG] Failed to load channels:', err);
      if ((err as { status?: number }).status === 401) {
        console.log('[DEBUG] 401 detected in loadChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const loadBouquetsAndChannels = useCallback(async (): Promise<void> => {
    setLoading(true);
    try {
      console.log('[DEBUG] Fetching bouquets...');
      const response = await ServicesService.getServicesBouquets();
      const bouquetData = response.data || [];
      setBouquets(bouquetData);
      console.log('[DEBUG] Bouquets loaded:', bouquetData);

      await loadChannels(selectedBouquet);
      setDataLoaded(true);
    } catch (err) {
      console.error('[DEBUG] Failed to load initial data:', err);
      console.log('[DEBUG] Error status:', (err as any).status, 'Body:', (err as any).body);
      if ((err as { status?: number }).status === 401) {
        console.log('[DEBUG] 401 detected in loadBouquetsAndChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  }, [selectedBouquet, loadChannels]);

  const checkConfigAndLoad = useCallback(async (): Promise<void> => {
    try {
      const response = await DefaultService.getSystemConfig();
      const config = response.data;
      console.log('[DEBUG] System Config:', config);

      if (!config?.openWebIF?.baseUrl) {
        console.log('[DEBUG] No Base URL configured. Switching to Setup Mode.');
        setView('config');
        return;
      }

      await loadBouquetsAndChannels();
    } catch (err) {
      console.error('[DEBUG] Failed to check config:', err);
      console.log('[DEBUG] Config check failed. Defaulting to Setup Mode.');
      setView('config');

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
