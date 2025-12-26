// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect } from 'react';
import './App.css';
import { useAppContext } from './context/AppContext';
import V3Player from './components/V3Player.tsx';
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

function App() {
  const ctx = useAppContext();
  const {
    view,
    auth,
    showAuth,
    setShowAuth,
    setToken,
    channels,
    playback,
    initializing,
    dataLoaded,
    checkConfigAndLoad,
    setPlayingChannel
  } = ctx;

  // Force mobile viewport
  useEffect(() => {
    let meta = document.querySelector('meta[name="viewport"]') as HTMLMetaElement | null;
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
  }, []); // Run once on mount

  const handleAuthSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const token = formData.get('token') as string;
    setToken(token);
    setShowAuth(false);
    window.location.reload();
  };

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
      {showAuth && (
        <div className="auth-overlay">
          <div className="auth-modal">
            <h2>🔐 Authentication Required</h2>
            <form onSubmit={handleAuthSubmit}>
              <input
                type="password"
                name="token"
                placeholder="Enter API Token"
                autoFocus
                required
              />
              <button type="submit">Authenticate</button>
            </form>
          </div>
        </div>
      )}

      {playback.playingChannel && (
        <V3Player
          token={auth.token || ''}
          channel={playback.playingChannel}
          autoStart={true}
          onClose={() => setPlayingChannel(null)}
        />
      )}

      <Navigation activeView={view} onViewChange={ctx.setView} />

      <main className="content-area">
        {view === 'dashboard' && <Dashboard />}

        {view === 'epg' && (
          <EPG
            channels={channels.channels}
            bouquets={channels.bouquets as any}
            selectedBouquet={channels.selectedBouquet}
            onSelectBouquet={ctx.loadChannels}
            onPlay={ctx.handlePlay}
          />
        )}

        {view === 'files' && <Files />}
        {view === 'recordings' && <RecordingsList />}
        {view === 'logs' && <Logs />}

        {view === 'timers' && <Timers />}
        {view === 'series' && <SeriesManager />}
        {view === 'config' && <Config />}
      </main>
    </div>
  );
}

export default App;
