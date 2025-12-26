// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { flushSync } from 'react-dom';
import './App.css';
import V3Player from './components/V3Player';
import Dashboard from './components/Dashboard';
import Files from './components/Files';
import Logs from './components/Logs';
import EPG from './components/EPG';
import Timers from './components/Timers';
import RecordingsList from './components/RecordingsList';
import SeriesManager from './components/SeriesManager';
import Config from './components/Config';
import Navigation from './components/Navigation';
import { OpenAPI } from './client/core/OpenAPI';
import { DefaultService } from './client/services/DefaultService';
import { ServicesService } from './client/services/ServicesService';


function App() {
  const [view, setView] = useState('epg');
  const [showAuth, setShowAuth] = useState(false);
  const [token, setToken] = useState(localStorage.getItem('XG2G_API_TOKEN') || '');

  // Channel Data State
  const [bouquets, setBouquets] = useState([]);
  const [selectedBouquet, setSelectedBouquet] = useState(''); // Empty string = "All Channels"
  const [channels, setChannels] = useState([]);
  // eslint-disable-next-line no-unused-vars
  const [loading, setLoading] = useState(false);
  const [initializing, setInitializing] = useState(true);
  const [dataLoaded, setDataLoaded] = useState(false); // To avoid re-fetching on tab switch

  // Force mobile viewport
  useEffect(() => {
    let meta = document.querySelector('meta[name="viewport"]');
    if (!meta) {
      meta = document.createElement('meta');
      meta.name = 'viewport';
      document.head.appendChild(meta);
    }
    meta.content = 'width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no';
  }, []);


  useEffect(() => {
    console.log('[DEBUG] App mounted. showingAuth:', showAuth);
    const handleAuth = () => {
      console.log('[DEBUG] auth-required event received');
      setShowAuth(true);
    };
    window.addEventListener('auth-required', handleAuth);

    // Initialize OpenAPI client with token if available
    const storedToken = localStorage.getItem('XG2G_API_TOKEN');
    console.log('[DEBUG] Stored token:', storedToken);
    if (storedToken) {
      OpenAPI.TOKEN = storedToken;
    }

    // Check config first, then load data if configured
    if (!dataLoaded) {
      checkConfigAndLoad();
    }

    return () => window.removeEventListener('auth-required', handleAuth);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Run once on mount

  const checkConfigAndLoad = async () => {
    try {
      const config = await DefaultService.getSystemConfig();
      console.log('[DEBUG] System Config:', config);

      if (!config.openWebIF?.baseUrl) {
        console.log('[DEBUG] No Base URL configured. Switching to Setup Mode.');
        setView('config'); // Force Config view
        return;
      }

      // If configured, load content
      loadBouquetsAndChannels();
    } catch (err) {
      console.error('[DEBUG] Failed to check config:', err);

      console.log('[DEBUG] Config check failed. Defaulting to Setup Mode.');
      setView('config');

      // If config check fails, we might as well show setup or auth if 401
      if (err.status === 401) {
        setShowAuth(true);
      }
    } finally {
      setInitializing(false);
    }
  };

  const loadBouquetsAndChannels = async () => {
    setLoading(true);
    try {
      console.log('[DEBUG] Fetching bouquets...');
      // 1. Fetch Bouquets
      const bouquetData = await ServicesService.getServicesBouquets();
      setBouquets(bouquetData || []);
      console.log('[DEBUG] Bouquets loaded:', bouquetData);

      // 2. Fetch All Channels (default view)
      await loadChannels(selectedBouquet);

      setDataLoaded(true);
    } catch (err) {
      console.error('[DEBUG] Failed to load initial data:', err);
      console.log('[DEBUG] Error status:', err.status, 'Body:', err.body);
      if (err.status === 401) {
        console.log('[DEBUG] 401 detected in loadBouquetsAndChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  };


  const loadChannels = async (bouquetName) => {
    setLoading(true);
    try {
      console.log('[DEBUG] Fetching channels for:', bouquetName);
      const params = bouquetName ? { bouquet: bouquetName } : {};
      const data = await DefaultService.getServices(params);
      setChannels(data || []);
      setSelectedBouquet(bouquetName);
      console.log('[DEBUG] Channels loaded. Count:', data?.length);
    } catch (err) {
      console.error('[DEBUG] Failed to load channels:', err);
      if (err.status === 401) {
        console.log('[DEBUG] 401 detected in loadChannels -> showing auth');
        setShowAuth(true);
      }
    } finally {
      setLoading(false);
    }
  };

  const saveToken = () => {
    localStorage.setItem('XG2G_API_TOKEN', token);
    OpenAPI.TOKEN = token;
    setShowAuth(false);
    window.location.reload();
  };

  // Player State
  const [playingChannel, setPlayingChannel] = useState(null);

  const handlePlay = (channel) => {
    // iOS/Safari only allows starting playback with sound inside a real user gesture.
    // Force the Player overlay to mount synchronously so its `useLayoutEffect` can call `video.play()`
    // within the same click/tap event.
    flushSync(() => setPlayingChannel(channel));
  };

  // ... (handlePlay remain same)

  if (initializing) {
    return (
      <div className="app-container" style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
        <div className="loading-spinner"></div>
        <p style={{ marginLeft: '10px' }}>Initializing...</p>
      </div>
    );
  }

  return (
    <div className="app-container">
      {/* Auth Modal */}
      {showAuth && (
        <div className="auth-modal-overlay">
          <div className="auth-modal glass">
            <h2>Authentication Required</h2>
            <p>Please enter your API Token</p>
            <input
              type="password"
              value={token}
              onChange={e => setToken(e.target.value)}
              placeholder="XG2G_API_TOKEN"
            />
            <button onClick={saveToken} disabled={!token}>Save</button>
          </div>
        </div>
      )}

      {/* Player Overlay */}
      {playingChannel && (
        <V3Player
          token={token}
          channel={playingChannel}
          autoStart={true}
          onClose={() => setPlayingChannel(null)}
        />
      )}

      {/* New Responsive Navigation */}
      <Navigation activeView={view} onViewChange={setView} />

      {/* Main Content Area */}
      <main className="app-main">
        {view === 'dashboard' && <Dashboard />}
        {view === 'epg' && (
          <EPG
            channels={channels}
            bouquets={bouquets}
            selectedBouquet={selectedBouquet}
            onSelectBouquet={loadChannels}
            onPlay={handlePlay}
          />
        )}
        {view === 'files' && <Files />}
        {view === 'logs' && <Logs />}
        {view === 'recordings' && <RecordingsList token={token} />}

        {view === 'timers' && <Timers />}
        {view === 'series' && <SeriesManager />}
        {view === 'config' && <Config />}
      </main>
    </div>
  );
}

export default App;
