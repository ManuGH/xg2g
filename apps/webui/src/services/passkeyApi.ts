// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { getStoredToken } from '../utils/tokenStorage';

async function fetchJSON<T>(url: string, options: RequestInit = {}): Promise<T> {
  const token = getStoredToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };

  if (token && !headers['Authorization']) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(url, {
    ...options,
    headers,
    credentials: 'include', // Includes xg2g_session cookie
  });

  if (!res.ok) {
    let errorMessage = `HTTP ${res.status}`;
    try {
      const errBody = await res.json();
      errorMessage = errBody.detail || errBody.title || errBody.message || errorMessage;
    } catch {
      // Ignore JSON parse error
    }
    throw new Error(errorMessage);
  }

  return res.json() as Promise<T>;
}

export interface PasskeyItem {
  id: string;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
}

export interface PasskeyRegisterStartResult {
  options: any;
}

export interface PasskeyRegisterFinishResult {
  success: boolean;
  recoveryCodes?: string[];
  credentialId?: string;
}

export interface PasskeyLoginStartResult {
  options: any;
}

export interface PasskeyLoginFinishResult {
  success: boolean;
  sessionId?: string;
}

export interface BootstrapCommitResult {
  success: boolean;
}

export async function startPasskeyRegistration(username = 'admin'): Promise<PasskeyRegisterStartResult> {
  return fetchJSON('/api/v3/auth/passkey/register/start', {
    method: 'POST',
    body: JSON.stringify({ username }),
  });
}

export async function finishPasskeyRegistration(attestationResponse: any): Promise<PasskeyRegisterFinishResult> {
  return fetchJSON('/api/v3/auth/passkey/register/finish', {
    method: 'POST',
    body: JSON.stringify({ attestation: attestationResponse }),
  });
}

export async function startPasskeyLogin(): Promise<PasskeyLoginStartResult> {
  return fetchJSON('/api/v3/auth/passkey/login/start', {
    method: 'POST',
    body: JSON.stringify({}),
  });
}

export async function finishPasskeyLogin(assertionResponse: any): Promise<PasskeyLoginFinishResult> {
  return fetchJSON('/api/v3/auth/passkey/login/finish', {
    method: 'POST',
    body: JSON.stringify({ assertion: assertionResponse }),
  });
}

export async function commitBootstrap(): Promise<BootstrapCommitResult> {
  return fetchJSON('/api/v3/auth/bootstrap/commit', {
    method: 'POST',
  });
}

export async function loginWithRecoveryCode(code: string): Promise<PasskeyLoginFinishResult> {
  return fetchJSON('/api/v3/auth/recovery/login', {
    method: 'POST',
    body: JSON.stringify({ code }),
  });
}

export async function listPasskeys(): Promise<PasskeyItem[]> {
  const data = await fetchJSON<{ passkeys: PasskeyItem[] }>('/api/v3/auth/passkeys');
  return data.passkeys || [];
}

export async function deletePasskey(id: string): Promise<{ success: boolean }> {
  return fetchJSON(`/api/v3/auth/passkeys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export async function revokeOtherSessions(): Promise<{ success: boolean }> {
  return fetchJSON('/api/v3/auth/sessions/revoke-others', {
    method: 'DELETE',
  });
}
