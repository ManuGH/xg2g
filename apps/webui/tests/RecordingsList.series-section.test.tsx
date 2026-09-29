import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, it, expect, vi } from 'vitest';
import RecordingsList from '../src/components/RecordingsList';

const { getSeriesRules, getServices, getRecordings } = vi.hoisted(() => ({
  getSeriesRules: vi.fn().mockResolvedValue({ data: [] }),
  getServices: vi.fn().mockResolvedValue({ data: [] }),
  getRecordings: vi.fn().mockResolvedValue({ data: { roots: [], recordings: [] } }),
}));

vi.mock('../src/client-ts', () => ({
  getSeriesRules,
  getServices,
  getRecordings,
}));

vi.mock('../src/context/AppContext', () => ({
  useAppContext: () => ({
    auth: { token: 'test-token' },
  }),
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm: vi.fn(),
    toast: vi.fn(),
  }),
}));

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => ({
    selectedProfile: { id: 'p1', name: 'Main' },
    canAccessDvrPlayback: true,
    canManageDvr: true,
    isReady: true,
  }),
}));

describe('RecordingsList series section routing', () => {
  it('renders SeriesManager when URL contains section=series', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/recordings?section=series']}>
          <Routes>
            <Route path="/recordings" element={<RecordingsList />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );

    // Context bar breadcrumb
    await waitFor(() => {
      expect(screen.getByText(/Back to recordings|Zurück zu Aufnahmen/i)).toBeInTheDocument();
    });

    // Check that SeriesManager loaded
    expect(screen.getByText(/Series Rules|Serienregeln/i)).toBeInTheDocument();
  });
});
