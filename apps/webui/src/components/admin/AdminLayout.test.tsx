import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, beforeEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AppProvider } from '../../context/AppContext';
import { AdminLayout } from './AdminLayout';

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

describe('AdminLayout Component', () => {
  beforeEach(() => {
    setTestLanguage('en');
  });

  describe('English locale (default)', () => {
    it('renders all 10 navigation items in English', () => {
      renderWithProviders(<AdminLayout />);

      expect(screen.getByText('Household & Administration')).toBeInTheDocument();
      expect(screen.getAllByText('Account').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Family').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Profiles').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Devices').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Security').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Access Times').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Parental Control').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Recordings').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Concurrency').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Notifications & Audit').length).toBeGreaterThan(0);
    });

    it('renders and switches through reachable admin sections with English section content', async () => {
      renderWithProviders(<AdminLayout initialSection="account" />);

      // Account section content
      expect(screen.getByText('Main Account Data')).toBeInTheDocument();
      expect(screen.getByText('Account status: Active (Admin)')).toBeInTheDocument();

      // Family section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Family/i }));
      });
      expect(screen.getByText('Family Members & Invitations')).toBeInTheDocument();
      expect(screen.getByText('✉️ Invite Member')).toBeInTheDocument();

      // Profiles section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Profiles/i }));
      });
      expect(screen.getByText('Managed Viewing Profiles')).toBeInTheDocument();
      expect(screen.getByText('New Profile')).toBeInTheDocument();

      // Devices section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Devices/i }));
      });
      expect(screen.getAllByText('Connected Devices & 30-Day Trust').length).toBeGreaterThan(0);

      // Security section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Security/i }));
      });
      expect(screen.getByText('Security, Admin Access & Passkeys')).toBeInTheDocument();

      // Access Times section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Access Times/i }));
      });
      expect(screen.getByText('Daily Access Times & Curfews')).toBeInTheDocument();
      expect(screen.getByText('Allowed Weekdays')).toBeInTheDocument();

      // Parental section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Parental Control/i }));
      });
      expect(screen.getByText('Parental Control & Live Approvals')).toBeInTheDocument();

      // Recordings section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Recordings/i }));
      });
      expect(screen.getByText('Recordings & Storage Quotas')).toBeInTheDocument();

      // Concurrency section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Concurrency/i }));
      });
      expect(screen.getByText('Concurrent Usage & Tuner Arbitration')).toBeInTheDocument();
      expect(screen.getByText('📡 Max Live TV Channels')).toBeInTheDocument();

      // Audit section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Notifications & Audit/i }));
      });
      expect(screen.getByText('Notifications & Immutable Audit Log')).toBeInTheDocument();
      expect(screen.getByText('📲 Browser WebPush & Push Notifications')).toBeInTheDocument();
    });
  });

  describe('German locale', () => {
    beforeEach(() => {
      setTestLanguage('de');
    });

    it('renders all 10 navigation items in German', () => {
      renderWithProviders(<AdminLayout />);

      expect(screen.getByText('Haushalt & Administration')).toBeInTheDocument();
      expect(screen.getAllByText('Konto').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Familie').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Profile').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Geräte').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Sicherheit').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Zugriffszeiten').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Jugendschutz').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Aufnahmen').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Gleichzeitige Nutzung').length).toBeGreaterThan(0);
      expect(screen.getAllByText('Benachrichtigungen & Audit').length).toBeGreaterThan(0);
    });

    it('renders and switches through reachable admin sections with German section content', async () => {
      renderWithProviders(<AdminLayout initialSection="account" />);

      // Account section content
      expect(screen.getByText('Hauptkontodaten')).toBeInTheDocument();
      expect(screen.getByText('Konto-Status: Aktiv (Admin)')).toBeInTheDocument();

      // Family section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Familie/i }));
      });
      expect(screen.getByText('Familienmitglieder & Einladungen')).toBeInTheDocument();
      expect(screen.getByText('✉️ Mitglied einladen')).toBeInTheDocument();

      // Profiles section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Profile/i }));
      });
      expect(screen.getByText('Verwaltete Sehprofile')).toBeInTheDocument();
      expect(screen.getByText('Neues Profil')).toBeInTheDocument();

      // Devices section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Geräte/i }));
      });
      expect(screen.getAllByText('Verbundene Geräte & 30-Tage-Vertrauen').length).toBeGreaterThan(0);

      // Security section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Sicherheit/i }));
      });
      expect(screen.getByText('Sicherheit, Admin-Zugang & Passkeys')).toBeInTheDocument();

      // Access Times section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Zugriffszeiten/i }));
      });
      expect(screen.getByText('Tägliche Zugriffszeiten & Sperrstunden')).toBeInTheDocument();
      expect(screen.getByText('Erlaubte Wochentage')).toBeInTheDocument();

      // Parental section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Jugendschutz/i }));
      });
      expect(screen.getByText('Jugendschutz & Live Freigaben')).toBeInTheDocument();

      // Recordings section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Aufnahmen/i }));
      });
      expect(screen.getByText('Aufnahmen & Speicherkontingente')).toBeInTheDocument();

      // Concurrency section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Gleichzeitige Nutzung/i }));
      });
      expect(screen.getByText('Gleichzeitige Nutzung & Tuner-Arbitrierung')).toBeInTheDocument();
      expect(screen.getByText('📡 Max Live TV Sender')).toBeInTheDocument();

      // Audit section
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Benachrichtigungen & Audit/i }));
      });
      expect(screen.getByText('Benachrichtigungen & Unveränderliches Audit-Protokoll')).toBeInTheDocument();
      expect(screen.getByText('📲 Browser-WebPush & Push-Benachrichtigungen')).toBeInTheDocument();
    });
  });
});
