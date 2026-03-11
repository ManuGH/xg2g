import type { ComponentProps } from 'react';
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, it, vi } from 'vitest';

const mockUseAppContext = vi.fn();
const mockGetStoredToken = vi.fn(() => null);

const translations: Record<string, string> = {
  'app.initializing': 'Wird initialisiert',
  'auth.requiredTitle': 'Anmeldung erforderlich',
  'auth.tokenPlaceholder': 'Token eingeben',
  'auth.authenticate': 'Authentifizieren'
};

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { defaultValue?: string }) => translations[key] ?? options?.defaultValue ?? key,
  }),
}));

vi.mock('./context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
}));

vi.mock('./components/Navigation', () => ({
  default: () => <div data-testid="navigation-stub" />,
}));

vi.mock('./components/ui', () => ({
  Button: ({ children, ...props }: ComponentProps<'button'>) => <button {...props}>{children}</button>,
}));

vi.mock('./utils/logging', () => ({
  debugLog: vi.fn(),
  redactToken: vi.fn((token: string | null | undefined) => token ?? ''),
}));

vi.mock('./utils/tokenStorage', () => ({
  getStoredToken: () => mockGetStoredToken(),
}));

vi.mock('./components/Files', () => ({
  default: () => <div>Files view</div>,
}));

describe('App', () => {
  beforeEach(() => {
    mockUseAppContext.mockReturnValue({
      view: 'files',
      auth: { token: '', isAuthenticated: false },
      showAuth: true,
      setShowAuth: vi.fn(),
      setToken: vi.fn(),
      channels: { bouquets: [], channels: [], selectedBouquet: null },
      playback: { playingChannel: null },
      initializing: false,
      dataLoaded: true,
      checkConfigAndLoad: vi.fn(),
      setPlayingChannel: vi.fn(),
      setView: vi.fn(),
      loadChannels: vi.fn(),
      handlePlay: vi.fn()
    });
    mockGetStoredToken.mockReturnValue(null);
  });

  it('renders translated auth modal copy', async () => {
    const { default: App } = await import('./App');

    render(<App />);

    screen.getByRole('heading', { name: 'Anmeldung erforderlich' });
    screen.getByPlaceholderText('Token eingeben');
    screen.getByRole('button', { name: 'Authentifizieren' });
  });
});
