import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, afterEach, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AppProvider } from '../../context/AppContext';
import { ProfileManagementSection } from './ProfileManagementSection';
import { DevicesManagementSection } from './DevicesManagementSection';
import { ParentalControlSection } from './ParentalControlSection';
import { ConcurrencySettingsSection } from './ConcurrencySettingsSection';

const setTestLanguage = (lang: 'en' | 'de') => {
  (globalThis as any).__setTestLanguage?.(lang);
};

const renderWithProviders = (ui: React.ReactElement) => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AppProvider>{ui}</AppProvider>
    </QueryClientProvider>
  );
};

describe('Admin Error Presentation & Diagnostic Detail Separation', () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  describe('ProfileManagementSection error handling', () => {
    it('presents localized error in English and does not expose technical message in the UI', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Error loading viewing profiles/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/Failed to fetch/i)).not.toBeInTheDocument();
    });

    it('presents localized error in German and does not expose technical message in the UI', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Fehler beim Laden der Sehprofile/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/Failed to fetch/i)).not.toBeInTheDocument();
    });

    it('presents localized error when backend returns HTTP 500 without leaking backend error detail into DOM', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        headers: new Headers(),
        json: async () => ({ detail: 'Database connection failed' }),
      } as any);

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Error loading viewing profiles/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/Database connection failed/i)).not.toBeInTheDocument();
    });

    it('displays correlation reference separately when backend provides X-Request-Id header', async () => {
      setTestLanguage('en');
      const headers = new Headers();
      headers.set('X-Request-Id', 'req-trace-456');

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        headers,
        json: async () => ({ detail: 'Database pool exhausted' }),
      } as any);

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Error loading viewing profiles/i)).toBeInTheDocument();
      });

      const ref = screen.getByTestId('error-reference');
      expect(ref).toBeInTheDocument();
      expect(ref).toHaveTextContent('(req-trace-456)');
      expect(screen.queryByText(/Database pool exhausted/i)).not.toBeInTheDocument();
      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
    });
  });

  describe('DevicesManagementSection error handling', () => {
    it('presents localized error in English without leaking raw API rejection to DOM', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('403 Forbidden: Invalid device credentials'));

      renderWithProviders(<DevicesManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load devices/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/403 Forbidden/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/Invalid device credentials/i)).not.toBeInTheDocument();
    });

    it('presents localized error in German without leaking raw API rejection to DOM', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('403 Forbidden: Invalid device credentials'));

      renderWithProviders(<DevicesManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Geräte konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/403 Forbidden/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/Invalid device credentials/i)).not.toBeInTheDocument();
    });
  });

  describe('ParentalControlSection error handling', () => {
    it('presents localized error in English for network failure without exposing raw error in DOM', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('NetworkError when attempting to fetch resource'));

      renderWithProviders(<ParentalControlSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load approval requests/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/NetworkError/i)).not.toBeInTheDocument();
    });

    it('presents localized error in German for network failure without exposing raw error in DOM', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('NetworkError when attempting to fetch resource'));

      renderWithProviders(<ParentalControlSection />);

      await waitFor(() => {
        expect(screen.getByText(/Freigabe-Anfragen konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/NetworkError/i)).not.toBeInTheDocument();
    });
  });

  describe('ConcurrencySettingsSection error handling', () => {
    it('presents localized error in English without leaking technical message to DOM', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Resource policy offline'));

      renderWithProviders(<ConcurrencySettingsSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load resource limits/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/Resource policy offline/i)).not.toBeInTheDocument();
    });

    it('presents localized error in German without leaking technical message to DOM', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Resource policy offline'));

      renderWithProviders(<ConcurrencySettingsSection />);

      await waitFor(() => {
        expect(screen.getByText(/Ressourcen-Limits konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      expect(screen.queryByTestId('error-detail')).not.toBeInTheDocument();
      expect(screen.queryByText(/Resource policy offline/i)).not.toBeInTheDocument();
    });
  });
});
