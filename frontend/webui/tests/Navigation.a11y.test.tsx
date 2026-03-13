import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { describe, it, expect, vi } from 'vitest';
import Navigation from '../src/components/Navigation';
import { ROUTE_MAP } from '../src/routes';

function LocationProbe() {
  const { pathname } = useLocation();
  return <div data-testid="pathname">{pathname}</div>;
}

describe('Navigation semantics', () => {
  it('renders nav items as links and sets aria-current on the active route', () => {
    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Navigation />
        <LocationProbe />
      </MemoryRouter>
    );

    const links = screen.getAllByRole('link');
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      expect(link).toHaveAttribute('href');
    }

    const current = links.filter(link => link.getAttribute('aria-current') === 'page');
    expect(current).toHaveLength(1);

    fireEvent.click(screen.getByRole('link', { name: /nav\.dashboard/i }));
    expect(screen.getByTestId('pathname')).toHaveTextContent(ROUTE_MAP.dashboard);
  });

  it('renders a logout action when provided', () => {
    const onLogout = vi.fn();

    render(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Navigation onLogout={onLogout} />
      </MemoryRouter>
    );

    fireEvent.click(screen.getByRole('button', { name: /nav\.logout/i }));
    expect(onLogout).toHaveBeenCalledTimes(1);
  });
});
