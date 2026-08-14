import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { ClientRequestError } from '../services/clientWrapper';
import { subscribeAuthRequired } from '../features/player/sessionEvents';
import { useAppContext } from '../context/AppContext';
import { useBootstrapConfig } from '../hooks/useServerQueries';
import { useTvInitialFocus } from '../hooks/useTvInitialFocus';
import { normalizePathname, ROUTE_MAP, UNLOCK_ROUTE } from '../routes';
import { isConfigured } from './Config';
import AuthSurface from './AuthSurface';
import PasskeyAuthFlow from './auth/PasskeyAuthFlow';
import LoadingSkeleton from './LoadingSkeleton';
import { Button } from './ui';

type AuthPromptReason = 'missing' | 'expired';
type AuthRetryState = { token: string; phase: 'pending' | 'fetching' | 'settled' };

// Keep purchase and diagnostics routes in this list when they must remain reachable
// while the monetization gate is locked, otherwise the user cannot complete unlock.
export const BOOTSTRAP_GATE_BYPASS_ROUTES: readonly string[] = [ROUTE_MAP.settings, UNLOCK_ROUTE];

function getErrorStatus(error: unknown): number | undefined {
  if (error instanceof ClientRequestError) {
    return error.status;
  }

  if (typeof error === 'object' && error !== null && 'status' in error) {
    const status = (error as { status?: unknown }).status;
    return typeof status === 'number' ? status : undefined;
  }

  return undefined;
}

function getErrorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return 'Unable to load the system configuration.';
}

function isBypassRoute(pathname: string): boolean {
  const normalizedPath = normalizePathname(pathname);
  return BOOTSTRAP_GATE_BYPASS_ROUTES.some((route) => (
    normalizedPath === route || normalizedPath.startsWith(`${route}/`)
  ));
}

