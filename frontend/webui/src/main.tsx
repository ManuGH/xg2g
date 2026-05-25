// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.


import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import AppRouter from './AppRouter';
// Self-hosted fonts (no external Google CDN call). Variable axes match the
// previous wght@400..700 / 400..600 / 400..500 usage.
import '@fontsource-variable/space-grotesk';
import '@fontsource-variable/ibm-plex-sans';
import '@fontsource-variable/jetbrains-mono';
import './index.css';
import App from './App.tsx';
import ErrorBoundary from './components/ErrorBoundary';
import { AppProvider } from './context/AppContext.tsx';
import { HouseholdProfilesProvider } from './context/HouseholdProfilesContext.tsx';
import { PendingChangesProvider } from './context/PendingChangesContext.tsx';
import { UiScaleProvider } from './context/UiScaleContext.tsx';
import { UiSurfaceProvider } from './context/UiSurfaceContext.tsx';
import { UiOverlayProvider } from './context/UiOverlayContext.tsx';
import { i18nReady } from './i18n';
import { applyHostEnvironmentToDocument, resolveHostEnvironment } from './lib/hostBridge';
import { applyUiScaleToDocument, readStoredUiScale } from './lib/uiScale.ts';
import { applyUiSurfaceToDocument, resolveUiSurface } from './lib/uiSurface.ts';
import { setClientAuthToken } from './services/clientWrapper';
import { ROUTE_MAP } from './routes.ts';
import { getStoredToken } from './utils/tokenStorage';

// TanStack Query Client Configuration
// Phase 1: Server-State Layer (2026 State-of-the-Art)
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1, // UI nicht "endlos" retryen (Homelab-first)
      refetchOnWindowFocus: false, // Homelab UI - sonst nervig
      staleTime: 0, // Per-Query separat definiert
      gcTime: 5 * 60 * 1000, // 5min garbage collection (vorher cacheTime)
    },
  },
});

// Prime the shared API client before React mounts so the first bootstrap query
// already carries the persisted token on cold starts.
setClientAuthToken(getStoredToken());
const hostEnvironment = resolveHostEnvironment();
applyHostEnvironmentToDocument(hostEnvironment);
applyUiScaleToDocument(readStoredUiScale(window.localStorage), document.documentElement);
applyUiSurfaceToDocument(resolveUiSurface(window, hostEnvironment), document.documentElement);

const root = createRoot(document.getElementById('root')!);

void i18nReady.finally(() => {
  root.render(
    <AppRouter>
      <ErrorBoundary
        fallbackTitle="xg2g could not be loaded"
        fallbackDetail="Try again to restore the interface."
        homeHref={ROUTE_MAP.dashboard}
      >
        <QueryClientProvider client={queryClient}>
          <UiSurfaceProvider>
            <UiScaleProvider>
              <UiOverlayProvider>
                <PendingChangesProvider>
                  <AppProvider>
                    <HouseholdProfilesProvider>
                      <App />
                    </HouseholdProfilesProvider>
                  </AppProvider>
                </PendingChangesProvider>
              </UiOverlayProvider>
            </UiScaleProvider>
          </UiSurfaceProvider>
        </QueryClientProvider>
      </ErrorBoundary>
    </AppRouter>,
  );
});
