// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, lazy, Suspense, useMemo } from 'react';


import './App.css';
import { useAppContext } from './context/AppContext';
import Navigation from './components/Navigation';

// Lazy load feature views (Phase 4: Bundle optimization)
// V3Player is lazy loaded because it includes heavy HLS.js dependency
const V3Player = lazy(() => import('./components/V3Player'));
const Dashboard = lazy(() => import('./components/Dashboard'));
const EPG = lazy(() => import('./features/epg/EPG'));
const Logs = lazy(() => import('./components/Logs'));
const Files = lazy(() => import('./components/Files'));
const SeriesManager = lazy(() => import('./components/SeriesManager'));
const Timers = lazy(() => import('./components/Timers'));
const RecordingsList = lazy(() => import('./components/RecordingsList'));
const Settings = lazy(() => import('./components/Settings'));
const SystemInfo = lazy(() => import('./features/system/SystemInfo').then(m => ({ default: m.SystemInfo })));

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

    // Initialize client with token if available
    const storedToken = localStorage.getItem('XG2G_API_TOKEN');
    console.log('[DEBUG] Stored token:', storedToken);
    if (storedToken) {
      setToken(storedToken);
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

  const memoizedBouquets = useMemo(() => (channels.bouquets || []).map(b => ({
    name: b.name || 'Unknown',
    services: b.services ?? 0
  })), [channels.bouquets]);

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
        <Suspense fallback={<div className="loading-spinner"></div>}>
          <V3Player
            token={auth.token || ''}
            channel={playback.playingChannel}
            autoStart={true}
            onClose={() => setPlayingChannel(null)}
          />
        </Suspense>
      )}

      <Navigation activeView={view} onViewChange={ctx.setView} />

      <main className="content-area">
        <Suspense fallback={<div className="loading-spinner" style={{ margin: '50px auto' }}></div>}>
          {view === 'dashboard' && <Dashboard />}

          {view === 'epg' && (
            <EPG
              channels={channels.channels}
              bouquets={memoizedBouquets}
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
          {view === 'settings' && <Settings />}
          {view === 'system' && <SystemInfo />}
        </Suspense>
      </main>
    </div>
  );
}

export default App;
