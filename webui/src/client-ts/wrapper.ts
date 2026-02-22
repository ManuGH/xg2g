import { client } from './client.gen';
import type { ApiError, ProblemDetails } from './types.gen';

export type MappedApiError = {
  status?: number;
  code?: string;
  title: string;
  detail?: string;
  requestId?: string;
};

export class ClientRequestError extends Error {
  readonly status?: number;
  readonly code?: string;
  readonly requestId?: string;
  readonly detail?: string;

  constructor(mapped: MappedApiError) {
    super(mapped.detail ?? mapped.title);
    this.name = 'ClientRequestError';
    this.status = mapped.status;
    this.code = mapped.code;
    this.requestId = mapped.requestId;
    this.detail = mapped.detail;
  }
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
      title: readString(error, 'title') ?? readString(error, 'message') ?? 'Request failed',
      detail: readString(error, 'detail'),
      requestId: readString(error, 'requestId')
    };
  }

  return {
    status: fallbackStatus,
    title: 'Request failed'
  };
}

export function setClientAuthToken(token?: string | null): void {
  client.setConfig({
    headers: {
      Authorization: token ? `Bearer ${token}` : null
    }
  });
}

export function getApiBaseUrl(defaultBase: string = '/api/v3'): string {
  return (client.getConfig().baseUrl || defaultBase).replace(/\/$/, '');
}

export async function putJsonOrThrow<TBody>(url: string, body: TBody): Promise<void> {
  const result = await client.put<unknown, ApiError | ProblemDetails>({
    url,
    body,
    headers: { 'Content-Type': 'application/json' }
  });

  if (result.error) {
    throw new ClientRequestError(mapApiError(result.error, result.response?.status));
  }
}
