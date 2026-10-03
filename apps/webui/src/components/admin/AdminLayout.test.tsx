import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AdminLayout } from './AdminLayout';

describe('AdminLayout Component', () => {
  it('renders all 10 Material 3 management sections', () => {
    render(<AdminLayout />);

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

  it('switches active section when clicked', async () => {
    render(<AdminLayout initialSection="account" />);

    const familyButton = screen.getByText('Family');
    await act(async () => {
      fireEvent.click(familyButton);
    });

    expect(screen.getAllByText(/Familienmitglieder/).length).toBeGreaterThan(0);
  });
});
