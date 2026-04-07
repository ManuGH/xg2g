import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, describe, it, expect, vi } from 'vitest';
import Navigation from '../src/components/Navigation';
import { ROUTE_MAP } from '../src/routes';

const mockUseHouseholdProfiles = vi.fn();
const mockConfirmPendingChanges = vi.fn();

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

vi.mock('../src/context/PendingChangesContext', () => ({
  usePendingChanges: () => ({
    confirmPendingChanges: mockConfirmPendingChanges,
  }),
}));

function LocationProbe() {
  const { pathname } = useLocation();
  return <div data-testid="pathname">{pathname}</div>;
}

describe('Navigation semantics', () => {
  beforeEach(() => {
    const defaultProfile = {
      id: 'household-default',
      name: 'Haushalt',
      kind: 'adult' as const,
      maxFsk: null,
      allowedBouquets: [],
      allowedServiceRefs: [],
      favoriteServiceRefs: [],
      permissions: {
        dvrPlayback: true,
        dvrManage: true,
        settings: true,
      },
    };

    mockConfirmPendingChanges.mockResolvedValue(true);
    mockUseHouseholdProfiles.mockReturnValue({
      profiles: [defaultProfile],
      selectedProfile: defaultProfile,
      selectProfile: vi.fn().mockResolvedValue(true),
      ensureUnlocked: vi.fn().mockResolvedValue(true),
      pinConfigured: false,
      isUnlocked: true,
      canAccessDvrPlayback: true,
      canManageDvr: true,
      canAccessSettings: true,
    });
  });

  it('renders nav items as links and sets aria-current on the active route', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Navigation />
        <LocationProbe />
      </MemoryRouter>
    );

    const links = screen.getAllByRole('link');
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      expect(link).toHaveAttribute('href');
    }

    const current = links.filter(link => link.getAttribute('aria-current') === 'page');
    expect(current).toHaveLength(1);

    fireEvent.click(screen.getByRole('link', { name: /nav\.dashboard|dashboard/i }));
    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.dashboard);
    });
  });

  it('renders a logout action when provided', async () => {
    const onLogout = vi.fn();

    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Navigation onLogout={onLogout} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole('button', { name: /nav\.logout|logout/i }));
    await waitFor(() => {
      expect(onLogout).toHaveBeenCalledTimes(1);
    });
  });
});
