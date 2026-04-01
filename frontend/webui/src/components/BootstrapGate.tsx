import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { ClientRequestError } from '../services/clientWrapper';
import { subscribeAuthRequired } from '../features/player/sessionEvents';
import { useAppContext } from '../context/AppContext';
import { queryKeys, useBootstrapConfig } from '../hooks/useServerQueries';
import { useTvInitialFocus } from '../hooks/useTvInitialFocus';
import { resolveHostEnvironment } from '../lib/hostBridge';
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
  const navigate = useNavigate();
  const { auth, setToken, setPlayingChannel } = useAppContext();
  const queryClient = useQueryClient();
  const hostEnvironment = useMemo(() => resolveHostEnvironment(), []);
  const isTvHost = hostEnvironment.isTv;
  const authReady = auth.isReady ?? true;
  const [tokenValue, setTokenValue] = useState('');
  const [forcedAuthPrompt, setForcedAuthPrompt] = useState<AuthPromptReason | null>(null);
  const [isTokenVisible, setIsTokenVisible] = useState<boolean>(() => isTvHost);
  const inputRef = useRef<HTMLInputElement>(null);
  const {
    data: config = null,
    error,
    isLoading,
    refetch,
  } = useBootstrapConfig(auth.isAuthenticated && authReady);

  const handleAuthRequired = useCallback(() => {
    setForcedAuthPrompt('expired');
    setTokenValue((current) => current.trim() || auth.token || '');
    setPlayingChannel(null);
    setToken('');
  }, [auth.token, setPlayingChannel, setToken]);

  useEffect(() => {
    return subscribeAuthRequired(() => {
      handleAuthRequired();
    });
  }, [handleAuthRequired]);

  const bootstrapStatus = getErrorStatus(error);
  const isUnauthorized = bootstrapStatus === 401;
  const isSettingsRoute = normalizePathname(pathname) === ROUTE_MAP.settings;
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
    if (isUnauthorized) {
      return 'expired';
    }
    if (!auth.isAuthenticated) {
      return 'missing';
    }
    return null;
  }, [auth.isAuthenticated, authReady, forcedAuthPrompt, isUnauthorized]);

  useEffect(() => {
    if (auth.isAuthenticated && !isUnauthorized && config) {
      setForcedAuthPrompt(null);
      setTokenValue('');
    }
  }, [auth.isAuthenticated, config, isUnauthorized]);

  useEffect(() => {
    if (isUnauthorized) {
      handleAuthRequired();
    }
  }, [handleAuthRequired, isUnauthorized]);

  useEffect(() => {
    if (authReason !== null) {
      setIsTokenVisible(isTvHost);
    }
  }, [authReason, isTvHost]);
  useTvInitialFocus({
    enabled: authReason !== null,
    targetRef: inputRef,
  });

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

    // Clear stale 401 bootstrap state before we apply the replacement token.
    // Otherwise the old unauthorized query error can immediately trip the
    // auth-required effect again and wipe the fresh token during re-auth.
    void queryClient.resetQueries({
      queryKey: queryKeys.bootstrapConfig,
      exact: true,
    });
    setForcedAuthPrompt(null);
    setTokenValue(token);
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
    const authBaseHint = authReason === 'expired'
      ? t('auth.expiredHint', {
        defaultValue: 'Submitting a new token will retry startup automatically.',
      })
      : t('auth.requiredHint', {
        defaultValue: 'The token is stored locally in this browser after successful sign-in.',
      });
    const authHint = isTvHost
      ? `${authBaseHint} ${t('auth.tvHint', {
        defaultValue: 'On TV the token can stay visible while typing so you can spot mistakes immediately.',
      })}`
      : authBaseHint;

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
          hint: authHint,
          inputType: isTokenVisible ? 'text' : 'password',
          inputActions: (
            <>
              <Button
                variant="ghost"
                size="sm"
                aria-pressed={isTokenVisible}
                onClick={() => {
                  setIsTokenVisible((current) => !current);
                  window.requestAnimationFrame(() => inputRef.current?.focus());
                }}
              >
                {isTokenVisible
                  ? t('auth.hideToken', { defaultValue: 'Hide token' })
                  : t('auth.showToken', { defaultValue: 'Show token' })}
              </Button>
              {tokenValue ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setTokenValue('');
                    inputRef.current?.focus();
                  }}
                >
                  {t('auth.clearToken', { defaultValue: 'Clear' })}
                </Button>
              ) : null}
            </>
          ),
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

  if (monetizationLocked && !isSettingsRoute) {
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
