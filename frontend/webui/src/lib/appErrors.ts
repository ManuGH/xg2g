import { ClientRequestError, mapApiError } from './clientWrapper';
import { getErrorCatalogEntry } from './errorCatalog';
import type { AppError, AppErrorSeverity } from '../types/errors';

interface AppErrorOptions {
  fallbackTitle?: string;
  fallbackDetail?: string;
  retryable?: boolean;
}

interface PlayerErrorOptions extends AppErrorOptions {
  status?: number;
}

function isAppError(value: unknown): value is AppError {
  return typeof value === 'object' && value !== null && 'title' in value && 'retryable' in value;
}

function getStatusCopy(status: number): Pick<AppError, 'title' | 'detail' | 'retryable' | 'severity'> {
  switch (status) {
    case 401:
      return {
        title: 'Authentication required',
        detail: 'Please sign in again to continue.',
        retryable: false,
        severity: 'warning',
      };
    case 403:
      return {
        title: 'Access denied',
        detail: 'You do not have permission to view this area.',
        retryable: false,
        severity: 'warning',
      };
    case 404:
      return {
        title: 'Not found',
        detail: 'The requested resource is not available.',
        retryable: false,
        severity: 'warning',
      };
    case 408:
    case 429:
    case 502:
    case 503:
    case 504:
      return {
        title: 'Service unavailable',
        detail: 'xg2g could not complete this request right now.',
        retryable: true,
        severity: 'error',
      };
    default:
      if (status >= 500) {
        return {
          title: 'Service unavailable',
          detail: 'xg2g could not complete this request right now.',
          retryable: true,
          severity: 'error',
        };
      }

      return {
        title: 'Request failed',
        retryable: true,
        severity: 'error',
      };
  }
}

function getCatalogCopy(code?: string): Partial<AppError> {
  const entry = getErrorCatalogEntry(code);
  if (!entry) {
    return {};
  }

  return {
    title: entry.title,
    detail: entry.description,
    severity: entry.severity as AppErrorSeverity,
    retryable: entry.retryable,
    operatorHint: entry.operatorHint,
    runbookUrl: entry.runbookUrl ?? undefined,
  };
}

function enrichAppError(error: AppError): AppError {
  const catalog = getCatalogCopy(error.code);
  return {
    title: error.title || catalog.title || 'Something went wrong',
    detail: error.detail ?? catalog.detail,
    status: error.status,
    retryable: error.retryable,
    code: error.code,
    requestId: error.requestId,
    severity: error.severity ?? catalog.severity,
    operatorHint: error.operatorHint ?? catalog.operatorHint,
    runbookUrl: error.runbookUrl ?? catalog.runbookUrl,
  };
}

function readStringField(value: unknown, key: string): string | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined;
  }
  const candidate = (value as Record<string, unknown>)[key];
  return typeof candidate === 'string' && candidate ? candidate : undefined;
}

function stringifyDetail(value: unknown): string | undefined {
  if (typeof value === 'string' && value) {
    return value;
  }

  if (value == null) {
    return undefined;
  }

  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

export function toAppError(error: unknown, options: AppErrorOptions = {}): AppError {
  if (isAppError(error)) {
    return enrichAppError(error);
  }

  const mapped =
    error instanceof ClientRequestError
      ? {
        status: error.status,
        code: error.code,
        title: error.title === 'Request failed' ? undefined : error.title,
        detail: error.detail,
        requestId: error.requestId,
      }
      : mapApiError(error);

  const catalog = getCatalogCopy(mapped.code);
  const statusCopy = typeof mapped.status === 'number' ? getStatusCopy(mapped.status) : null;
  const title =
    mapped.title ??
    catalog.title ??
    statusCopy?.title ??
    options.fallbackTitle ??
    'Something went wrong';
  const detail =
    mapped.detail ??
    options.fallbackDetail ??
    catalog.detail ??
    (mapped.title && mapped.title !== title ? mapped.title : undefined) ??
    statusCopy?.detail;
  const retryable =
    options.retryable ??
    catalog.retryable ??
    statusCopy?.retryable ??
    (typeof mapped.status === 'number' ? mapped.status >= 500 || mapped.status === 408 || mapped.status === 429 : true);

  return {
    title,
    detail,
    status: mapped.status,
    retryable,
    code: mapped.code,
    requestId: mapped.requestId,
    severity: catalog.severity ?? statusCopy?.severity,
    operatorHint: catalog.operatorHint,
    runbookUrl: catalog.runbookUrl,
  };
}

export function normalizePlayerError(error: unknown, options: PlayerErrorOptions = {}): AppError {
  const errorWithStatus =
    typeof options.status === 'number' &&
    typeof error === 'object' &&
    error !== null &&
    !('status' in error)
      ? { ...(error as Record<string, unknown>), status: options.status }
      : error;

  const base = toAppError(errorWithStatus, options);
  const inlineDetail =
    stringifyDetail(
      typeof error === 'object' && error !== null && 'details' in error
        ? (error as { details?: unknown }).details
        : undefined
    ) ??
    stringifyDetail(
      typeof error === 'object' && error !== null && 'detail' in error
        ? (error as { detail?: unknown }).detail
        : undefined
    );

  const code = readStringField(error, 'code');
  const requestId = readStringField(error, 'requestId');
  const type = readStringField(error, 'type');
  const metadataParts = [
    code ? `code=${code}` : null,
    type && type !== 'about:blank' ? type : null,
    requestId ? `requestId=${requestId}` : null,
  ].filter((part): part is string => Boolean(part));

  const detailParts = [
    options.fallbackDetail ?? base.detail ?? inlineDetail ?? (error instanceof Error ? error.stack ?? undefined : undefined),
    ...metadataParts.filter((part) => !(base.detail ?? inlineDetail ?? '').includes(part)),
  ].filter((part): part is string => Boolean(part));

  return {
    title: base.title,
    detail: detailParts.length > 0 ? detailParts.join(' · ') : undefined,
    status: base.status ?? options.status,
    retryable: options.retryable ?? base.retryable,
    code: base.code ?? code,
    requestId: base.requestId ?? requestId,
    severity: base.severity,
    operatorHint: base.operatorHint,
    runbookUrl: base.runbookUrl,
  };
}
