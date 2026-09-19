// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

export interface StreamInfoResult {
  status: number;
  data: unknown;
  headers: Headers;
}

export interface StartIntentResult {
  status: number;
  data: unknown;
  headers: Headers;
}

export interface SessionReadyResult {
  sessionId: string;
  playbackUrl?: string;
  requestId?: string;
  mode?: string;
  heartbeatIntervalSeconds?: number;
  leaseExpiresAt?: string;
  [key: string]: unknown;
}

export interface PostHeartbeatParams {
  sessionId: string;
  signal?: AbortSignal;
}

export interface PostHeartbeatResult {
  status: number;
  data: unknown;
  headers: Headers;
}

export interface FetchSessionSnapshotParams {
  sessionId: string;
  signal?: AbortSignal;
}

export interface FetchSessionSnapshotResult {
  status: number;
  data: unknown;
  headers: Headers;
}

export interface LiveSessionTransport {
  readonly apiBase?: string;
  fetchStreamInfo(params: {
    serviceRef: string;
    capabilities?: unknown;
    profileHeaders?: Record<string, string>;
    signal?: AbortSignal;
  }): Promise<StreamInfoResult>;

  postStartIntent(params: {
    body: unknown;
    signal?: AbortSignal;
  }): Promise<StartIntentResult>;

  waitForReady(params: {
    sessionId: string;
    signal?: AbortSignal;
    budgetMs?: number;
  }): Promise<SessionReadyResult>;

  postStopIntent(params: {
    sessionId: string;
    signal?: AbortSignal;
  }): Promise<void>;

  postHeartbeat?(params: PostHeartbeatParams): Promise<PostHeartbeatResult>;

  fetchSessionSnapshot?(params: FetchSessionSnapshotParams): Promise<FetchSessionSnapshotResult>;
}

export class PlaybackHttpError extends Error {
  readonly status: number;
  readonly data: unknown;
  readonly headers?: Headers;
  readonly requestId?: string;
  readonly traceId?: string;
  readonly sessionId?: string;

  constructor(message: string, status: number, data: unknown, headers?: Headers | null, sessionId?: string) {
    super(message);
    this.name = 'PlaybackHttpError';
    this.status = status;
    this.data = data;
    this.headers = (headers as Headers | undefined) ?? undefined;
    this.sessionId =
      sessionId ||
      (typeof data === 'object' && data !== null && typeof (data as { sessionId?: unknown }).sessionId === 'string'
        ? (data as { sessionId: string }).sessionId
        : undefined);
    const reqId =
      (typeof data === 'object' && data !== null && typeof (data as { requestId?: unknown }).requestId === 'string'
        ? (data as { requestId: string }).requestId
        : undefined) ||
      headers?.get?.('X-Request-ID') ||
      headers?.get?.('x-request-id') ||
      undefined;
    this.requestId = reqId;
    this.traceId =
      (typeof data === 'object' && data !== null && typeof (data as { traceId?: unknown }).traceId === 'string'
        ? (data as { traceId: string }).traceId
        : undefined) ||
      headers?.get?.('X-Trace-ID') ||
      headers?.get?.('x-trace-id') ||
      reqId;
  }
}

export interface DefaultLiveSessionTransportOptions {
  apiBase: string;
  authHeaders: (hasBody?: boolean) => Record<string, string>;
  fetchFn?: typeof fetch;
  recoverSessionCookie?: (source: string) => Promise<boolean>;
}

function raceWithSignal<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  if (signal.aborted) {
    return Promise.reject(new DOMException('Aborted', 'AbortError'));
  }
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(new DOMException('Aborted', 'AbortError'));
    signal.addEventListener('abort', onAbort, { once: true });
    promise.then(
      (val) => {
        signal.removeEventListener('abort', onAbort);
        resolve(val);
      },
      (err) => {
        signal.removeEventListener('abort', onAbort);
        reject(err);
      },
    );
  });
}

