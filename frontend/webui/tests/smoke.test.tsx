import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import Settings from '../src/components/Settings';
import { describe, it, expect, vi } from 'vitest';

// Mock Config component (child) to isolate Settings test
vi.mock('../src/components/Config', async (importOriginal) => {
  const actual = await importOriginal<any>();
  return {
    ...actual,
    default: () => <div data-testid="mock-config">Config Component</div>,
    // Smoke tests focus on the Settings page contract, not setup gating.
    isConfigured: () => true,
  };
});

// Mock API client
vi.mock('../src/client-ts', () => ({
  getSystemScanStatus: vi.fn().mockResolvedValue({ data: { state: 'idle' } }),
  triggerSystemScan: vi.fn(),
  // P0: Reviewer requirement - Mock the ACTUAL config call
  getSystemConfig: vi.fn().mockResolvedValue({
    data: {
      streaming: {
        deliveryPolicy: 'universal' // Strict backend contract value
      }
    }
  }),
}));

describe('Frontend Smoke Tests', () => {
  it('Settings page loads and shows Universal Policy (read-only) from API', async () => {
    render(<Settings />);

    // P0: Universal Policy Check
    // Expect strict read-only display
    // We verified above that we return 'universal' in the mock
    // The UI maps 'universal' -> "Universal (H.264/AAC/fMP4)"

    await waitFor(() => {
      // We look for the value constructed from API data
      // Inputs are checked by display value
      expect(screen.getByDisplayValue(/Universal \(H.264\/AAC\/fMP4\)/i)).toBeInTheDocument();

      // Removed fragile "ADR-00X" assertion as per review
      expect(screen.getByText(/Strict Universal-Only/i)).toBeInTheDocument();
    });

    // Verify input is disabled (read-only)
    const input = screen.getByDisplayValue(/Universal/i);
    expect(input).toBeDisabled();
  });

  it('No profile dropdown exists (Thin Client Contract)', async () => {
    render(<Settings />);

    await waitFor(() => {
      expect(screen.getByDisplayValue(/Universal/i)).toBeInTheDocument();
    });

    // Query for legacy elements that SHOULD NOT be there
    const profileLabel = screen.queryByText(/Stream-Encoding-Profil/i);
    expect(profileLabel).not.toBeInTheDocument();

    // Also check by role/label if it were accessible
    const dropdown = screen.queryByRole('combobox', { name: /profile/i });
    expect(dropdown).not.toBeInTheDocument();
  });
});
