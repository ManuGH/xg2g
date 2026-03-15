import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import Navigation from './Navigation';
import { ROUTE_MAP } from '../routes';


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

    screen.getByRole('navigation', { name: 'Main navigation' });
    screen.getByRole('navigation', { name: 'Mobile navigation', hidden: true });
    expect(screen.getAllByText('Control').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Browse').length).toBeGreaterThan(0);
    expect(screen.getAllByText('System').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    screen.getByText('Navigation');
    screen.getByText('Control surfaces', { selector: 'h2' });
    screen.getByText('Close');
  });

  it('promotes settings into the mobile primary row and keeps timers in the more sheet', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    const mobileNav = screen.getByRole('navigation', { name: 'Mobile navigation', hidden: true });
    const mobileLinks = screen.getAllByRole('link', { hidden: true }).filter((link) => mobileNav.contains(link));
    expect(mobileLinks.map((link) => link.textContent)).toContain('Settings');
    expect(mobileLinks.map((link) => link.textContent)).not.toContain('Timers');

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    expect(screen.getByRole('link', { name: 'Timers' })).toHaveAttribute('href', ROUTE_MAP.timers);
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

    expect(screen.getByRole('button', { name: 'More', hidden: true })).not.toHaveAttribute('aria-current');
  });

  it('marks the more button active when an overflow route is active', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.logs]}>
        <Navigation />
      </MemoryRouter>
    );

    expect(screen.getByRole('button', { name: 'More', hidden: true })).toHaveAttribute('aria-current', 'page');
  });

  it('opens the more sheet as a dialog and focuses the close button', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));

    const sheetTitle = screen.getByText('Control surfaces', { selector: 'h2' });
    const dialog = sheetTitle.closest('[role="dialog"]');
    expect(dialog).toBeInTheDocument();
    await waitFor(() => {
      expect(within(dialog as HTMLElement).getByRole('button', { name: 'Close', hidden: true })).toHaveFocus();
    });
  });

  it('closes the more sheet on escape, restores focus, and releases scroll lock', async () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.dashboard]}>
        <Navigation />
      </MemoryRouter>
    );

    const moreButton = screen.getByRole('button', { name: 'More', hidden: true });
    fireEvent.click(moreButton);

    expect(document.body.style.overflow).toBe('hidden');
    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Control surfaces' })).toBeNull();
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

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));
    fireEvent.click(screen.getByRole('link', { name: 'Timers' }));

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.timers);
      expect(screen.queryByRole('dialog', { name: 'Control surfaces' })).toBeNull();
    });
  });
});
