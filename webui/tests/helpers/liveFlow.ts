import { vi } from 'vitest';

type FetchMock = ReturnType<typeof vi.fn>;

type LiveFlowOptions = {
  mode?: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny';
  requestId?: string;
  playbackDecisionToken?: string;
  decisionReasons?: string[];
  sessionId?: string;
  playbackUrl?: string;
  heartbeatInterval?: number;
};

function mockJsonResponse(url: string, status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    url,
    headers: {
      get: (name: string) => (name.toLowerCase() === 'content-type' ? 'application/json' : null),
    },
    json: async () => body,
    text: async () => JSON.stringify(body),
  };
}

export function mockLiveFlowFetch(options: LiveFlowOptions = {}): FetchMock {
  const mode = options.mode ?? 'hlsjs';
  const requestId = options.requestId ?? 'req-live-flow-1';
  const playbackDecisionToken = options.playbackDecisionToken ?? 'token-live-flow-1';
  const decisionReasons = options.decisionReasons ?? ['direct_stream_match'];
  const sessionId = options.sessionId ?? 'sess-live-flow-1';
  const playbackUrl = options.playbackUrl ?? '/live.m3u8';
  const heartbeatInterval = options.heartbeatInterval ?? 1;

  const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
    const url = String(input);

    if (url.includes('/live/stream-info')) {
      return Promise.resolve(
        mockJsonResponse(url, 200, {
          mode,
          requestId,
          playbackDecisionToken,
          decision: { reasons: decisionReasons },
        }),
      );
    }

    if (url.includes('/intents')) {
      return Promise.resolve(
        mockJsonResponse(url, 200, {
          sessionId,
          requestId: `${requestId}-intent`,
        }),
      );
    }

    if (url.includes(`/sessions/${sessionId}`) && !url.includes('/heartbeat') && !url.includes('/feedback')) {
      return Promise.resolve(
        mockJsonResponse(url, 200, {
          state: 'READY',
          playbackUrl,
          heartbeat_interval: heartbeatInterval,
        }),
      );
    }

    if (url.includes('/heartbeat')) {
      return Promise.resolve(
        mockJsonResponse(url, 200, {
          lease_expires_at: 'next',
        }),
      );
    }

    return Promise.resolve(mockJsonResponse(url, 200, {}));
  });

  vi.stubGlobal('fetch', fetchMock as unknown as typeof globalThis.fetch);
  return fetchMock;
}

export function findFetchCall(fetchMock: FetchMock, pathFragment: string): [unknown, RequestInit?] | undefined {
  return fetchMock.mock.calls.find((call: unknown[]) => String(call[0]).includes(pathFragment)) as
    | [unknown, RequestInit?]
    | undefined;
}
