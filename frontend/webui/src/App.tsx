// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { Suspense, useEffect, useMemo, type ReactElement } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import './App.css';
import { lazyWithRetry } from './lib/lazyWithRetry';
import { useAppContext } from './context/AppContext';
import { useHouseholdProfiles } from './context/HouseholdProfilesContext';
import { useUiOverlay } from './context/UiOverlayContext';
import AppShell from './AppShell';
import BootstrapGate from './components/BootstrapGate';
import {
  filterServicesForProfile,
  isServiceAllowedForProfile,
  sortServicesForProfile,
} from './features/household/model';
import { deleteServerSession } from './features/household/api';
import { usePlayerHistoryBridge } from './features/player/usePlayerHistoryBridge';
import {
  ROUTE_MAP,
  UNLOCK_ROUTE,
  buildEpgRoute,
  buildRecordingsRoute,
  buildSettingsRoute,
} from './routes';
import { formatError } from './utils/logging';

// Lazy load feature views (Phase 4: Bundle optimization)
// V3Player is lazy loaded because it includes heavy HLS.js dependency
const V3Player = lazyWithRetry(() => import('./features/player/components/V3Player'));
const Dashboard = lazyWithRetry(() => import('./components/Dashboard'));
const EPG = lazyWithRetry(() => import('./features/epg/EPG'));
const RecordingsList = lazyWithRetry(() => import('./components/RecordingsList'));
const Settings = lazyWithRetry(() => import('./components/Settings'));
const SystemInfo = lazyWithRetry(() => import('./features/system/SystemInfo').then(m => ({ default: m.SystemInfo })));
const UnlockStatus = lazyWithRetry(() => import('./features/unlock/UnlockStatus').then(m => ({ default: m.UnlockStatus })));
const EmptyStatePlayground = lazyWithRetry(() => import('./dev/EmptyStatePlayground'));

function ProfileRouteGate({ allowed, children }: { allowed: boolean; children: ReactElement }) {
  return allowed ? children : <Navigate to={ROUTE_MAP.dashboard} replace />;
}

function App() {
  const ctx = useAppContext();
  const household = useHouseholdProfiles();
  const { toast } = useUiOverlay();
  const {
    auth,
    setToken,
    channels,
    playback,
    setPlayingChannel
  } = ctx;

  const handleLogout = async () => {
    try {
      await deleteServerSession();
    } catch (error) {
      toast({
        kind: 'error',
        message: `Abmeldung fehlgeschlagen: ${formatError(error)}`,
      });
      return;
    }

    setPlayingChannel(null);
    setToken('');
  };
  const handlePlayerClose = usePlayerHistoryBridge(
    playback.playingChannel !== null,
    () => setPlayingChannel(null),
  );

  const filteredChannels = useMemo(() => (
    sortServicesForProfile(
      household.selectedProfile,
      filterServicesForProfile(household.selectedProfile, channels.channels)
    )
  ), [channels.channels, household.selectedProfile]);

  const memoizedBouquets = useMemo(() => {
    const counts = new Map<string, number>();

    filteredChannels.forEach((channel) => {
      const bouquetName = String(channel.group || '').trim();
      if (!bouquetName) {
        return;
      }
      counts.set(bouquetName, (counts.get(bouquetName) || 0) + 1);
    });

    return Array.from(counts.entries())
      .sort(([left], [right]) => left.localeCompare(right, undefined, { sensitivity: 'base' }))
      .map(([name, services]) => ({ name, services }));
  }, [filteredChannels]);

  useEffect(() => {
    if (!playback.playingChannel) {
      return;
    }

    if (isServiceAllowedForProfile(household.selectedProfile, playback.playingChannel)) {
      return;
    }

    setPlayingChannel(null);
  }, [household.selectedProfile, playback.playingChannel, setPlayingChannel]);

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
        {import.meta.env.DEV && (
          <Route path="/__playground/empty-state" element={<Suspense fallback={<div className="loading-spinner"></div>}><EmptyStatePlayground /></Suspense>} />
        )}
        <Route element={<BootstrapGate />}>
          <Route element={<AppShell onLogout={auth.isAuthenticated ? handleLogout : undefined} />}>
            <Route path={ROUTE_MAP.dashboard} element={<Dashboard />} />
            <Route
              path={ROUTE_MAP.epg}
              element={(
                <EPG
                  channels={filteredChannels}
                  bouquets={memoizedBouquets}
                  selectedBouquet={channels.selectedBouquet}
                  onSelectBouquet={ctx.loadChannels}
                  onPlay={ctx.handlePlay}
                />
              )}
            />
            <Route
              path={ROUTE_MAP.files}
              element={(
                <ProfileRouteGate allowed={household.canAccessSettings}>
                  <Navigate to={buildSettingsRoute({ section: 'advanced', tool: 'files' })} replace />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.recordings}
              element={(
                <ProfileRouteGate allowed={household.canAccessDvrPlayback}>
                  <RecordingsList />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.logs}
              element={(
                <ProfileRouteGate allowed={household.canAccessSettings}>
                  <Navigate to={buildSettingsRoute({ section: 'advanced', tool: 'logs' })} replace />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.timers}
              element={(
                <ProfileRouteGate allowed={household.canManageDvr}>
                  <Navigate to={buildEpgRoute('timers')} replace />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.series}
              element={(
                <ProfileRouteGate allowed={household.canManageDvr}>
                  <Navigate to={buildRecordingsRoute({ section: 'series' })} replace />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.settings}
              element={(
                <ProfileRouteGate allowed={household.canAccessSettings}>
                  <Settings />
                </ProfileRouteGate>
              )}
            />
            <Route
              path={ROUTE_MAP.system}
              element={(
                <ProfileRouteGate allowed={household.canAccessSettings}>
                  <SystemInfo />
                </ProfileRouteGate>
              )}
            />
            <Route path={UNLOCK_ROUTE} element={<UnlockStatus />} />
            <Route path="/" element={<Navigate to={ROUTE_MAP.dashboard} replace />} />
            <Route path="*" element={<Navigate to={ROUTE_MAP.dashboard} replace />} />
          </Route>
        </Route>
      </Routes>
    </div>
  );
}

export default App;
