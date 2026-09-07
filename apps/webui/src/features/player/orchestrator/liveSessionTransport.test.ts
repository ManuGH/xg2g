// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { describe, expect, it, vi } from 'vitest';
import { createDefaultLiveSessionTransport } from './liveSessionTransport';

describe('createDefaultLiveSessionTransport', () => {
  const apiBase = 'http://test.local/api/v3';
  const authHeaders = (hasBody?: boolean) => ({
    Authorization: 'Bearer test-token',
    ...(hasBody ? { 'X-CSRF-Token': 'csrf-123' } : {}),
  });

  it('fetchStreamInfo posts to /live/stream-info with auth and profile headers', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      headers: new Headers({ 'Content-Type': 'application/json' }),
      json: async () => ({
        mode: 'hlsjs',
        playbackDecisionToken: 'dt-123',
        sessionId: 'sess-preflight',
      }),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    const result = await transport.fetchStreamInfo({
      serviceRef: '1:0:1:1:0:0:0:0:0:0:',
      capabilities: { preferredHlsEngine: 'hlsjs' },
      profileHeaders: { 'X-Playback-Profile': 'lan' },
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('http://test.local/api/v3/live/stream-info');
    expect(init.method).toBe('POST');
    expect(init.headers['Authorization']).toBe('Bearer test-token');
    expect(init.headers['X-Playback-Profile']).toBe('lan');
    expect(JSON.parse(init.body)).toEqual({
      serviceRef: '1:0:1:1:0:0:0:0:0:0:',
      capabilities: { preferredHlsEngine: 'hlsjs' },
    });

    expect(result.status).toBe(200);
    expect(result.data).toEqual({
      mode: 'hlsjs',
      playbackDecisionToken: 'dt-123',
      sessionId: 'sess-preflight',
    });
  });

  it('postStartIntent posts formatted payload to /intents', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      headers: new Headers(),
      json: async () => ({ sessionId: 'sess-started-1' }),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    const body = {
      type: 'stream.start',
      serviceRef: 'srv_1',
      playbackDecisionToken: 'dt-123',
    };
    const result = await transport.postStartIntent({ body });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('http://test.local/api/v3/intents');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual(body);
    expect(result.data).toEqual({ sessionId: 'sess-started-1' });
  });

  it('waitForReady polls GET /sessions/:sessionId and resolves when READY with playbackUrl', async () => {
    let callCount = 0;
    const fetchMock = vi.fn().mockImplementation(async () => {
      callCount++;
      if (callCount === 1) {
        return {
          status: 200,
          ok: true,
          json: async () => ({ state: 'STARTING' }),
        };
      }
      return {
        status: 200,
        ok: true,
        json: async () => ({
          state: 'READY',
          playbackUrl: 'http://test.local/hls/index.m3u8',
          requestId: 'req-ready-1',
          heartbeatIntervalSeconds: 30,
          leaseExpiresAt: '2026-09-07T12:00:00Z',
        }),
      };
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    const result = await transport.waitForReady({
      sessionId: 'sess-test',
      budgetMs: 5_000,
    });

    expect(result).toEqual({
      sessionId: 'sess-test',
      playbackUrl: 'http://test.local/hls/index.m3u8',
      requestId: 'req-ready-1',
      mode: undefined,
      heartbeatIntervalSeconds: 30,
      leaseExpiresAt: '2026-09-07T12:00:00Z',
    });
    // Ensure the path is /sessions/sess-test, NOT /sessions/sess-test/status
    expect(fetchMock.mock.calls[0]![0]).toBe('http://test.local/api/v3/sessions/sess-test');
  });

  it('waitForReady rejects on terminal failure states (FAILED / STOPPED / CANCELLED)', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      json: async () => ({
        state: 'FAILED',
        reason: 'R_FFMPEG_START_FAILED',
      }),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    await expect(
      transport.waitForReady({ sessionId: 'sess-fail', budgetMs: 2_000 }),
    ).rejects.toThrow(/reached terminal state FAILED \(R_FFMPEG_START_FAILED\)/);
  });

  it('waitForReady rejects on 401 / 403 authentication failures', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 401,
      ok: false,
      json: async () => ({}),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    await expect(
      transport.waitForReady({ sessionId: 'sess-auth', budgetMs: 2_000 }),
    ).rejects.toThrow(/Session authorization failed \(HTTP 401\)/);
  });

  it('waitForReady rejects on 410 expired session', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 410,
      ok: false,
      json: async () => ({ reason: 'R_LEASE_EXPIRED' }),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    await expect(
      transport.waitForReady({ sessionId: 'sess-exp', budgetMs: 2_000 }),
    ).rejects.toThrow(/Session sess-exp expired or gone: R_LEASE_EXPIRED/);
  });

  it('postStopIntent sends stream.stop intent and throws on non-2xx response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      json: async () => ({ title: 'Internal Error' }),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    await expect(
      transport.postStopIntent({ sessionId: 'sess-stop' }),
    ).rejects.toThrow(/Stop intent failed with HTTP 500/);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe('http://test.local/api/v3/intents');
    expect(JSON.parse(init.body)).toEqual({
      type: 'stream.stop',
      sessionId: 'sess-stop',
    });
  });

  it('postStopIntent succeeds on 200 response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      json: async () => ({}),
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    await expect(
      transport.postStopIntent({ sessionId: 'sess-stop' }),
    ).resolves.toBeUndefined();
  });

  it('bounds readiness response-body reading and preserves cancellation', async () => {
    vi.useFakeTimers();
    try {
      const bodyPromise = new Promise(() => {});
      const abort = new AbortController();
      const fetchMock = vi.fn().mockResolvedValue({
        status: 200,
        ok: true,
        json: () => bodyPromise,
      });
      const transport = createDefaultLiveSessionTransport({
        apiBase,
        authHeaders,
        fetchFn: fetchMock as unknown as typeof fetch,
      });
      let settled = false;
      void transport
        .waitForReady({ sessionId: 'S', budgetMs: 1_000, signal: abort.signal })
        .then(
          () => {
            settled = true;
          },
          () => {
            settled = true;
          },
        );
      await vi.advanceTimersByTimeAsync(0);
      abort.abort();
      await vi.advanceTimersByTimeAsync(6_000);
      expect(fetchMock.mock.calls[0]![1].signal.aborted).toBe(true);
      expect(settled).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('postStartIntent rejects with AbortError when signal aborts while reading response body', async () => {
    const abort = new AbortController();
    const bodyPromise = new Promise<{ sessionId: string }>(() => {});
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      headers: new Headers(),
      json: () => bodyPromise,
    });

    const transport = createDefaultLiveSessionTransport({
      apiBase,
      authHeaders,
      fetchFn: fetchMock as unknown as typeof fetch,
    });

    const postPromise = transport.postStartIntent({
      body: { type: 'stream.start' },
      signal: abort.signal,
    });

    abort.abort();

    await expect(postPromise).rejects.toThrow();
    await expect(postPromise).rejects.toHaveProperty('name', 'AbortError');
  });
});
