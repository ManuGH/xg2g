import { afterEach, describe, expect, it, vi } from 'vitest';
import { client } from './client.gen';
import { isApiError, isProblemDetails, mapApiError, putJsonOrThrow } from './wrapper';

describe('client-ts wrapper error mapping', () => {
  afterEach(() => {
    vi.restoreAllMocks();
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
      title: 'Lease busy',
      detail: 'Stream lease already in use',
      requestId: 'req-409'
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
