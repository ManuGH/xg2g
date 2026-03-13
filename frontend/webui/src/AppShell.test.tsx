import { lazy, type ReactNode } from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import AppShell from './AppShell';

const mockUseAppContext = vi.fn();

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (_key: string, options?: { defaultValue?: string }) => options?.defaultValue ?? 'Loading…',
  }),
}));

vi.mock('./context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
}));

vi.mock('./components/Navigation', () => ({
  default: () => <div data-testid="navigation-stub" />,
}));

const PendingRoute = lazy(async () => {
  await new Promise(() => {});
  return { default: () => <div>Pending route</div> };
});

function renderShell(initialEntries: string[] = ['/epg'], routeElement: ReactNode = <div>EPG view</div>) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route element={<AppShell />}>
          <Route path="/epg" element={routeElement} />
          <Route path="/settings" element={<div>Settings view</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe('AppShell', () => {
  beforeEach(() => {
    mockUseAppContext.mockReturnValue({
      auth: { token: 'stored-token', isAuthenticated: true },
      channels: { bouquets: [], channels: [], selectedBouquet: '', loading: false },
      dataLoaded: true,
      loadBouquetsAndChannels: vi.fn(),
    });
  });

  it('shows the page skeleton while initial shell hydration is in progress', () => {
    mockUseAppContext.mockReturnValue({
      auth: { token: 'stored-token', isAuthenticated: true },
      channels: { bouquets: [], channels: [], selectedBouquet: '', loading: true },
      dataLoaded: false,
      loadBouquetsAndChannels: vi.fn(),
    });

    renderShell();

    expect(screen.getByRole('status', { name: 'Loading…' })).toHaveAttribute('data-loading-variant', 'page');
    expect(screen.getByTestId('navigation-stub')).toBeInTheDocument();
  });

  it('shows the page skeleton for lazy route suspense fallback', () => {
    renderShell(['/epg'], <PendingRoute />);

    expect(screen.getByRole('status', { name: 'Loading…' })).toHaveAttribute('data-loading-variant', 'page');
  });
});
