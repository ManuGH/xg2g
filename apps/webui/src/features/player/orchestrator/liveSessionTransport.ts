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
      while (Date.now() < deadline) {
        if (signal?.aborted) {
          throw new DOMException('Aborted', 'AbortError');
        }
        const res = await fetchFn(`${apiBase}/sessions/${sessionId}/status`, {
          headers: authHeaders(false),
          signal,
        });
        if (res.ok) {
          const data: any = await res.json();
          if (data?.phase === 'ready' || data?.status === 'ready' || data?.playbackUrl) {
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
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
      throw new Error(`Timeout waiting for session readiness: ${sessionId}`);
    },

    async postStopIntent({ sessionId, signal }) {
      const headers = {
        ...authHeaders(true),
        'Content-Type': 'application/json',
      };
      await fetchFn(`${apiBase}/intents`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          type: 'stream.stop',
          sessionId,
        }),
        signal,
      });
    },
  };
}
