// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import { AppProvider } from '../src/context/AppContext';
import SecuritySettingsSection from '../src/components/settings/SecuritySettingsSection';
import * as passkeyApi from '../src/services/passkeyApi';

const {
  mockGetSystemConfig,
  mockGetServicesBouquets,
  mockGetServices,
  mockStoredToken,
  mockToast,
  mockUseHouseholdProfiles,
} = vi.hoisted(() => ({
  mockGetSystemConfig: vi.fn(),
  mockGetServicesBouquets: vi.fn(),
  mockGetServices: vi.fn(),
  mockStoredToken: { value: '' },
  mockToast: vi.fn(),
  mockUseHouseholdProfiles: vi.fn(),
}));

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    toast: mockToast,
    confirm: vi.fn(),
    promptPin: vi.fn(),
  }),
}));

vi.mock('../src/client-ts', () => ({
  getSystemConfig: (...args: unknown[]) => mockGetSystemConfig(...args),
  getServicesBouquets: (...args: unknown[]) => mockGetServicesBouquets(...args),
  getServices: (...args: unknown[]) => mockGetServices(...args),
}));

vi.mock('../src/utils/tokenStorage', () => ({
  getStoredToken: () => mockStoredToken.value,
  setStoredToken: (val: string) => { mockStoredToken.value = val; },
  clearStoredToken: () => { mockStoredToken.value = ''; },
}));

