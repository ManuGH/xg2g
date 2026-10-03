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
    it('presents localized error in English and preserves browser network error in diagnostic detail', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Error loading viewing profiles/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('Failed to fetch');
    });

    it('presents localized error in German and preserves browser network error in diagnostic detail', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));

      renderWithProviders(<ProfileManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Fehler beim Laden der Sehprofile/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('Failed to fetch');
    });

    it('presents localized error when backend returns HTTP 500 and keeps detail separate', async () => {
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

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('Database connection failed');
    });
  });

  describe('DevicesManagementSection error handling', () => {
    it('presents localized error in English with separate error detail for API rejection', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('403 Forbidden: Invalid device credentials'));

      renderWithProviders(<DevicesManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load devices/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('403 Forbidden: Invalid device credentials');
    });

    it('presents localized error in German with separate error detail for API rejection', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('403 Forbidden: Invalid device credentials'));

      renderWithProviders(<DevicesManagementSection />);

      await waitFor(() => {
        expect(screen.getByText(/Geräte konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('403 Forbidden: Invalid device credentials');
    });
  });

  describe('ParentalControlSection error handling', () => {
    it('presents localized error in English for network failure and keeps technical message in detail', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('NetworkError when attempting to fetch resource'));

      renderWithProviders(<ParentalControlSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load approval requests/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('NetworkError when attempting to fetch resource');
    });

    it('presents localized error in German for network failure and keeps technical message in detail', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('NetworkError when attempting to fetch resource'));

      renderWithProviders(<ParentalControlSection />);

      await waitFor(() => {
        expect(screen.getByText(/Freigabe-Anfragen konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('NetworkError when attempting to fetch resource');
    });
  });

  describe('ConcurrencySettingsSection error handling', () => {
    it('presents localized error in English on fetch failure', async () => {
      setTestLanguage('en');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Resource policy offline'));

      renderWithProviders(<ConcurrencySettingsSection />);

      await waitFor(() => {
        expect(screen.getByText(/Could not load resource limits/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('Resource policy offline');
    });

    it('presents localized error in German on fetch failure', async () => {
      setTestLanguage('de');
      globalThis.fetch = vi.fn().mockRejectedValue(new Error('Resource policy offline'));

      renderWithProviders(<ConcurrencySettingsSection />);

      await waitFor(() => {
        expect(screen.getByText(/Ressourcen-Limits konnten nicht geladen werden/i)).toBeInTheDocument();
      });

      const detail = screen.getByTestId('error-detail');
      expect(detail).toBeInTheDocument();
      expect(detail).toHaveTextContent('Resource policy offline');
    });
  });
});
