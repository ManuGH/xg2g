export const AUTH_REQUIRED_EVENT = 'auth-required';

export interface AuthRequiredDetail {
  source?: string;
  status?: number;
  code?: string;
}

type AuthRequiredHandler = (detail: AuthRequiredDetail | undefined) => void;

function readAuthRequiredDetail(event: Event): AuthRequiredDetail | undefined {
  if (!(event instanceof CustomEvent)) {
    return undefined;
  }

  const detail = event.detail;
  if (typeof detail !== 'object' || detail === null) {
    return undefined;
  }

  return detail as AuthRequiredDetail;
}

export function requestAuthRequired(detail?: AuthRequiredDetail): void {
  window.dispatchEvent(new CustomEvent<AuthRequiredDetail | undefined>(AUTH_REQUIRED_EVENT, { detail }));
}

export function subscribeAuthRequired(handler: AuthRequiredHandler): () => void {
  const wrapped = (event: Event) => {
    handler(readAuthRequiredDetail(event));
  };

  window.addEventListener(AUTH_REQUIRED_EVENT, wrapped);
  return () => window.removeEventListener(AUTH_REQUIRED_EVENT, wrapped);
}

export function isUnauthorizedStatus(status?: number): status is 401 {
  return status === 401;
}

export function isUnauthorizedError(error: unknown): boolean {
  if (typeof error !== 'object' || error === null || !('status' in error)) {
    return false;
  }

  return isUnauthorizedStatus((error as { status?: unknown }).status as number | undefined);
}
