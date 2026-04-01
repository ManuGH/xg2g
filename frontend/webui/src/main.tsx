// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.


import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import './index.css';
import App from './App.tsx';
import ErrorBoundary from './components/ErrorBoundary';
import { AppProvider } from './context/AppContext.tsx';
import { HouseholdProfilesProvider } from './context/HouseholdProfilesContext.tsx';
import { PendingChangesProvider } from './context/PendingChangesContext.tsx';
import { UiOverlayProvider } from './context/UiOverlayContext.tsx';
import { i18nReady } from './i18n';
import { applyHostEnvironmentToDocument, resolveHostEnvironment } from './lib/hostBridge';
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
applyHostEnvironmentToDocument(resolveHostEnvironment());

const root = createRoot(document.getElementById('root')!);

void i18nReady.finally(() => {
  root.render(
    <BrowserRouter>
      <ErrorBoundary
        fallbackTitle="xg2g could not be loaded"
        fallbackDetail="Try again to restore the interface."
        homeHref={ROUTE_MAP.dashboard}
      >
        <QueryClientProvider client={queryClient}>
          <UiOverlayProvider>
            <PendingChangesProvider>
              <AppProvider>
                <HouseholdProfilesProvider>
                  <App />
                </HouseholdProfilesProvider>
              </AppProvider>
            </PendingChangesProvider>
          </UiOverlayProvider>
        </QueryClientProvider>
      </ErrorBoundary>
    </BrowserRouter>,
  );
});
