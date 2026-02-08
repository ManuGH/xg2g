// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.


import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import './i18n';
import './index.css';
import App from './App.tsx';
import ErrorBoundary from './components/ErrorBoundary';
import { AppProvider } from './context/AppContext.tsx';
import { UiOverlayProvider } from './context/UiOverlayContext.tsx';

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

createRoot(document.getElementById('root')!).render(
  <ErrorBoundary>
    <QueryClientProvider client={queryClient}>
      <UiOverlayProvider>
        <AppProvider>
          <App />
        </AppProvider>
      </UiOverlayProvider>
    </QueryClientProvider>
  </ErrorBoundary>,
);
