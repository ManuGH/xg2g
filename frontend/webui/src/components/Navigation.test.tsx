import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import Navigation from './Navigation';
import { ROUTE_MAP } from '../routes';

const translations: Record<string, string> = {
  'nav.dashboard': 'Dashboard',
  'nav.epg': 'TV/EPG',
  'nav.recordings': 'Aufnahmen',
  'nav.timers': 'Timer',
  'nav.series': 'Serien',
  'nav.files': 'Dateien',
  'nav.logs': 'Logs',
  'nav.playerSettings': 'Einstellungen',
  'nav.system': 'System',
  'nav.logout': 'Abmelden',
  'nav.more': 'Mehr',
  'nav.sectionControl': 'Steuerung',
  'nav.sectionBrowse': 'Durchsuchen',
  'nav.sectionSystem': 'Systembereich',
  'nav.sheetEyebrow': 'Navigation',
  'nav.sheetTitle': 'Steuerflaechen',
  'nav.mainNavigationLabel': 'Hauptnavigation',
  'nav.mobileNavigationLabel': 'Mobile Navigation',
  'nav.closeNavigationLabel': 'Navigation schliessen',
  'common.close': 'Schliessen'
};

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { defaultValue?: string }) => translations[key] ?? options?.defaultValue ?? key,
  }),
}));

function LocationProbe() {
  const { pathname } = useLocation();
  return <div data-testid="pathname">{pathname}</div>;
}

describe('Navigation', () => {
  it('renders translated section labels and sheet copy', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation onLogout={() => {}} />
      </MemoryRouter>
    );

    screen.getByRole('navigation', { name: 'Hauptnavigation' });
    screen.getByRole('navigation', { name: 'Mobile Navigation', hidden: true });
    expect(screen.getAllByText('Steuerung').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Durchsuchen').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Systembereich').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'Mehr', hidden: true }));

    screen.getByText('Navigation');
    screen.getByText('Steuerflaechen', { selector: 'h2' });
    screen.getByText('Schliessen');
  });

  it('promotes settings into the mobile primary row and keeps timers in the more sheet', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    const mobileNav = screen.getByRole('navigation', { name: 'Mobile Navigation', hidden: true });
    const mobileLinks = screen.getAllByRole('link', { hidden: true }).filter((link) => mobileNav.contains(link));
    expect(mobileLinks.map((link) => link.textContent)).toContain('Einstellungen');
    expect(mobileLinks.map((link) => link.textContent)).not.toContain('Timer');

    fireEvent.click(screen.getByRole('button', { name: 'Mehr', hidden: true }));

    expect(screen.getByRole('link', { name: 'Timer' })).toHaveAttribute('href', ROUTE_MAP.timers);
  });

  it('navigates via links and marks the active route', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Navigation />
        <LocationProbe />
      </MemoryRouter>
    );

    expect(screen.getByRole('link', { name: 'TV/EPG' })).toHaveAttribute('aria-current', 'page');

    fireEvent.click(screen.getByRole('link', { name: 'Dashboard' }));

    expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.dashboard);
  });

  it('does not mark the more button active when a primary route is active', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    expect(screen.getByRole('button', { name: 'Mehr', hidden: true })).not.toHaveAttribute('aria-current');
  });

  it('marks the more button active when an overflow route is active', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.logs]}>
        <Navigation />
      </MemoryRouter>
    );

    expect(screen.getByRole('button', { name: 'Mehr', hidden: true })).toHaveAttribute('aria-current', 'page');
  });

  it('opens the more sheet as a dialog and focuses the close button', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole('button', { name: 'Mehr', hidden: true }));

    const sheetTitle = screen.getByText('Steuerflaechen', { selector: 'h2' });
    const dialog = sheetTitle.closest('[role="dialog"]');
    expect(dialog).toBeInTheDocument();
    await waitFor(() => {
      expect(within(dialog as HTMLElement).getByRole('button', { name: 'Schliessen', hidden: true })).toHaveFocus();
    });
  });

  it('closes the more sheet on escape, restores focus, and releases scroll lock', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    const moreButton = screen.getByRole('button', { name: 'Mehr', hidden: true });
    fireEvent.click(moreButton);

    expect(document.body.style.overflow).toBe('hidden');
    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Steuerflaechen' })).toBeNull();
      expect(moreButton).toHaveFocus();
    });
    expect(document.body.style.overflow).toBe('');
  });

  it('closes the more sheet after navigating to an overflow route', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
        <LocationProbe />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole('button', { name: 'Mehr', hidden: true }));
    fireEvent.click(screen.getByRole('link', { name: 'Timer' }));

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.timers);
      expect(screen.queryByRole('dialog', { name: 'Steuerflaechen' })).toBeNull();
    });
  });
});
