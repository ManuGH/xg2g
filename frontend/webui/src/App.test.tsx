import type { ComponentProps } from 'react';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Outlet } from 'react-router-dom';
import { beforeEach, describe, it, vi } from 'vitest';

const mockUseAppContext = vi.fn();

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { defaultValue?: string }) => options?.defaultValue ?? key,
  }),
}));

vi.mock('./context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
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
