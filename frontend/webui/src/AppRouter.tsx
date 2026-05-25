// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import type { ReactNode } from 'react';
import { BrowserRouter } from 'react-router-dom';

/**
 * computeRouterBasename derives the react-router basename from the build-time
 * base path (Vite's `base`, surfaced as import.meta.env.BASE_URL). It is the
 * single source of truth that keeps the router aligned with the path the app is
 * actually served under (e.g. "/ui/"), so deep links and bookmarks resolve
 * instead of falling through to the catch-all. A root base ("/") yields
 * undefined, i.e. react-router's default.
 */
export function computeRouterBasename(rawBase: string | undefined): string | undefined {
  if (!rawBase) return undefined;
  const trimmed = rawBase.replace(/\/+$/, '');
  return trimmed.length > 0 ? trimmed : undefined;
}

/**
 * AppRouter is the single router wiring shared by the app entrypoint and the
 * routing tests, so the basename behaviour proven in tests is the exact wiring
 * that ships.
 */
export default function AppRouter({ children }: { children: ReactNode }) {
  return (
    <BrowserRouter basename={computeRouterBasename(import.meta.env.BASE_URL)}>
      {children}
    </BrowserRouter>
  );
}
