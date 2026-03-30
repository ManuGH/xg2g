// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { lazy, Suspense, useMemo } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import './App.css';
import { useAppContext } from './context/AppContext';
import AppShell from './AppShell';
import BootstrapGate from './components/BootstrapGate';
import { usePlayerHistoryBridge } from './features/player/usePlayerHistoryBridge';
import { ROUTE_MAP } from './routes';

// Lazy load feature views (Phase 4: Bundle optimization)
// V3Player is lazy loaded because it includes heavy HLS.js dependency
const V3Player = lazy(() => import('./features/player/components/V3Player'));
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
    auth,
    setToken,
    channels,
    playback,
    setPlayingChannel
  } = ctx;

  const handleLogout = () => {
    setPlayingChannel(null);
    setToken('');
  };
  const handlePlayerClose = usePlayerHistoryBridge(
    playback.playingChannel !== null,
    () => setPlayingChannel(null),
  );

  const memoizedBouquets = useMemo(() => (channels.bouquets || []).map(b => ({
    name: b.name || 'Unknown',
    services: b.services ?? 0
  })), [channels.bouquets]);

  return (
    <div className="app-container">
      {playback.playingChannel && (
        <Suspense fallback={<div className="loading-spinner"></div>}>
          <V3Player
            token={auth.token || ''}
            channel={playback.playingChannel}
            autoStart={true}
            onClose={handlePlayerClose}
          />
        </Suspense>
      )}

      <Routes>
        <Route element={<BootstrapGate />}>
          <Route element={<AppShell onLogout={auth.isAuthenticated ? handleLogout : undefined} />}>
            <Route path={ROUTE_MAP.dashboard} element={<Dashboard />} />
            <Route
              path={ROUTE_MAP.epg}
              element={(
                <EPG
                  channels={channels.channels}
                  bouquets={memoizedBouquets}
                  selectedBouquet={channels.selectedBouquet}
                  onSelectBouquet={ctx.loadChannels}
                  onPlay={ctx.handlePlay}
                />
              )}
            />
            <Route path={ROUTE_MAP.files} element={<Files />} />
            <Route path={ROUTE_MAP.recordings} element={<RecordingsList />} />
            <Route path={ROUTE_MAP.logs} element={<Logs />} />
            <Route path={ROUTE_MAP.timers} element={<Timers />} />
            <Route path={ROUTE_MAP.series} element={<SeriesManager />} />
            <Route path={ROUTE_MAP.settings} element={<Settings />} />
            <Route path={ROUTE_MAP.system} element={<SystemInfo />} />
            <Route path="/" element={<Navigate to={ROUTE_MAP.epg} replace />} />
            <Route path="*" element={<Navigate to={ROUTE_MAP.epg} replace />} />
          </Route>
        </Route>
      </Routes>
    </div>
  );
}

export default App;
