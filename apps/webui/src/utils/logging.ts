export function isDebugLogsEnabled(): boolean {
  return (
    Boolean(import.meta.env.DEV) &&
    (import.meta.env.MODE !== 'test' || import.meta.env.VITE_XG2G_TEST_DEBUG_LOGS === '1') &&
    import.meta.env.VITE_XG2G_DEBUG_LOGS !== '0'
  );
}

export function debugLog(...args: unknown[]): void {
  if (!isDebugLogsEnabled()) {
    return;
  }
  console.log(...args);
}

export function debugWarn(...args: unknown[]): void {
  if (!isDebugLogsEnabled()) {
    return;
  }
  console.warn(...args);
}

export function debugError(...args: unknown[]): void {
  if (!isDebugLogsEnabled()) {
    return;
  }
  console.error(...args);
}

export function redactToken(token?: string | null): string {
  if (!token) {
    return '';
  }
  const trimmed = token.trim();
  if (trimmed.length <= 8) {
    return '***';
  }
  return `${trimmed.slice(0, 4)}...${trimmed.slice(-4)}`;
}

export function formatError(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === 'string') {
    return err;
  }
  return 'unknown error';
}

export interface DiagnosticSummary {
  operation: string;
  status?: number;
  code?: string;
  correlationId?: string;
}

export function summarizeDiagnostic(operation: string, err: unknown): DiagnosticSummary {
  const cleanOp =
    typeof operation === 'string' && /^[a-zA-Z0-9_.:-]+$/.test(operation.trim())
      ? operation.trim()
      : 'unknown_operation';

  let status: number | undefined;
  let code: string | undefined;
  let correlationId: string | undefined;

  if (typeof err === 'object' && err !== null) {
    const errObj = err as Record<string, unknown>;

    // Validate status (HTTP 100..599)
    const rawStatus =
      typeof errObj.status === 'number'
        ? errObj.status
        : typeof errObj.statusCode === 'number'
          ? errObj.statusCode
          : undefined;
    if (typeof rawStatus === 'number' && Number.isInteger(rawStatus) && rawStatus >= 100 && rawStatus <= 599) {
      status = rawStatus;
    }

    // Validate code (alphanumeric uppercase and underscore only, 2..50 chars)
    if (typeof errObj.code === 'string' && /^[A-Z0-9_]{2,50}$/.test(errObj.code.trim())) {
      code = errObj.code.trim();
    } else if (err instanceof TypeError) {
      code = 'NETWORK_ERROR';
    } else if (err instanceof DOMException && err.name === 'AbortError') {
      code = 'ABORTED';
    } else if (typeof errObj.name === 'string' && /^[A-Za-z0-9_]{2,50}$/.test(errObj.name)) {
      if (errObj.name === 'AbortError') code = 'ABORTED';
      else if (errObj.name === 'TimeoutError') code = 'TIMEOUT';
      else if (errObj.name === 'TypeError') code = 'NETWORK_ERROR';
    }

    // Derive code from status if code not explicitly provided
    if (!code && status !== undefined) {
      switch (status) {
        case 400: code = 'BAD_REQUEST'; break;
        case 401: code = 'UNAUTHORIZED'; break;
        case 403: code = 'FORBIDDEN'; break;
        case 404: code = 'NOT_FOUND'; break;
        case 409: code = 'CONFLICT'; break;
        case 429: code = 'RATE_LIMITED'; break;
        case 500: code = 'INTERNAL_ERROR'; break;
        case 502: code = 'BAD_GATEWAY'; break;
        case 503: code = 'SERVICE_UNAVAILABLE'; break;
        case 504: code = 'GATEWAY_TIMEOUT'; break;
        default:
          code = status >= 500 ? 'SERVER_ERROR' : status >= 400 ? 'CLIENT_ERROR' : 'REQUEST_ERROR';
      }
    }

    // Validate correlationId (alphanumeric, dashes, underscores, max 64 chars)
    const rawCorr =
      typeof errObj.requestId === 'string'
        ? errObj.requestId
        : typeof errObj.correlationId === 'string'
          ? errObj.correlationId
          : undefined;
    if (typeof rawCorr === 'string' && /^[a-zA-Z0-9_-]{1,64}$/.test(rawCorr.trim())) {
      correlationId = rawCorr.trim();
    }
  } else if (err instanceof TypeError) {
    code = 'NETWORK_ERROR';
  }

  if (!code && !status) {
    code = 'UNKNOWN_ERROR';
  }

  return {
    operation: cleanOp,
    ...(status !== undefined ? { status } : {}),
    ...(code ? { code } : {}),
    ...(correlationId ? { correlationId } : {}),
  };
}

export function formatDiagnosticSummary(operation: string, err: unknown): string {
  const summary = summarizeDiagnostic(operation, err);
  const parts = [`[${summary.operation}]`];
  if (summary.code) parts.push(`code=${summary.code}`);
  if (summary.status !== undefined) parts.push(`status=${summary.status}`);
  if (summary.correlationId) parts.push(`correlationId=${summary.correlationId}`);
  return parts.join(' ');
}

export function debugDiagnosticError(operation: string, err: unknown): void {
  if (!isDebugLogsEnabled()) {
    return;
  }
  console.error(formatDiagnosticSummary(operation, err));
}

export interface PlayerTelemetryEvent {
  event:
    | 'player.buffer_low'
    | 'player.stall_started'
    | 'player.stall_recovered'
    | 'player.network_error'
    | 'player.media_error'
    | 'player.recovery_attempted'
    | 'player.intent_recreated';
  playbackInstanceId: string;
  intentId?: string;
  sessionId?: string;
  reason?: string;
  details?: Record<string, unknown>;
}

export function emitPlayerTelemetry(payload: PlayerTelemetryEvent): void {
  debugLog('[TELEMETRY]', payload);
}
