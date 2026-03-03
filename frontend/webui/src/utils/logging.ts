const isDev = import.meta.env.DEV;

export function debugLog(...args: unknown[]): void {
  if (!isDev) {
    return;
  }
  console.log(...args);
}

export function debugWarn(...args: unknown[]): void {
  if (!isDev) {
    return;
  }
  console.warn(...args);
}

export function debugError(...args: unknown[]): void {
  if (!isDev) {
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
