// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { flushSync } from 'react-dom';
import './App.css';
import Player from './components/Player';
import V3Player from './components/V3Player';
import Dashboard from './components/Dashboard';
import Files from './components/Files';
import Logs from './components/Logs';
import EPG from './components/EPG';
import Timers from './components/Timers';
import RecordingsList from './components/RecordingsList';
import SeriesManager from './components/SeriesManager';
import Navigation from './components/Navigation';
import { OpenAPI } from './client/core/OpenAPI';
import { DefaultService } from './client/services/DefaultService';
import { ServicesService } from './client/services/ServicesService';


function App() {
  const [view, setView] = useState('epg');
  const [showAuth, setShowAuth] = useState(!localStorage.getItem('XG2G_API_TOKEN'));
  const [token, setToken] = useState(localStorage.getItem('XG2G_API_TOKEN') || '');

  // Channel Data State
  const [bouquets, setBouquets] = useState([]);
  const [selectedBouquet, setSelectedBouquet] = useState(''); // Empty string = "All Channels"
  const [channels, setChannels] = useState([]);
  // eslint-disable-next-line no-unused-vars
  const [loading, setLoading] = useState(false);
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
    const handleAuth = () => setShowAuth(true);
    window.addEventListener('auth-required', handleAuth);

    // Initialize OpenAPI client with token
    const storedToken = localStorage.getItem('XG2G_API_TOKEN');
    if (storedToken) {
      OpenAPI.TOKEN = storedToken;
      // Initial load if token exists
      if (!dataLoaded) {
        loadBouquetsAndChannels();
      }
    }

    return () => window.removeEventListener('auth-required', handleAuth);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Run once on mount

  const loadBouquetsAndChannels = async () => {
    setLoading(true);
    try {
      // 1. Fetch Bouquets
      const bouquetData = await ServicesService.getServicesBouquets();
      setBouquets(bouquetData || []);

      // 2. Fetch All Channels (default view)
      // If we already have a selected bouquet (e.g. from saved state), use it. 
      // Otherwise default to '' (All).
      await loadChannels(selectedBouquet);

      setDataLoaded(true);
    } catch (err) {
      console.error('Failed to load initial data:', err);
      if (err.status === 401) setShowAuth(true);
    } finally {
      setLoading(false);
    }
  };


  const loadChannels = async (bouquetName) => {
    setLoading(true);
    try {
      // If bouquetName is empty string, we fetch ALL services (no bouquet param)
      // If bouquetName is set, we fetch for that bouquet
      const params = bouquetName ? { bouquet: bouquetName } : {};
      const data = await DefaultService.getServices(params);
      setChannels(data || []);
      setSelectedBouquet(bouquetName);
    } catch (err) {
      console.error('Failed to load channels:', err);
      if (err.status === 401) setShowAuth(true);
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

  // ... (existing helper functions)

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
        {view === 'v3' && <div className="glass"><V3Player token={token} /></div>}
        {view === 'series' && <SeriesManager />}
      </main>
    </div>
  );
}

export default App;
