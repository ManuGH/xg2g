import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import Navigation from './Navigation';

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

describe('Navigation', () => {
  it('renders translated section labels and sheet copy', () => {
    render(
      <Navigation
        activeView="dashboard"
        onViewChange={() => {}}
        onLogout={() => {}}
      />
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
});
