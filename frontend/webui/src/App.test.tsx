import type { ComponentProps } from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Outlet } from 'react-router-dom';
import { beforeEach, describe, it, vi } from 'vitest';

const mockUseAppContext = vi.fn();
const mockUseHouseholdProfiles = vi.fn();
const mockToast = vi.fn();

vi.mock('./context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
}));

vi.mock('./context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

vi.mock('./context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    toast: mockToast,
    confirm: vi.fn(),
    promptPin: vi.fn(),
  }),
}));

vi.mock('./hooks/useServerQueries', () => ({
  useErrorCatalog: vi.fn(),
}));

vi.mock('./components/Navigation', () => ({
  default: () => <div data-testid="navigation-stub" />,
}));

vi.mock('./components/BootstrapGate', () => ({
  default: () => <Outlet />,
}));

vi.mock('./components/ui', () => ({
  Button: ({ children, ...props }: ComponentProps<'button'>) => <button {...props}>{children}</button>,
}));

vi.mock('./components/Files', () => ({
  default: () => <div>Files view</div>,
}));

vi.mock('./features/epg/EPG', () => ({
  default: () => <div>EPG view</div>,
}));

describe('App', () => {
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

    mockUseAppContext.mockReturnValue({
      auth: { token: '', isAuthenticated: false },
      setToken: vi.fn(),
      channels: { bouquets: [], channels: [], selectedBouquet: '', loading: false },
      playback: { playingChannel: null },
      dataLoaded: true,
      setPlayingChannel: vi.fn(),
      loadChannels: vi.fn(),
      loadBouquetsAndChannels: vi.fn(),
      handlePlay: vi.fn()
    });
    mockUseHouseholdProfiles.mockReturnValue({
      profiles: [defaultProfile],
      selectedProfile: defaultProfile,
      selectedProfileId: defaultProfile.id,
      isReady: true,
      pinConfigured: false,
      isUnlocked: true,
      selectProfile: vi.fn().mockResolvedValue(true),
      ensureUnlocked: vi.fn().mockResolvedValue(true),
      saveProfile: vi.fn().mockResolvedValue(undefined),
      deleteProfile: vi.fn().mockResolvedValue(undefined),
      toggleFavoriteService: vi.fn(),
      isFavoriteService: vi.fn().mockReturnValue(false),
      canAccessDvrPlayback: true,
      canManageDvr: true,
      canAccessSettings: true,
    });
  });

  it('renders the route-matched view and redirects root to epg', async () => {
    const { default: App } = await import('./App');

    const { unmount } = render(
      <MemoryRouter initialEntries={['/files']}>
        <App />
      </MemoryRouter>
    );

    await screen.findByText('Files view');

    unmount();

    render(
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    );

    await screen.findByText('EPG view');
  });
});
