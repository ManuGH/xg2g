import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../context/HouseholdProfilesContext';
import { PendingChangesProvider, usePendingChanges } from '../context/PendingChangesContext';
import { setClientAuthToken } from '../services/clientWrapper';
import Navigation from './Navigation';
import { ROUTE_MAP } from '../routes';

const { promptPin, toast } = vi.hoisted(() => ({
  promptPin: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    promptPin,
    toast,
  }),
}));

function LocationProbe() {
  const { pathname } = useLocation();
  return <div data-testid="pathname">{pathname}</div>;
}

function DirtyGuardProbe({ allowNavigation }: { allowNavigation: boolean }) {
  const { setPendingChangesGuard } = usePendingChanges();

  useEffect(() => {
    setPendingChangesGuard({
      isDirty: true,
      confirmDiscard: () => Promise.resolve(allowNavigation),
    });

    return () => {
      setPendingChangesGuard(null);
    };
  }, [allowNavigation, setPendingChangesGuard]);

  return null;
}

function renderWithProviders(children: ReactNode, initialEntries: string[] = [ROUTE_MAP.dashboard]) {
  return render(
    <PendingChangesProvider>
      <HouseholdProfilesProvider>
        <MemoryRouter initialEntries={initialEntries}>
          {children}
        </MemoryRouter>
      </HouseholdProfilesProvider>
    </PendingChangesProvider>
  );
}

describe('Navigation', () => {
  afterEach(() => {
    vi.clearAllMocks();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  it('renders translated section labels and sheet copy', () => {
    renderWithProviders(
      <>
        <Navigation onLogout={() => {}} />
      </>
    );

    screen.getByRole('navigation', { name: 'Main navigation' });
    screen.getByRole('navigation', { name: 'Mobile navigation', hidden: true });
    expect(screen.getAllByText('Start').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Media').length).toBeGreaterThan(0);
    expect(screen.getAllByText('System').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    screen.getByText('Navigation');
    screen.getByText('More sections', { selector: 'h2' });
    screen.getByText('Close');
  });

  it('keeps system and settings in the more sheet while the primary row stays focused on core routes', () => {
    renderWithProviders(
      <>
        <Navigation />
      </>
    );

    const mobileNav = screen.getByRole('navigation', { name: 'Mobile navigation', hidden: true });
    const mobileLinks = screen.getAllByRole('link', { hidden: true }).filter((link) => mobileNav.contains(link));
    expect(mobileLinks.map((link) => link.textContent)).toContain('Start');
    expect(mobileLinks.map((link) => link.textContent)).toContain('Live TV');
    expect(mobileLinks.map((link) => link.textContent)).toContain('Recordings');
    expect(mobileLinks.map((link) => link.textContent)).not.toContain('System');
    expect(mobileLinks.map((link) => link.textContent)).not.toContain('Settings');

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute('href', ROUTE_MAP.settings);
    expect(screen.getByRole('link', { name: 'System' })).toHaveAttribute('href', ROUTE_MAP.system);
  });

  it('navigates via links and marks the active route', async () => {
    renderWithProviders(
      <>
        <Navigation />
        <LocationProbe />
      </>,
      [ROUTE_MAP.epg]
    );

    expect(screen.getByRole('link', { name: 'Live TV' })).toHaveAttribute('aria-current', 'page');

    fireEvent.click(screen.getByRole('link', { name: 'Start' }));

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.dashboard);
    });
  });

  it('does not mark the more button active when a primary route is active', () => {
    renderWithProviders(
      <>
        <Navigation />
      </>
    );

    expect(screen.getByRole('button', { name: 'More', hidden: true })).not.toHaveAttribute('aria-current');
  });

  it('marks the more button active when an overflow route is active', () => {
    renderWithProviders(
      <>
        <Navigation />
      </>,
      [ROUTE_MAP.system]
    );

    expect(screen.getByRole('button', { name: 'More', hidden: true })).toHaveAttribute('aria-current', 'page');
  });

  it('shows the current nested mobile path for subsection routes', () => {
    renderWithProviders(
      <>
        <Navigation />
      </>,
      ['/settings?section=advanced&tool=files']
    );

    expect(screen.getByText('Current area')).toBeInTheDocument();
    expect(screen.getByText('Settings / Expert tools / Files')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    expect(screen.getAllByText('Settings / Expert tools / Files').length).toBeGreaterThan(0);
  });

  it('opens the more sheet as a dialog and focuses the close button', async () => {
    renderWithProviders(
      <>
        <Navigation />
      </>
    );

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    const sheetTitle = screen.getByText('More sections', { selector: 'h2' });
    const dialog = sheetTitle.closest('[role="dialog"]');
    expect(dialog).toBeInTheDocument();
    await waitFor(() => {
      expect(within(dialog as HTMLElement).getByRole('button', { name: 'Close', hidden: true })).toHaveFocus();
    });
  });

  it('closes the more sheet on escape, restores focus, and releases scroll lock', async () => {
    renderWithProviders(
      <>
        <Navigation />
      </>
    );

    const moreButton = screen.getByRole('button', { name: 'More', hidden: true });
    fireEvent.click(moreButton);

    expect(document.body.style.overflow).toBe('hidden');
    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'More sections' })).toBeNull();
      expect(moreButton).toHaveFocus();
    });
    expect(document.body.style.overflow).toBe('');
  });

  it('closes the more sheet after navigating to an overflow route', async () => {
    renderWithProviders(
      <>
        <Navigation />
        <LocationProbe />
      </>
    );

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));
    fireEvent.click(screen.getByRole('link', { name: 'System' }));

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.system);
      expect(screen.queryByRole('dialog', { name: 'More sections' })).toBeNull();
    });
  });

  it('keeps the current route when pending changes reject navigation', async () => {
    renderWithProviders(
      <>
        <DirtyGuardProbe allowNavigation={false} />
        <Navigation />
        <LocationProbe />
      </>
    );

    fireEvent.click(screen.getByRole('link', { name: 'Live TV' }));

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.dashboard);
    });
  });
});
