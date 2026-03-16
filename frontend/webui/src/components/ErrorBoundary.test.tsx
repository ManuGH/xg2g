import { fireEvent, render, screen } from '@testing-library/react';
import { Link, MemoryRouter, Outlet, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ErrorBoundary from './ErrorBoundary';


vi.mock('../utils/logging', () => ({
  debugError: vi.fn(),
}));

let shouldThrow = false;

function CrashRoute() {
  if (shouldThrow) {
    throw new Error('boom');
  }

  return <div>Recovered route</div>;
}

function SafeRoute() {
  return <div>Safe route</div>;
}

function RouteScopedBoundaryHarness() {
  const { pathname } = useLocation();

  return (
    <>
      <Link to="/safe">Go safe</Link>
      <ErrorBoundary resetKey={pathname} homeHref="/dashboard" titleAs="h3">
        <Outlet />
      </ErrorBoundary>
    </>
  );
}

describe('ErrorBoundary', () => {
  const originalError = console.error;

  beforeEach(() => {
    shouldThrow = false;
    console.error = vi.fn();
  });

  afterEach(() => {
    console.error = originalError;
    vi.clearAllMocks();
  });

  it('retries the current route after resetting the boundary', () => {
    shouldThrow = true;

    render(
      <MemoryRouter>
        <ErrorBoundary titleAs="h3">
          <CrashRoute />
        </ErrorBoundary>
      </MemoryRouter>
    );

    screen.getByRole('heading', { name: 'boom' });

    shouldThrow = false;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(screen.getByText('Recovered route')).toBeInTheDocument();
  });

  it('resets when the route pathname changes', async () => {
    shouldThrow = true;

    render(
      <MemoryRouter initialEntries={['/crash']}>
        <Routes>
          <Route element={<RouteScopedBoundaryHarness />}>
            <Route path="/crash" element={<CrashRoute />} />
            <Route path="/safe" element={<SafeRoute />} />
          </Route>
        </Routes>
      </MemoryRouter>
    );

    screen.getByRole('heading', { name: 'boom' });

    fireEvent.click(screen.getByRole('link', { name: 'Go safe' }));

    expect(await screen.findByText('Safe route')).toBeInTheDocument();
  });
});
