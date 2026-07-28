import { afterEach, describe, expect, it, vi } from 'vitest';
import { client } from '../client-ts/client.gen';
import { mergeHeaders } from '../client-ts/client';
import { subscribeAuthRequired } from '../features/player/sessionEvents';
import {
  buildClientHeaders,
  CLIENT_AUTH_CHANGED_EVENT,
  ClientRequestError,
  getClientAuthToken,
  getClientHouseholdProfileId,
  isApiError,
  isProblemDetails,
  mapApiError,
  putJsonOrThrow,
  setClientAuthToken,
  setClientHouseholdProfileId,
  throwOnClientResultError,
  unwrapClientResultOrThrow
} from './clientWrapper';

describe('client-ts wrapper error mapping', () => {
  afterEach(() => {
    setClientAuthToken('');
    setClientHouseholdProfileId('');
    vi.restoreAllMocks();
  });

  it('updates auth and household headers independently', () => {
    const authChanged = vi.fn();
    window.addEventListener(CLIENT_AUTH_CHANGED_EVENT, authChanged);

    try {
      setClientHouseholdProfileId('child-profile');
      setClientAuthToken('test-token');

      expect(getClientHouseholdProfileId()).toBe('child-profile');
      expect(getClientAuthToken()).toBe('test-token');
      expect(authChanged).toHaveBeenCalledTimes(1);
      const headers = new Headers(client.getConfig().headers as HeadersInit);
      expect(headers.get('Authorization')).toBe('Bearer test-token');
      expect(headers.get('X-Household-Profile')).toBe('child-profile');
      expect(client.getConfig().auth).toBeTypeOf('function');
    } finally {
      window.removeEventListener(CLIENT_AUTH_CHANGED_EVENT, authChanged);
    }
  });

  it('strips the household profile header through the SDK merge while keeping bearer auth', () => {
    setClientAuthToken('test-token');
    setClientHouseholdProfileId('child-profile');

    const requestHeaders = buildClientHeaders({
      'X-Household-Profile': null,
    });

    // The override must be carried as an explicit null delete-marker (a Headers
    // instance could only omit the key, which the merge would then re-fill).
    expect(requestHeaders['Authorization']).toBe('Bearer test-token');
    expect(requestHeaders['X-Household-Profile']).toBeNull();

    // End-to-end: hey-api applies request headers via mergeHeaders(config, request).
    // The config-level X-Household-Profile must actually be removed, while the bearer
    // token survives. Before the fix the profile header leaked through and bricked
    // cross-tab profile recovery (every /api/v3 request 400s).
    const merged = mergeHeaders(client.getConfig().headers, requestHeaders);
    expect(merged.get('Authorization')).toBe('Bearer test-token');
    expect(merged.get('X-Household-Profile')).toBeNull();
  });

  it('keeps a bearer auth fallback for secured requests when the header is absent', async () => {
    setClientAuthToken('test-token');

    const authConfig = client.getConfig().auth;
    expect(authConfig).toBeTypeOf('function');
    expect(typeof authConfig).toBe('function');
    if (typeof authConfig !== 'function') {
      throw new Error('expected auth fallback function');
    }

    const resolvedToken = await authConfig({
      scheme: 'bearer',
      type: 'http',
    });
    expect(resolvedToken).toBe('test-token');

    client.setConfig({
      headers: {
        Authorization: null,
      },
    });

    const headerlessConfig = client.getConfig();
    const headers = new Headers(headerlessConfig.headers as HeadersInit);
    expect(headers.get('Authorization')).toBeNull();
    expect(typeof headerlessConfig.auth).toBe('function');
    if (typeof headerlessConfig.auth !== 'function') {
      throw new Error('expected auth fallback function after header removal');
    }

    const fallbackToken = await headerlessConfig.auth({
      scheme: 'bearer',
      type: 'http',
    });
    expect(fallbackToken).toBe('test-token');
  });

  it('maps RFC7807 problem details with typed fields', () => {
    const problem = {
      type: 'about:blank',
      title: 'Lease busy',
      status: 409,
      requestId: 'req-409',
      code: 'LEASE_BUSY',
      detail: 'Stream lease already in use'
    };

    expect(isProblemDetails(problem)).toBe(true);
    expect(mapApiError(problem)).toEqual({
      status: 409,
      code: 'LEASE_BUSY',
      type: 'about:blank',
      title: 'Lease busy',
      detail: 'Stream lease already in use',
      requestId: 'req-409'
    });
  });

  it('preserves stable live truth problem fields from RFC7807 responses', () => {
    const problem = {
      type: '/problems/live/partial_truth',
      title: 'Live media truth incomplete',
      status: 503,
      requestId: 'req-live',
      code: 'UNAVAILABLE',
      detail: 'Live media truth is incomplete',
      retryAfterSeconds: 5,
      truthState: 'partial',
      truthReason: 'partial_scan_truth',
      truthOrigin: 'live_unverified',
      problemFlags: ['live_truth_unverified', 'partial_scan_truth']
    };

    expect(mapApiError(problem)).toEqual({
      status: 503,
      code: 'UNAVAILABLE',
      type: '/problems/live/partial_truth',
      title: 'Live media truth incomplete',
      detail: 'Live media truth is incomplete',
      requestId: 'req-live',
      retryAfterSeconds: 5,
      truthState: 'partial',
      truthReason: 'partial_scan_truth',
      truthOrigin: 'live_unverified',
      problemFlags: ['live_truth_unverified', 'partial_scan_truth']
    });
  });

  it('maps API error payload and keeps fallback status', () => {
    const apiError = {
      code: 'AUTH_REQUIRED',
      message: 'Authentication required',
      requestId: 'req-auth',
      details: 'Missing bearer token'
    };

    expect(isApiError(apiError)).toBe(true);
    expect(mapApiError(apiError, 401)).toEqual({
      status: 401,
      code: 'AUTH_REQUIRED',
      title: 'Authentication required',
      detail: 'Missing bearer token',
      requestId: 'req-auth'
    });
  });

  it('falls back for generic thrown errors', () => {
    expect(mapApiError(new Error('network down'), 503)).toEqual({
      status: 503,
      title: 'network down'
    });
  });

  it('throws ClientRequestError on failed JSON PUT', async () => {
    vi.spyOn(client, 'put').mockResolvedValue({
      data: undefined,
      error: {
        type: 'about:blank',
        title: 'Preparing',
        status: 503,
        requestId: 'req-503',
        code: 'UNAVAILABLE',
        detail: 'warming up'
      },
      request: new Request('http://localhost/api/v3/recordings/abc/resume', { method: 'PUT' }),
      response: new Response('{}', {
        status: 503,
        headers: { 'Content-Type': 'application/problem+json' }
      })
    } as any);

    await expect(
      putJsonOrThrow('/recordings/abc/resume', { position: 10 })
    ).rejects.toEqual(
      expect.objectContaining({
        name: 'ClientRequestError',
        status: 503,
        code: 'UNAVAILABLE',
        requestId: 'req-503',
        message: 'warming up'
      })
    );
  });

  it('dispatches auth-required details for 401 client results', () => {
    const authRequired = vi.fn();
    const unsubscribe = subscribeAuthRequired(authRequired);

    try {
      expect(() => {
        throwOnClientResultError({
          error: {
            code: 'AUTH_REQUIRED',
            message: 'Authentication required',
            requestId: 'req-auth'
          },
          response: { status: 401 }
        }, { source: 'useSystemHealth' });
      }).toThrowError(ClientRequestError);

      expect(authRequired).toHaveBeenCalledWith({
        source: 'useSystemHealth',
        status: 401,
        code: 'AUTH_REQUIRED'
      });
    } finally {
      unsubscribe();
    }
  });

  it('supports silent unwrap while still signaling session expiry on 401', () => {
    const authRequired = vi.fn();
    const unsubscribe = subscribeAuthRequired(authRequired);

    try {
      expect(
        unwrapClientResultOrThrow({
          error: {
            code: 'AUTH_REQUIRED',
            message: 'Authentication required',
            requestId: 'req-auth-silent'
          },
          response: { status: 401 }
        }, {
          source: 'useSystemConfig',
          silent: true
        })
      ).toBeUndefined();

      expect(authRequired).toHaveBeenCalledWith({
        source: 'useSystemConfig',
        status: 401,
        code: 'AUTH_REQUIRED'
      });
    } finally {
      unsubscribe();
    }
  });

  it('returns without throwing when JSON PUT succeeds', async () => {
    vi.spyOn(client, 'put').mockResolvedValue({
      data: {},
      error: undefined,
      request: new Request('http://localhost/api/v3/recordings/abc/resume', { method: 'PUT' }),
      response: new Response('{}', { status: 200 })
    } as any);

    await expect(
      putJsonOrThrow('/recordings/abc/resume', { position: 10 })
    ).resolves.toBeUndefined();
  });
});
