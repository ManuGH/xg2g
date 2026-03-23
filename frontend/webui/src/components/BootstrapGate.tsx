import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ClientRequestError } from '../services/clientWrapper';
import { subscribeAuthRequired } from '../features/player/sessionEvents';
import { useAppContext } from '../context/AppContext';
import { useBootstrapConfig } from '../hooks/useServerQueries';
import { normalizePathname, ROUTE_MAP } from '../routes';
import { isConfigured } from './Config';
import AuthSurface from './AuthSurface';
import LoadingSkeleton from './LoadingSkeleton';
import { Button } from './ui';

type AuthPromptReason = 'missing' | 'expired';

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

export default function BootstrapGate() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const { auth, setToken, setPlayingChannel } = useAppContext();
  const authReady = auth.isReady ?? true;
  const [tokenValue, setTokenValue] = useState('');
  const [forcedAuthPrompt, setForcedAuthPrompt] = useState<AuthPromptReason | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const {
    data: config = null,
    error,
    isLoading,
    refetch,
  } = useBootstrapConfig(auth.isAuthenticated && authReady);

  const handleAuthRequired = useCallback(() => {
    setForcedAuthPrompt('expired');
    setTokenValue('');
    setPlayingChannel(null);
    setToken('');
  }, [setPlayingChannel, setToken]);

  useEffect(() => {
    return subscribeAuthRequired(() => {
      handleAuthRequired();
    });
  }, [handleAuthRequired]);

  const bootstrapStatus = getErrorStatus(error);
  const isUnauthorized = bootstrapStatus === 401;
  const isSettingsRoute = normalizePathname(pathname) === ROUTE_MAP.settings;
  const authReason: AuthPromptReason | null = useMemo(() => {
    if (forcedAuthPrompt) {
      return forcedAuthPrompt;
    }
    if (!authReady) {
      return null;
    }
    if (isUnauthorized) {
      return 'expired';
    }
    if (!auth.isAuthenticated) {
      return 'missing';
    }
    return null;
  }, [auth.isAuthenticated, authReady, forcedAuthPrompt, isUnauthorized]);

  useEffect(() => {
    if (auth.isAuthenticated && !isUnauthorized) {
      setForcedAuthPrompt(null);
      setTokenValue('');
    }
  }, [auth.isAuthenticated, isUnauthorized]);

  useEffect(() => {
    if (isUnauthorized) {
      handleAuthRequired();
    }
  }, [handleAuthRequired, isUnauthorized]);

  useEffect(() => {
    if (authReason) {
      inputRef.current?.focus();
    }
  }, [authReason]);

  if (!authReady) {
    return <LoadingSkeleton variant="gate" label={t('app.initializing', { defaultValue: 'Initializing...' })} />;
  }

  const handleAuthSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const token = tokenValue.trim();
    if (!token) {
      setTokenValue('');
      inputRef.current?.focus();
      return;
    }

    setTokenValue('');
    setToken(token);
  };

  if (authReason) {
    const authTitle =
      authReason === 'expired'
        ? t('auth.expiredTitle', { defaultValue: 'Session Expired' })
        : t('auth.requiredTitle', { defaultValue: 'Authentication Required' });
    const authCopy =
      authReason === 'expired'
        ? t('auth.expiredCopy', {
          defaultValue: 'Your saved API token was rejected. Enter a valid token to continue.',
        })
        : t('auth.requiredCopy', {
          defaultValue: 'Enter your API token to open the xg2g control surface.',
        });
    const authEyebrow =
      authReason === 'expired'
        ? t('auth.expiredEyebrow', { defaultValue: 'Re-authenticate' })
        : t('auth.requiredEyebrow', { defaultValue: 'Sign in' });

    return (
      <AuthSurface
        eyebrow={authEyebrow}
        title={authTitle}
        copy={authCopy}
        form={{
          label: t('auth.tokenLabel', { defaultValue: 'API Token' }),
          name: 'token',
          value: tokenValue,
          onValueChange: setTokenValue,
          onSubmit: handleAuthSubmit,
          submitLabel: t('auth.authenticate', { defaultValue: 'Authenticate' }),
          submitDisabled: tokenValue.trim().length === 0,
          placeholder: t('auth.tokenPlaceholder', { defaultValue: 'Enter API Token' }),
          inputRef,
          hint: authReason === 'expired'
            ? t('auth.expiredHint', {
              defaultValue: 'Submitting a new token will retry startup automatically.',
            })
            : t('auth.requiredHint', {
              defaultValue: 'The token is stored locally in this browser after successful sign-in.',
            }),
        }}
      />
    );
  }

  if (isLoading) {
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

  if (!isConfigured(config) && !isSettingsRoute) {
    return <Navigate to={ROUTE_MAP.settings} replace />;
  }

  return <Outlet />;
}
