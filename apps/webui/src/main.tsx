// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.


import { Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import AppRouter from './AppRouter';
// Self-hosted fonts (no external Google CDN call).
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
import { safeLocalStorage } from './lib/safeStorage.ts';
import { ensureCanonicalBasePath, cleanupStaleServiceWorkers } from './bootRecovery.ts';

// Recover from stale service workers and non-canonical (basename-less) URLs
// before anything renders, so the router never refuses to render (black screen).
cleanupStaleServiceWorkers();
ensureCanonicalBasePath();

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
applyUiScaleToDocument(readStoredUiScale(safeLocalStorage()), document.documentElement);
applyUiSurfaceToDocument(resolveUiSurface(window, hostEnvironment), document.documentElement);

const root = createRoot(document.getElementById('root')!);

function renderApp() {
  root.render(
    <AppRouter>
      <ErrorBoundary
        fallbackTitle="xg2g could not be loaded"
        fallbackDetail="Try again to restore the interface."
        homeHref={ROUTE_MAP.dashboard}
      >
        {/* Root Suspense safety net: any suspension above the app's own
            boundaries commits this fallback instead of leaving #root empty
            (which rendered as a black screen on iOS Safari). */}
        <Suspense fallback={<div className="loading-spinner" />}>
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
        </Suspense>
      </ErrorBoundary>
    </AppRouter>,
  );
}

// Mount as soon as translations are ready, but NEVER block the UI on them: on
// iOS Safari the locale chunk's dynamic import() can stall indefinitely, which
// left i18nReady pending, root.render() uncalled, and #root empty (a black
// screen). Cap the wait so the app always mounts; untranslated keys fill in
// when the bundle arrives.
void Promise.race([
  i18nReady.catch(() => undefined),
  new Promise<void>((resolve) => {
    window.setTimeout(resolve, 1200);
  }),
]).then(renderApp);
