import { buildClientHeaders, getApiBaseUrl, putJsonOrThrow } from '../../services/clientWrapper';

export interface ResumeState {
  posSeconds: number;
  durationSeconds?: number;
  finished?: boolean;
}

export interface SaveResumeRequest {
  position: number;
  total?: number;
  finished?: boolean;
  /** Display-metadata snapshots so continue-watching renders without a receiver round-trip. */
  title?: string;
  channel?: string;
}

export interface SaveResumeOptions {
  /**
   * Send the save as a fire-and-forget keepalive request. Required on
   * pagehide/visibilitychange paths where the browser tears the page down
   * before a regular async fetch completes.
   */
  keepalive?: boolean;
}

export const saveResume = async (
  recordingId: string,
  data: SaveResumeRequest,
  options: SaveResumeOptions = {},
): Promise<void> => {
  const url = `/recordings/${recordingId}/resume`;

  if (options.keepalive && typeof fetch === 'function') {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    for (const [key, value] of Object.entries(buildClientHeaders())) {
      if (typeof value === 'string') headers[key] = value;
    }
    await fetch(`${getApiBaseUrl()}${url}`, {
      method: 'PUT',
      keepalive: true,
      headers,
      body: JSON.stringify(data),
    });
    return;
  }

  await putJsonOrThrow(url, data);
};

export interface ContinueWatchingItem {
  recordingId: string;
  title?: string;
  channel?: string;
  posSeconds: number;
  durationSeconds?: number;
  updatedAt?: string;
}

interface ContinueWatchingResponse {
  items?: ContinueWatchingItem[];
}

export const fetchContinueWatching = async (limit = 12): Promise<ContinueWatchingItem[]> => {
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries(buildClientHeaders())) {
    if (typeof value === 'string') headers[key] = value;
  }
  const response = await fetch(`${getApiBaseUrl()}/recordings/continue?limit=${limit}`, { headers });
  if (!response.ok) {
    throw new Error(`continue-watching request failed (${response.status})`);
  }
  const payload = (await response.json()) as ContinueWatchingResponse;
  return payload.items ?? [];
};