export default function BootstrapGate() {
  const { t } = useTranslation();
  const location = useLocation();
  const pathname = location.pathname;
  const navigate = useNavigate();
  const { auth, setToken, setPlayingChannel, setServerSessionAuthenticated } = useAppContext();
  const authReady = auth.isReady ?? true;
  const hasToken = Boolean(auth.token?.trim());
  const [tokenValue, setTokenValue] = useState('');
  const [forcedAuthPrompt, setForcedAuthPrompt] = useState<AuthPromptReason | null>(null);
  const [authRetry, setAuthRetry] = useState<AuthRetryState | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const authRetryInFlightRef = useRef(false);
  const {
    data: config = null,
    error,
    isLoading,
    refetch,
  } = useBootstrapConfig(authReady);

  const handleAuthRequired = useCallback(() => {
    setAuthRetry(null);
    setForcedAuthPrompt(auth.isAuthenticated ? 'expired' : 'missing');
    setTokenValue((current) => current.trim() || auth.token || '');
    setPlayingChannel(null);
    if (hasToken) {
      setToken('');
      return;
    }
    setServerSessionAuthenticated(false);
  }, [auth.isAuthenticated, auth.token, hasToken, setPlayingChannel, setServerSessionAuthenticated, setToken]);

  useEffect(() => {
    return subscribeAuthRequired(() => {
      handleAuthRequired();
    });
  }, [handleAuthRequired]);

  const bootstrapStatus = getErrorStatus(error);
  const isUnauthorized = bootstrapStatus === 401;
  const isSuppressingBootstrap401 = Boolean(
    authRetry &&
    authRetry.token === auth.token &&
    authRetry.phase !== 'settled'
  );
  const shouldTreatAsUnauthorized = isUnauthorized && !isSuppressingBootstrap401;
  const bypassRoute = isBypassRoute(pathname);
  const monetizationLocked = Boolean(
    config?.monetization?.enabled &&
    config.monetization.model === 'one_time_unlock' &&
    config.monetization.enforcement === 'required' &&
    config.monetization.unlocked === false
  );
  const authReason: AuthPromptReason | null = useMemo(() => {
    if (forcedAuthPrompt) {
      return forcedAuthPrompt;
    }
    if (!authReady) {
      return null;
    }
    if (shouldTreatAsUnauthorized) {
      return auth.isAuthenticated ? 'expired' : 'missing';
    }
    if (error) {
      return null;
    }
    if (config) {
      return null;
    }
    if (!auth.isAuthenticated) {
      return 'missing';
    }
    return null;
  }, [auth.isAuthenticated, authReady, config, error, forcedAuthPrompt, shouldTreatAsUnauthorized]);

  useEffect(() => {
    if (!config || shouldTreatAsUnauthorized || forcedAuthPrompt !== null) {
      return;
    }
    setAuthRetry(null);
    setServerSessionAuthenticated(true);
    setTokenValue('');
  }, [config, forcedAuthPrompt, setServerSessionAuthenticated, shouldTreatAsUnauthorized]);

  useEffect(() => {
    if (authReady && shouldTreatAsUnauthorized) {
      handleAuthRequired();
    }
  }, [authReady, handleAuthRequired, shouldTreatAsUnauthorized]);

  useEffect(() => {
    if (
      !authRetry ||
      authRetry.phase !== 'pending' ||
      !authReady ||
      auth.token !== authRetry.token ||
      authRetryInFlightRef.current
    ) {
      return;
    }

    authRetryInFlightRef.current = true;
    setAuthRetry((current) => (
      current?.token === authRetry.token ? { ...current, phase: 'fetching' } : current
    ));
    void refetch().finally(() => {
      authRetryInFlightRef.current = false;
      setAuthRetry((current) => (
        current?.token === authRetry.token ? { ...current, phase: 'settled' } : current
      ));
    });
  }, [auth.token, authReady, authRetry, refetch]);

  useTvInitialFocus({
    enabled: authReason !== null,
    targetRef: inputRef,
  });

  if (!authReady) {
    return <LoadingSkeleton variant="gate" label={t('app.initializing', { defaultValue: 'Initializing...' })} />;
  }

  const searchParams = new URLSearchParams(location.search);
  const setupTokenFromUrl = searchParams.get('setup_token') || searchParams.get('token') || '';

  if (setupTokenFromUrl || (config as any)?.setupRequired || (config as any)?.identityReady === false) {
    return (
      <PasskeyAuthFlow
        mode="bootstrap"
        setupToken={setupTokenFromUrl}
        onSuccess={() => {
          void refetch();
        }}
        onSetToken={(t) => {
          setToken(t);
        }}
      />
    );
  }

  if (authReason) {
    const fromPath = (location.state as { from?: string })?.from || new URLSearchParams(location.search).get('redirect') || '/';
    return (
      <PasskeyAuthFlow
        mode={authReason === 'expired' ? 'expired' : 'login'}
        initialToken={tokenValue || auth.token || undefined}
        onSuccess={() => {
          setForcedAuthPrompt(null);
          setServerSessionAuthenticated(true);
          void refetch();
          if (fromPath && fromPath !== pathname) {
            navigate(fromPath, { replace: true });
          }
        }}
        onSetToken={(t) => {
          setForcedAuthPrompt(null);
          setTokenValue(t);
          setAuthRetry({ token: t, phase: 'pending' });
          setToken(t);
        }}
      />
    );
  }

  if (isLoading || isSuppressingBootstrap401) {
    return <LoadingSkeleton variant="gate" label={t('app.initializing', { defaultValue: 'Initializing...' })} />;
  }

  if (error) {
    return (
      <AuthSurface
        eyebrow={t('app.bootstrapErrorEyebrow', { defaultValue: 'Recovery' })}
        title={t('app.bootstrapErrorTitle', { defaultValue: 'Unable to start xg2g' })}
        copy={getErrorMessage(error)}
        actions={(
          <Button onClick={() => { void refetch(); }}>
            {t('common.retry', { defaultValue: 'Retry' })}
          </Button>
        )}
      />
    );
  }

  if (!isConfigured(config) && !bypassRoute) {
    return <Navigate to={ROUTE_MAP.settings} replace />;
  }

  if (monetizationLocked && !bypassRoute) {
    const productName = config?.monetization?.productName?.trim() || 'xg2g Unlock';
    const purchaseUrl = config?.monetization?.purchaseUrl?.trim();

    return (
      <AuthSurface
        eyebrow={t('unlock.eyebrow', { defaultValue: 'Unlock Required' })}
        title={t('unlock.title', {
          defaultValue: `${productName} required`,
        })}
        copy={purchaseUrl
          ? t('unlock.copyWithUrl', {
            defaultValue: 'This app remains free to install, but this server requires a one-time unlock before playback surfaces open. Complete the unlock, then sign in again if needed.',
          })
          : t('unlock.copyNoUrl', {
            defaultValue: 'This app remains free to install, but this server requires a one-time unlock before playback surfaces open. Contact the operator for access.',
          })}
        actions={(
          <>
            {purchaseUrl ? (
              <Button href={purchaseUrl} target="_blank" rel="noreferrer">
                {t('unlock.openInfo', { defaultValue: 'Open Unlock Info' })}
              </Button>
            ) : null}
            <Button
              variant="secondary"
              onClick={() => {
                navigate(UNLOCK_ROUTE);
              }}
            >
              {t('unlock.viewStatus', { defaultValue: 'View Unlock Status' })}
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                navigate(ROUTE_MAP.settings);
              }}
            >
              {t('unlock.openSettings', { defaultValue: 'Open Settings' })}
            </Button>
          </>
        )}
      />
    );
  }

  return <Outlet />;
}
