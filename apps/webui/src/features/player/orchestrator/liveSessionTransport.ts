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

export interface LiveSessionTransport {
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
}

export interface DefaultLiveSessionTransportOptions {
  apiBase: string;
  authHeaders: (hasBody?: boolean) => Record<string, string>;
  fetchFn?: typeof fetch;
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

export function createDefaultLiveSessionTransport({
  apiBase,
  authHeaders,
  fetchFn = globalThis.fetch,
}: DefaultLiveSessionTransportOptions): LiveSessionTransport {
  return {
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
      let data: unknown = null;
      try {
        data = await res.json();
      } catch {
        // Body parse failed
      }
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
      let data: unknown = null;
      try {
        data = await res.json();
      } catch {
        // Body parse failed
      }
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

          if (res.status === 401 || res.status === 403) {
            throw new Error(`Session authorization failed (HTTP ${res.status})`);
          }

          if (res.status === 410) {
            let reason = 'expired';
            try {
              const problem = await raceWithSignal(res.json(), pollController.signal);
              reason = problem?.reason || problem?.state || reason;
            } catch {
              // body parse fallback
            }
            throw new Error(`Session ${sessionId} expired or gone: ${reason}`);
          }

          if (res.ok) {
            let data: any;
            try {
              data = await raceWithSignal(res.json(), pollController.signal);
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
              throw new Error(`Session ${sessionId} reached terminal state ${state}${reason}`);
            }

            if ((state === 'READY' || state === 'DRAINING') && (data?.playbackUrl || data?.streamUrl)) {
              return {
                sessionId,
                playbackUrl: data.playbackUrl ?? data.streamUrl,
                requestId: data.requestId,
                mode: data.mode,
                heartbeatIntervalSeconds: data.heartbeatIntervalSeconds,
                leaseExpiresAt: data.leaseExpiresAt,
              };
            }
          }

          await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
        } catch (err) {
          if (signal?.aborted) {
            throw new DOMException('Aborted', 'AbortError');
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
  };
}
