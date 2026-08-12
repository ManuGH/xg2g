// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { request } from '../lib/api';

export interface PasskeyCredentialSummary {
  id: string;
  nickname: string;
  createdAt: string;
  backupEligible?: boolean;
  backupState?: boolean;
}

export interface FinishRegistrationResponse {
  status: 'bootstrap_completed' | 'registered';
  user?: {
    id: string;
    username: string;
    role: string;
  };
  credential?: PasskeyCredentialSummary;
  id?: string;
  nickname?: string;
  recoveryCodes?: string[];
  expiresAt?: string;
}

export interface AuthSessionResponse {
  user: {
    id: string;
    username: string;
    role: string;
  };
  expiresAt: string;
}

/**
 * Start Passkey registration ceremony.
 * For bootstrap mode (0 users), setupToken can be passed in X-Setup-Token header.
 */
export async function startPasskeyRegistration(username?: string, setupToken?: string): Promise<{ options: any }> {
  const headers: Record<string, string> = {};
  if (setupToken) {
    headers['X-Setup-Token'] = setupToken;
  }

  const data = await request<any>('/api/v3/auth/passkey/register/start', {
    method: 'POST',
    headers,
    body: username ? { username } : {},
  });

  return { options: data };
}

/**
 * Finish Passkey registration ceremony.
 */
export async function finishPasskeyRegistration(
  attestation: any,
  nickname = 'Passkey'
): Promise<FinishRegistrationResponse> {
  const data = await request<FinishRegistrationResponse>('/api/v3/auth/passkey/register/finish', {
    method: 'POST',
    body: {
      response: attestation.response,
      nickname,
    },
  });

  return data;
}

/**
 * Start Passkey login ceremony.
 */
export async function startPasskeyLogin(): Promise<{ options: any }> {
  const data = await request<any>('/api/v3/auth/passkey/login/start', {
    method: 'POST',
    body: {},
  });

  return { options: data };
}

/**
 * Finish Passkey login ceremony.
 * Backend returns AuthSessionResponse { user, expiresAt } and sets xg2g_session cookie.
 */
export async function finishPasskeyLogin(assertion: any): Promise<AuthSessionResponse> {
  const responsePayload = {
    id: assertion.id || assertion.response?.id,
    clientDataJSON: assertion.response?.clientDataJSON,
    authenticatorData: assertion.response?.authenticatorData,
    signature: assertion.response?.signature,
    userHandle: assertion.response?.userHandle,
  };

  const data = await request<AuthSessionResponse>('/api/v3/auth/passkey/login/finish', {
    method: 'POST',
    body: {
      response: responsePayload,
    },
  });

  return data;
}

/**
 * Acknowledge recovery codes after bootstrap passkey setup.
 * Backend route: POST /api/v3/auth/bootstrap/acknowledge-recovery
 */
export async function acknowledgeRecovery(): Promise<void> {
  await request<void>('/api/v3/auth/bootstrap/acknowledge-recovery', {
    method: 'POST',
    body: {},
  });
}

/**
 * Login using a 10-character recovery code.
 * Backend route: POST /api/v3/auth/recovery (requires username and code)
 */
export async function loginWithRecoveryCode(username: string, code: string): Promise<AuthSessionResponse> {
  const data = await request<AuthSessionResponse>('/api/v3/auth/recovery', {
    method: 'POST',
    body: {
      username,
      code,
    },
  });

  return data;
}

/**
 * List registered passkeys for the current user.
 * Backend route: GET /api/v3/auth/passkeys
 * Returns PasskeyCredentialSummary[] directly as a JSON array.
 */
export async function listPasskeys(): Promise<PasskeyCredentialSummary[]> {
  const data = await request<PasskeyCredentialSummary[] | { passkeys: PasskeyCredentialSummary[] }>('/api/v3/auth/passkeys', {
    method: 'GET',
  });

  if (Array.isArray(data)) {
    return data;
  }
  if (data && Array.isArray((data as any).passkeys)) {
    return (data as any).passkeys;
  }
  return [];
}

/**
 * Delete a passkey credential by ID.
 * Backend route: DELETE /api/v3/auth/passkeys/{id}
 */
export async function deletePasskey(id: string): Promise<void> {
  await request<void>(`/api/v3/auth/passkeys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

/**
 * Revoke all other active web sessions.
 * Backend route: POST /api/v3/auth/sessions/revoke-others
 */
export async function revokeOtherSessions(): Promise<void> {
  await request<void>('/api/v3/auth/sessions/revoke-others', {
    method: 'POST',
    body: {},
  });
}
