// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { getStoredToken } from '../utils/tokenStorage';
import { ClientRequestError } from './appErrors';

export async function request<T>(
  endpoint: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: any;
  } = {}
): Promise<T> {
  const token = getStoredToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers || {}),
  };

  if (token && !headers['Authorization']) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const init: RequestInit = {
    method: options.method || 'GET',
    headers,
    credentials: 'same-origin',
  };

  if (options.body !== undefined) {
    init.body = typeof options.body === 'string' ? options.body : JSON.stringify(options.body);
  }

  const res = await fetch(endpoint, init);

  if (!res.ok) {
    let errBody: any = null;
    try {
      errBody = await res.json();
    } catch {
      // quiet catch
    }
    throw new ClientRequestError({
      status: res.status,
      code: errBody?.code || errBody?.title || 'REQUEST_FAILED',
      title: errBody?.detail || errBody?.title || errBody?.message || 'Request Failed',
      requestId: res.headers.get('X-Request-Id') || undefined,
    });
  }

  if (res.status === 204) {
    return undefined as unknown as T;
  }

  return (await res.json()) as T;
}