async function parseResponseBody(res: Response, signal?: AbortSignal): Promise<unknown> {
  const maybe = res as unknown as {
    json?: () => Promise<unknown>;
    text?: () => Promise<string>;
  };
  if (typeof maybe.json === 'function') {
    try {
      const p = maybe.json();
      return signal ? await raceWithSignal(p, signal) : await p;
    } catch (err) {
      if (
        signal?.aborted ||
        (err instanceof DOMException && err.name === 'AbortError') ||
        (typeof err === 'object' && err !== null && (err as { name?: unknown }).name === 'AbortError')
      ) {
        throw err;
      }
      // json() failed or was not a json endpoint, try text fallback
    }
  }
  if (typeof maybe.text === 'function') {
    try {
      const p = maybe.text();
      const rawText = signal ? await raceWithSignal(p, signal) : await p;
      if (!rawText) return null;
      return JSON.parse(rawText);
    } catch (err) {
      if (
        signal?.aborted ||
        (err instanceof DOMException && err.name === 'AbortError') ||
        (typeof err === 'object' && err !== null && (err as { name?: unknown }).name === 'AbortError')
      ) {
        throw err;
      }
      // text parse failed
    }
  }
  return null;
}

export function createDefaultLiveSessionTransport({
  apiBase,
  authHeaders,
  fetchFn = globalThis.fetch,
  recoverSessionCookie,
}: DefaultLiveSessionTransportOptions): LiveSessionTransport {
  return {
    apiBase,
    async fetchStreamInfo({ serviceRef, capabilities, profileHeaders, signal }) {
      const headers = {
        ...authHeaders(true),
        'Content-Type': 'application/json',
        ...(profileHeaders ?? {}),
      };
      const res = await fetchFn(`${apiBase}/live/stream-info`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ serviceRef, capabilities }),
        signal,
      });
      const data = await parseResponseBody(res, signal);
      return {
        status: res.status,
        data,
        headers: res.headers,
      };
    },

    async postStartIntent({ body, signal }) {
      const headers = {
        ...authHeaders(true),
        'Content-Type': 'application/json',
      };
      const res = await fetchFn(`${apiBase}/intents`, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal,
      });
      const data = await parseResponseBody(res, signal);
      return {
        status: res.status,
        data,
        headers: res.headers,
      };
    },

    async waitForReady({ sessionId, signal, budgetMs = 60_000 }) {
      const deadline = Date.now() + budgetMs;
      const pollIntervalMs = 500;
      const requestTimeoutMs = 5_000;
      let recoveredSessionAuth = false;

      while (Date.now() < deadline) {
        if (signal?.aborted) {
          throw new DOMException('Aborted', 'AbortError');
        }

        const pollController = new AbortController();
        const remainingBudget = Math.max(0, deadline - Date.now());
        const pollTimeoutMs = Math.min(requestTimeoutMs, remainingBudget);
        const pollTimer = setTimeout(() => pollController.abort(), pollTimeoutMs);

        const onAbort = () => pollController.abort();
        signal?.addEventListener('abort', onAbort);

        try {
          const res = await fetchFn(`${apiBase}/sessions/${sessionId}`, {
            headers: authHeaders(false),
            signal: pollController.signal,
          });

          if (res.status === 401) {
            if (recoverSessionCookie && !recoveredSessionAuth) {
              const recovered = await recoverSessionCookie('liveSessionTransport.waitForReady');
              if (recovered) {
                recoveredSessionAuth = true;
                continue;
              }
            }
            const data = await parseResponseBody(res, pollController.signal);
            throw new PlaybackHttpError(`Session authorization failed (HTTP ${res.status})`, 401, data, res.headers);
          }

          if (res.status === 403) {
            const data = await parseResponseBody(res, pollController.signal);
            throw new PlaybackHttpError(`Session authorization failed (HTTP ${res.status})`, 403, data, res.headers);
          }

          if (res.status === 410) {
            let reason = 'expired';
            let problem: unknown = null;
            try {
              problem = (await parseResponseBody(res, pollController.signal)) as any;
              reason = (problem as any)?.reason || (problem as any)?.state || reason;
            } catch {
              // body parse fallback
            }
            const dataWithAuth = (problem && typeof problem === 'object')
              ? { ...problem, recoveredSessionAuth }
              : { recoveredSessionAuth };
            throw new PlaybackHttpError(`Session ${sessionId} expired or gone: ${reason}`, 410, dataWithAuth, res.headers);
          }

          if (res.ok) {
            let data: any;
            try {
              data = await parseResponseBody(res, pollController.signal);
            } catch (parseErr) {
              if (signal?.aborted || pollController.signal.aborted) {
                throw parseErr;
              }
              await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
              continue;
            }

            const state = data?.state;
            if (
              state === 'FAILED' ||
              state === 'STOPPED' ||
              state === 'CANCELLED' ||
              state === 'STOPPING'
            ) {
              const reason = data?.reason ? ` (${data.reason})` : '';
              throw new PlaybackHttpError(
                `Session ${sessionId} reached terminal state ${state}${reason}`,
                410,
                data,
                res.headers,
                sessionId,
              );
            }

            if ((state === 'READY' || state === 'DRAINING') && (data?.playbackUrl || data?.streamUrl)) {
              return {
                sessionId,
                playbackUrl: data.playbackUrl ?? data.streamUrl,
                requestId: data.requestId,
                mode: data.mode,
                heartbeatIntervalSeconds: data.heartbeatIntervalSeconds,
                leaseExpiresAt: data.leaseExpiresAt,
                ...(data.trace ? { trace: data.trace } : {}),
              };
            }
          }

          await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
        } catch (err) {
          if (signal?.aborted) {
            throw new DOMException('Aborted', 'AbortError');
          }
          if (err instanceof PlaybackHttpError) {
            throw err;
          }
          if (err instanceof Error && (
            err.message.includes('Session authorization failed') ||
            err.message.includes('expired or gone') ||
            err.message.includes('reached terminal state')
          )) {
            throw err;
          }
          if (Date.now() >= deadline) {
            break;
          }
          await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
        } finally {
          clearTimeout(pollTimer);
          signal?.removeEventListener('abort', onAbort);
        }
      }
      throw new Error(`Timeout waiting for session readiness: ${sessionId}`);
    },

    async postStopIntent({ sessionId, signal }) {
      const headers = {
        ...authHeaders(true),
        'Content-Type': 'application/json',
      };
      const res = await fetchFn(`${apiBase}/intents`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          type: 'stream.stop',
          sessionId,
        }),
        signal,
      });
      if (!res.ok) {
        throw new Error(`Stop intent failed with HTTP ${res.status}`);
      }
    },

    async postHeartbeat({ sessionId, signal }) {
      let recoveredSessionAuth = false;
      while (true) {
        if (signal?.aborted) {
          throw new DOMException('Aborted', 'AbortError');
        }

        const headers = {
          ...authHeaders(true),
          'Content-Type': 'application/json',
        };

        const res = await fetchFn(`${apiBase}/sessions/${sessionId}/heartbeat`, {
          method: 'POST',
          headers,
          signal,
        });

        if (res.status === 401 && recoverSessionCookie && !recoveredSessionAuth) {
          const recovered = await recoverSessionCookie('liveSessionTransport.heartbeat');
          if (recovered) {
            recoveredSessionAuth = true;
            continue;
          }
        }

        const data = await parseResponseBody(res, signal);
        return {
          status: res.status,
          data,
          headers: res.headers,
        };
      }
    },

    async fetchSessionSnapshot({ sessionId, signal }) {
      let recoveredSessionAuth = false;
      while (true) {
        if (signal?.aborted) {
          throw new DOMException('Aborted', 'AbortError');
        }

        const headers = authHeaders(false);

        const res = await fetchFn(`${apiBase}/sessions/${sessionId}`, {
          headers,
          signal,
        });

        if (res.status === 401 && recoverSessionCookie && !recoveredSessionAuth) {
          const recovered = await recoverSessionCookie('liveSessionTransport.fetchSessionSnapshot');
          if (recovered) {
            recoveredSessionAuth = true;
            continue;
          }
        }

        const data = await parseResponseBody(res, signal);
        return {
          status: res.status,
          data,
          headers: res.headers,
        };
      }
    },
  };
}
