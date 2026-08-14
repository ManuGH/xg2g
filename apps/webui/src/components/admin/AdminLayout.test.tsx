import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AdminLayout } from './AdminLayout';

describe('AdminLayout Component', () => {
  it('renders all 10 Material 3 management sections', () => {
    render(<AdminLayout />);

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

  it('switches active section when clicked', () => {
    render(<AdminLayout initialSection="account" />);

    const familyButton = screen.getByText('Familie');
    fireEvent.click(familyButton);

    expect(screen.getAllByText(/Familienmitglieder/).length).toBeGreaterThan(0);
  });
});