describe('Passkey Public Auth Journey', () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: 0 },
      },
    });

    mockUseHouseholdProfiles.mockReturnValue({
      profiles: [],
      selectedProfile: null,
      selectProfile: vi.fn(),
      setProfiles: vi.fn(),
      pinConfigured: false,
    });

    // Mock WebAuthn Browser APIs
    (global as any).navigator.credentials = {
      create: vi.fn().mockResolvedValue({
        id: 'test_cred_id_123',
        rawId: new Uint8Array([1, 2, 3]).buffer,
        type: 'public-key',
        response: {
          clientDataJSON: new Uint8Array([4, 5, 6]).buffer,
          attestationObject: new Uint8Array([7, 8, 9]).buffer,
          getTransports: () => ['internal'],
        },
        getClientExtensionResults: () => ({}),
      }),
      get: vi.fn().mockResolvedValue({
        id: 'test_cred_id_123',
        rawId: new Uint8Array([1, 2, 3]).buffer,
        type: 'public-key',
        response: {
          clientDataJSON: new Uint8Array([4, 5, 6]).buffer,
          authenticatorData: new Uint8Array([7, 8, 9]).buffer,
          signature: new Uint8Array([10, 11, 12]).buffer,
          userHandle: new Uint8Array([13, 14, 15]).buffer,
        },
        getClientExtensionResults: () => ({}),
      }),
    };
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders Bootstrap Passkey Creation and mandates Recovery Code Confirmation', async () => {
    mockGetSystemConfig.mockResolvedValue({
      data: {
        configured: true,
        setupRequired: true,
        identityReady: false,
      },
    });

    vi.spyOn(passkeyApi, 'startPasskeyRegistration').mockResolvedValue({
      options: {
        publicKey: {
          challenge: 'test_challenge_base64',
          rp: { name: 'xg2g' },
          user: { id: 'admin_id', name: 'admin', displayName: 'Admin' },
          pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
        },
      },
    });

    vi.spyOn(passkeyApi, 'finishPasskeyRegistration').mockResolvedValue({
      status: 'bootstrap_completed',
      recoveryCodes: ['CODE-0001', 'CODE-0002', 'CODE-0003'],
    });

    vi.spyOn(passkeyApi, 'acknowledgeRecovery').mockResolvedValue(undefined);

    render(
      <QueryClientProvider client={queryClient}>
        <AppProvider>
          <MemoryRouter initialEntries={['/']}>
            <App />
          </MemoryRouter>
        </AppProvider>
      </QueryClientProvider>
    );

    // 1. Verify Bootstrap Passkey Screen rendered
    await waitFor(() => {
      expect(screen.getByTestId('bootstrap-passkey-surface')).toBeInTheDocument();
    });
    expect(screen.getByText('xg2g einrichten')).toBeInTheDocument();

    // 2. Click Passkey erstellen
    const createBtn = screen.getByTestId('create-passkey-button');
    await act(async () => {
      fireEvent.click(createBtn);
    });

    // 3. Verify Recovery Codes Backup Screen rendered
    await waitFor(() => {
      expect(screen.getByTestId('bootstrap-recovery-surface')).toBeInTheDocument();
    });
    expect(screen.getByText('CODE-0001')).toBeInTheDocument();

    // 4. Verify Finish button is disabled until confirmation checkbox is clicked
    const finishBtn = screen.getByTestId('finish-bootstrap-button');
    expect(finishBtn).toBeDisabled();

    const checkbox = screen.getByTestId('confirm-recovery-codes-checkbox');
    await act(async () => {
      fireEvent.click(checkbox);
    });
    expect(finishBtn).not.toBeDisabled();

    // 5. Complete Bootstrap (calls acknowledgeRecovery)
    await act(async () => {
      fireEvent.click(finishBtn);
    });
    expect(passkeyApi.acknowledgeRecovery).toHaveBeenCalledTimes(1);
  });

  it('renders Passkey Login View and handles usernameless sign-in', async () => {
    mockGetSystemConfig.mockRejectedValue({ status: 401 });

    vi.spyOn(passkeyApi, 'startPasskeyLogin').mockResolvedValue({
      options: {
        publicKey: {
          challenge: 'login_challenge_123',
        },
      },
    });

    vi.spyOn(passkeyApi, 'finishPasskeyLogin').mockResolvedValue({
      user: { id: 'admin_id', username: 'admin', role: 'admin' },
      expiresAt: '2026-08-13T00:00:00Z',
    });

    render(
      <QueryClientProvider client={queryClient}>
        <AppProvider>
          <MemoryRouter initialEntries={['/settings']}>
            <App />
          </MemoryRouter>
        </AppProvider>
      </QueryClientProvider>
    );

    // 1. Verify Passkey Login Surface
    await waitFor(() => {
      expect(screen.getByTestId('auth-surface')).toBeInTheDocument();
    });
    expect(screen.getByText('Mit Passkey anmelden')).toBeInTheDocument();

    // 2. Click Passkey login button
    const loginBtn = screen.getByTestId('passkey-login-button');
    await act(async () => {
      fireEvent.click(loginBtn);
    });

    expect(passkeyApi.startPasskeyLogin).toHaveBeenCalledTimes(1);
    expect(passkeyApi.finishPasskeyLogin).toHaveBeenCalledTimes(1);
  });

  it('renders SecuritySettingsSection and handles listing and deleting passkeys', async () => {
    vi.spyOn(passkeyApi, 'listPasskeys').mockResolvedValue([
      { id: 'pk_1', nickname: 'MacBook Touch ID', createdAt: '2026-08-12T20:00:00Z' },
    ]);
    vi.spyOn(passkeyApi, 'deletePasskey').mockResolvedValue(undefined);

    render(
      <QueryClientProvider client={queryClient}>
        <AppProvider>
          <SecuritySettingsSection />
        </AppProvider>
      </QueryClientProvider>
    );

    await waitFor(() => {
      expect(screen.getByText('MacBook Touch ID')).toBeInTheDocument();
    });

    const deleteBtn = screen.getByTestId('delete-passkey-pk_1');
    await act(async () => {
      fireEvent.click(deleteBtn);
    });

    const confirmBtn = screen.getByRole('button', { name: 'Ja' });
    await act(async () => {
      fireEvent.click(confirmBtn);
    });

    expect(passkeyApi.deletePasskey).toHaveBeenCalledWith('pk_1');
  });
});
