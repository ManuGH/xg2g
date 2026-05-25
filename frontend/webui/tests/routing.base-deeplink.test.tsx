// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Navigate, Route, Routes } from 'react-router-dom';
import AppRouter, { computeRouterBasename } from '../src/AppRouter';
import { ROUTE_MAP } from '../src/routes';

// Mirror of App.tsx's route shape (real ROUTE_MAP paths + the same catch-all
// that redirects unmatched paths to the dashboard). Page bodies are markers:
// this is a routing-wiring test, not a page-render test.
function TestRoutes() {
  return (
    <Routes>
      <Route path={ROUTE_MAP.dashboard} element={<div>DASHBOARD_PAGE</div>} />
      <Route path={ROUTE_MAP.epg} element={<div>EPG_PAGE</div>} />
      <Route path="/" element={<Navigate to={ROUTE_MAP.dashboard} replace />} />
      <Route path="*" element={<Navigate to={ROUTE_MAP.dashboard} replace />} />
    </Routes>
  );
}

describe('deep links under the /ui base', () => {
  beforeEach(() => {
    // Simulate the production serving condition: app built/served under /ui/.
    vi.stubEnv('BASE_URL', '/ui/');
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    window.history.replaceState({}, '', '/');
  });

  it('normalizes the base into a router basename', () => {
    expect(computeRouterBasename('/ui/')).toBe('/ui');
    expect(computeRouterBasename('/ui')).toBe('/ui');
    expect(computeRouterBasename('/')).toBeUndefined();
    expect(computeRouterBasename('')).toBeUndefined();
    expect(computeRouterBasename(undefined)).toBeUndefined();
  });

  it('resolves a bookmarked /ui/epg to the EPG page, not the dashboard', () => {
    // The exact case a user bookmarks while the app is served under /ui/.
    window.history.replaceState({}, '', '/ui/epg');

    render(
      <AppRouter>
        <TestRoutes />
      </AppRouter>,
    );

    expect(screen.getByText('EPG_PAGE')).toBeInTheDocument();
    expect(screen.queryByText('DASHBOARD_PAGE')).not.toBeInTheDocument();
  });
});
