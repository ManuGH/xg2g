import { client } from '../client-ts/client.gen';
import type { ApiError, ProblemDetails } from '../client-ts/types.gen';
import { isUnauthorizedStatus, requestAuthRequired } from '../features/player/sessionEvents';

export const HOUSEHOLD_PROFILE_HEADER = 'X-Household-Profile';
export const CLIENT_AUTH_CHANGED_EVENT = 'xg2g:client-auth-changed';

export type MappedApiError = {
  status?: number;
  code?: string;
  title?: string;
  detail?: string;
  requestId?: string;
};

export class ClientRequestError extends Error {
  readonly status?: number;
  readonly code?: string;
  readonly requestId?: string;
  readonly detail?: string;
  readonly title: string;

  constructor(mapped: MappedApiError) {
    super(mapped.detail ?? mapped.title ?? 'Request failed');
    this.name = 'ClientRequestError';
    this.status = mapped.status;
    this.code = mapped.code;
    this.requestId = mapped.requestId;
    this.detail = mapped.detail;
    this.title = mapped.title ?? 'Request failed';
  }
}

export interface ClientResult<TData = unknown> {
  data?: TData;
  error?: unknown;
  response?: { status?: number };
}

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null;

const readString = (value: Record<string, unknown>, key: string): string | undefined => {
  const candidate = value[key];
  return typeof candidate === 'string' ? candidate : undefined;
};

const readNumber = (value: Record<string, unknown>, key: string): number | undefined => {
  const candidate = value[key];
  return typeof candidate === 'number' ? candidate : undefined;
};

export function isProblemDetails(value: unknown): value is ProblemDetails {
  if (!isObject(value)) {
    return false;
  }
  return (
    typeof value.type === 'string' &&
    typeof value.title === 'string' &&
    typeof value.status === 'number' &&
    typeof value.requestId === 'string'
  );
}

export function isApiError(value: unknown): value is ApiError {
  if (!isObject(value)) {
    return false;
  }
  return (
    typeof value.code === 'string' &&
    typeof value.message === 'string' &&
    typeof value.requestId === 'string'
  );
}

export function mapApiError(error: unknown, fallbackStatus?: number): MappedApiError {
  if (isProblemDetails(error)) {
    return {
      status: error.status,
      code: error.code,
      title: error.title,
      detail: error.detail,
      requestId: error.requestId
    };
  }

  if (isApiError(error)) {
    return {
      status: fallbackStatus,
      code: error.code,
      title: error.message,
      detail: typeof error.details === 'string' ? error.details : undefined,
      requestId: error.requestId
    };
  }

  if (error instanceof Error) {
    return {
      status: fallbackStatus,
      title: error.message
    };
  }

  if (typeof error === 'string') {
    return {
      status: fallbackStatus,
      title: error
    };
  }

  if (isObject(error)) {
    return {
      status: readNumber(error, 'status') ?? fallbackStatus,
      code: readString(error, 'code'),
      title: readString(error, 'title') ?? readString(error, 'message'),
      detail: readString(error, 'detail'),
      requestId: readString(error, 'requestId')
    };
  }

  return {
    status: fallbackStatus
  };
}

export function setClientAuthToken(token?: string | null): void {
  const normalizedToken = normalizeToken(token);
  client.setConfig({
    headers: {
      Authorization: normalizedToken ? `Bearer ${normalizedToken}` : null
    }
  });

  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent(CLIENT_AUTH_CHANGED_EVENT, {
      detail: { token: normalizedToken }
    }));
  }
}

export function getClientAuthToken(): string | null {
  const authorization = readClientHeader('Authorization');
  if (!authorization) {
    return null;
  }

  const trimmed = authorization.trim();
  if (!trimmed) {
    return null;
  }

  if (trimmed.toLowerCase().startsWith('bearer ')) {
    return trimmed.slice(7).trim() || null;
  }

  return trimmed;
}

export function setClientHouseholdProfileId(profileId?: string | null): void {
  const normalizedProfileId = String(profileId || '').trim();
  client.setConfig({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: normalizedProfileId || null
    }
  });
}

export function getClientHouseholdProfileId(): string | null {
  const profileId = readClientHeader(HOUSEHOLD_PROFILE_HEADER);
  if (!profileId) {
    return null;
  }

  const trimmed = profileId.trim();
  return trimmed || null;
}

export function getApiBaseUrl(defaultBase: string = '/api/v3'): string {
  return (client.getConfig().baseUrl || defaultBase).replace(/\/$/, '');
}

interface ClientResultOptions {
  source?: string;
  silent?: boolean;
}

export function throwOnClientResultError(
  result: Pick<ClientResult, 'error' | 'response'>,
  options: ClientResultOptions = {}
): void {
  if (!result.error) {
    return;
  }

  const status = result.response?.status;
  const mapped = mapApiError(result.error, status);
  if (isUnauthorizedStatus(status)) {
    requestAuthRequired({
      source: options.source,
      status,
      code: mapped.code
    });
  }

  throw new ClientRequestError(mapped);
}

export function unwrapClientResultOrThrow<TData>(
  result: ClientResult<TData>,
  options: ClientResultOptions = {}
): TData {
  if (result.error) {
    if (options.silent) {
      const status = result.response?.status;
      const mapped = mapApiError(result.error, status);
      if (isUnauthorizedStatus(status)) {
        requestAuthRequired({
          source: options.source,
          status,
          code: mapped.code
        });
      }

      return undefined as TData;
    }

    throwOnClientResultError(result, options);
  }

  return result.data as TData;
}

export async function putJsonOrThrow<TBody>(url: string, body: TBody): Promise<void> {
  const result = await client.put<unknown, ApiError | ProblemDetails>({
    url,
    body,
    headers: { 'Content-Type': 'application/json' }
  });

  throwOnClientResultError(result, { source: `PUT ${url}` });
}

function normalizeToken(token?: string | null): string | null {
  const trimmed = String(token || '').trim();
  return trimmed || null;
}

function readClientHeader(name: string): string | null {
  const headers = client.getConfig().headers;
  if (!headers) {
    return null;
  }

  if (headers instanceof Headers) {
    return headers.get(name);
  }

  if (Array.isArray(headers)) {
    const match = headers.find(([key]) => key.toLowerCase() === name.toLowerCase());
    return match?.[1] || null;
  }

  const recordHeaders = headers as Record<string, unknown>;
  const direct = recordHeaders[name];
  if (typeof direct === 'string') {
    return direct;
  }

  const normalizedName = name.toLowerCase();
  for (const [key, value] of Object.entries(recordHeaders)) {
    if (key.toLowerCase() !== normalizedName || typeof value !== 'string') {
      continue;
    }
    return value;
  }

  return null;
}
