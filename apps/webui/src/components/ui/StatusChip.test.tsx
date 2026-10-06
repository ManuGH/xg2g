// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { StatusChip, computeAccessibleChipName } from './StatusChip';

describe('StatusChip', () => {
  it('always appends the localized state to the accessible name', () => {
    const accessible = computeAccessibleChipName('System gesund', 'success', 'Erfolgreich');
    expect(accessible).toBe('System gesund – Erfolgreich');
    expect(computeAccessibleChipName('Lokal', 'success', 'Erfolgreich')).toBe('Lokal – Erfolgreich');
  });

  it('appends localized state for count labels that do not express status alone', () => {
    const accessible = computeAccessibleChipName('2 Sitzungen', 'live', 'Live');
    expect(accessible).toBe('2 Sitzungen – Live');
  });

  it('renders role="status" and correct localized aria-label without English state keys', () => {
    render(<StatusChip state="live" label="2 Sitzungen" />);
    const chip = screen.getByRole('status');
    expect(chip).toBeInTheDocument();
    // Must never contain raw English key "live" in lowercase after hyphen (e.g. "2 Sitzungen - live")
    expect(chip.getAttribute('aria-label')).toBe('2 Sitzungen – Live');
  });

  it('uses an explicit accessible name when supplied', () => {
    render(<StatusChip state="success" label="System gesund" ariaLabel="Healthy system" />);
    const chip = screen.getByRole('status');
    // Must NOT be "System gesund - success"
    expect(chip.getAttribute('aria-label')).toBe('Healthy system');
  });
});
