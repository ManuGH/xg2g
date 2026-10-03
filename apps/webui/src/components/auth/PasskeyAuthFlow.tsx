// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useState, useEffect, useRef, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import AuthSurface from '../AuthSurface';
import { Button } from '../ui';
import { resolveHostEnvironment } from '../../lib/hostBridge';
import {
  createPasskeyCredential,
  getPasskeyAssertion,
} from '../../lib/webauthn';
import {
  startPasskeyRegistration,
  finishPasskeyRegistration,
  startPasskeyLogin,
  finishPasskeyLogin,
  acknowledgeRecovery,
  loginWithRecoveryCode,
} from '../../services/passkeyApi';
import { debugError, formatError } from '../../utils/logging';

interface PasskeyAuthFlowProps {
  mode: 'bootstrap' | 'login' | 'expired';
  initialToken?: string;
  setupToken?: string;
  defaultUsername?: string;
  onSuccess: () => void;
  onSetToken: (token: string) => void;
}

export default function PasskeyAuthFlow({
  mode,
  initialToken = '',
  setupToken = '',
  defaultUsername = 'admin',
  onSuccess,
  onSetToken,
}: PasskeyAuthFlowProps) {
  const { t } = useTranslation();
  const hostEnv = resolveHostEnvironment();
  const isTvHost = hostEnv.isTv;

  const [step, setStep] = useState<'passkey' | 'recovery-backup' | 'recovery-login' | 'token-login'>('passkey');
  const [loading, setLoading] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [correlationId, setCorrelationId] = useState<string | null>(null);

  // Recovery code backup state
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [codesConfirmed, setCodesConfirmed] = useState(false);

  // Input states for fallbacks and bootstrap
  const [setupTokenInput, setSetupTokenInput] = useState(setupToken);
  const [recoveryCodeInput, setRecoveryCodeInput] = useState('');
  const [recoveryUsernameInput, setRecoveryUsernameInput] = useState(defaultUsername);
  const [apiTokenInput, setApiTokenInput] = useState(initialToken);
  const [isTokenVisible, setIsTokenVisible] = useState<boolean>(() => isTvHost);

  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setApiTokenInput(initialToken);
  }, [initialToken]);

  useEffect(() => {
    if (setupToken) {
      setSetupTokenInput(setupToken);
    }
  }, [setupToken]);

  useEffect(() => {
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
    });
  }, [step]);

  // Attempt WebAuthn Conditional UI on mount for login mode
  useEffect(() => {
    if (mode === 'login' && step === 'passkey' && !isTvHost) {
      let active = true;
      const attemptConditionalUI = async () => {
        try {
          if (window.PublicKeyCredential && (PublicKeyCredential as any).isConditionalMediationAvailable) {
            const available = await (PublicKeyCredential as any).isConditionalMediationAvailable();
            if (available && active) {
              const startRes = await startPasskeyLogin();
              const assertion = await getPasskeyAssertion(startRes.options, true);
              if (active && assertion) {
                const finishRes = await finishPasskeyLogin(assertion);
                if (finishRes && finishRes.user) {
                  onSuccess();
                }
              }
            }
          }
        } catch {
          // Conditional UI quiet catch - user will click "Mit Passkey anmelden" explicitly
        }
      };
      void attemptConditionalUI();
      return () => {
        active = false;
      };
    }
  }, [mode, step, isTvHost, onSuccess]);

  // Handler for First-Admin Passkey Creation
  const handleCreatePasskey = async (tokenToUse?: string) => {
    setLoading(true);
    setErrorMsg(null);
    setCorrelationId(null);
    try {
      const activeToken = (tokenToUse !== undefined ? tokenToUse : setupTokenInput).trim() || setupToken;
      const startRes = await startPasskeyRegistration('admin', activeToken);
      const attestation = await createPasskeyCredential(startRes.options);
      const finishRes = await finishPasskeyRegistration(attestation, 'Admin Passkey');

      if (finishRes.status === 'bootstrap_completed' && finishRes.recoveryCodes) {
        setRecoveryCodes(finishRes.recoveryCodes);
        setStep('recovery-backup');
      } else if (finishRes.status === 'registered' || finishRes.credential || finishRes.id) {
        onSuccess();
      } else {
        throw new Error(t('auth.bootstrap.passkeyFailed'));
      }
    } catch (err: any) {
      debugError('Failed to create passkey:', formatError(err));
      setErrorMsg(t('auth.bootstrap.passkeyCouldNotBeCreated'));
      setCorrelationId(err?.requestId || null);
    } finally {
      setLoading(false);
    }
  };

  // Handler for Acknowledging Recovery Codes (Final Bootstrap Commit)
  const handleCommitBootstrap = async () => {
    if (!codesConfirmed) return;
    setLoading(true);
    setErrorMsg(null);
    setCorrelationId(null);
    try {
      await acknowledgeRecovery();
      onSuccess();
    } catch (err: any) {
      debugError('Failed to acknowledge recovery codes:', formatError(err));
      setErrorMsg(t('auth.bootstrap.setupCouldNotBeCompleted'));
      setCorrelationId(err?.requestId || null);
    } finally {
      setLoading(false);
    }
  };

  // Handler for Passkey Login (Usernameless / Single Tap)
  const handlePasskeyLogin = async () => {
    setLoading(true);
    setErrorMsg(null);
    setCorrelationId(null);
    try {
      const startRes = await startPasskeyLogin();
      const assertion = await getPasskeyAssertion(startRes.options, false);
      const finishRes = await finishPasskeyLogin(assertion);

      if (finishRes && finishRes.user) {
        onSuccess();
      } else {
        throw new Error(t('auth.bootstrap.passkeyLoginFailed'));
      }
    } catch (err: any) {
      debugError('Failed to login with passkey:', formatError(err));
      setErrorMsg(t('auth.bootstrap.passkeyLoginFailed'));
      setCorrelationId(err?.requestId || null);
    } finally {
      setLoading(false);
    }
  };

  // Handler for Recovery Code Fallback Login (POST /api/v3/auth/recovery with username + code)
  const handleRecoveryLogin = async (e: FormEvent) => {
    e.preventDefault();
    const code = recoveryCodeInput.trim();
    const username = recoveryUsernameInput.trim() || 'admin';
    if (!code) return;

    setLoading(true);
    setErrorMsg(null);
    setCorrelationId(null);
    try {
      const res = await loginWithRecoveryCode(username, code);
      if (res && res.user) {
        onSuccess();
      } else {
        throw new Error(t('auth.bootstrap.invalidRecoveryCode'));
      }
    } catch (err: any) {
      debugError('Failed to login with recovery code:', formatError(err));
      setErrorMsg(t('auth.bootstrap.recoveryCodeRejected'));
      setCorrelationId(err?.requestId || null);
    } finally {
      setLoading(false);
    }
  };

  // Handler for Legacy API Token Fallback Login
  const handleTokenLogin = (e: FormEvent) => {
    e.preventDefault();
    const token = apiTokenInput.trim();
    if (!token) return;
    onSetToken(token);
  };

  // Copy Recovery Codes to Clipboard
  const handleCopyCodes = () => {
    const text = recoveryCodes.join('\n');
    void navigator.clipboard.writeText(text);
  };

  // Download Recovery Codes as text file
  const handleDownloadCodes = () => {
    const text = `xg2g Wiederherstellungscodes\nErstellt am: ${new Date().toLocaleString()}\n\n` + recoveryCodes.join('\n');
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'xg2g-recovery-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  const getSurfaceTitles = () => {
    if (mode === 'expired') {
      return {
        eyebrow: t('auth.expiredEyebrow'),
        title: t('auth.expiredTitle'),
        copy: t('auth.expiredCopy'),
      };
    }
    if (mode === 'bootstrap') {
      return {
        eyebrow: t('auth.bootstrap.eyebrow'),
        title: t('auth.bootstrap.title'),
        copy: t('auth.bootstrap.copy'),
      };
    }
    return {
      eyebrow: t('auth.requiredEyebrow'),
      title: t('auth.requiredTitle'),
      copy: t('auth.requiredCopy'),
    };
  };

  const titles = getSurfaceTitles();

  // RENDER STEP 1: BOOTSTRAP PASSKEY CREATION WITH SETUP-TOKEN SUPPORT
  if (mode === 'bootstrap' && step === 'passkey') {
    return (
      <AuthSurface
        eyebrow={titles.eyebrow}
        title={titles.title}
        copy={titles.copy}
        testId="bootstrap-passkey-surface"
        actions={
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', width: '100%' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
              <label style={{ fontSize: '0.8rem', color: 'var(--text-tertiary)' }}>{t('auth.bootstrap.setupTokenLabel')}</label>
              <input
                type="text"
                value={setupTokenInput}
                onChange={(e) => setSetupTokenInput(e.target.value)}
                placeholder={t('auth.bootstrap.setupTokenPlaceholder')}
                style={{
                  backgroundColor: 'rgba(255,255,255,0.05)',
                  border: '1px solid rgba(255,255,255,0.1)',
                  borderRadius: '6px',
                  padding: '0.5rem',
                  color: 'var(--text-primary)',
                  fontSize: '0.9rem',
                }}
                data-testid="bootstrap-setup-token-input"
              />
            </div>
            {errorMsg ? (
              <div style={{ color: 'var(--status-error)', fontSize: '0.875rem' }}>
                <span>{errorMsg}</span>
                {correlationId && (
                  <span data-testid="error-reference" style={{ marginLeft: '8px', opacity: 0.75, fontSize: '11px', fontFamily: 'monospace' }}>
                    ({correlationId})
                  </span>
                )}
              </div>
            ) : null}
            <Button
              onClick={() => { void handleCreatePasskey(); }}
              disabled={loading}
              style={{ width: '100%', padding: '0.75rem', fontSize: '1rem', fontWeight: 600 }}
              data-testid="create-passkey-button"
            >
              {loading ? t('auth.bootstrap.creatingPasskey') : t('auth.bootstrap.createPasskey')}
            </Button>
          </div>
        }
      />
    );
  }

  // RENDER STEP 2: RECOVERY CODES BACKUP & CONFIRMATION
  if (mode === 'bootstrap' && step === 'recovery-backup') {
    return (
      <AuthSurface
        eyebrow={t('auth.bootstrap.backupEyebrow')}
        title={t('auth.bootstrap.backupTitle')}
        copy={t('auth.bootstrap.backupCopy')}
        testId="bootstrap-recovery-surface"
      >
        <div style={{ margin: '1rem 0', display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: '0.5rem',
              backgroundColor: 'rgba(255,255,255,0.05)',
              padding: '1rem',
              borderRadius: '8px',
              fontFamily: 'monospace',
              fontSize: '0.9rem',
              textAlign: 'center',
            }}
          >
            {recoveryCodes.map((code, idx) => (
              <div key={idx} style={{ padding: '0.25rem' }}>
                {code}
              </div>
            ))}
          </div>

          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <Button variant="secondary" onClick={handleDownloadCodes} style={{ flex: 1, fontSize: '0.85rem' }}>
              {t('auth.bootstrap.downloadCodes')}
            </Button>
            <Button variant="secondary" onClick={handleCopyCodes} style={{ flex: 1, fontSize: '0.85rem' }}>
              {t('auth.bootstrap.copyCodes')}
            </Button>
          </div>

          <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem', cursor: 'pointer', marginTop: '0.5rem' }}>
            <input
              type="checkbox"
              checked={codesConfirmed}
              onChange={(e) => setCodesConfirmed(e.target.checked)}
              data-testid="confirm-recovery-codes-checkbox"
            />
            <span>{t('auth.bootstrap.codesSavedConfirm')}</span>
          </label>

          {errorMsg ? (
            <div style={{ color: 'var(--status-error)', fontSize: '0.875rem' }}>
              <span>{errorMsg}</span>
              {correlationId && (
                <span data-testid="error-reference" style={{ marginLeft: '8px', opacity: 0.75, fontSize: '11px', fontFamily: 'monospace' }}>
                  ({correlationId})
                </span>
              )}
            </div>
          ) : null}

          <Button
            onClick={() => { void handleCommitBootstrap(); }}
            disabled={!codesConfirmed || loading}
            style={{ width: '100%', padding: '0.75rem', marginTop: '0.5rem' }}
            data-testid="finish-bootstrap-button"
          >
            {loading ? t('auth.bootstrap.finishingSetup') : t('auth.bootstrap.finishSetup')}
          </Button>
        </div>
      </AuthSurface>
    );
  }

  // RENDER RECOVERY CODE FALLBACK LOGIN
  if (step === 'recovery-login') {
    return (
      <AuthSurface
        eyebrow={t('auth.bootstrap.recoveryEyebrow')}
        title={t('auth.bootstrap.recoveryTitle')}
        copy={t('auth.bootstrap.recoveryCopy')}
        testId="recovery-login-surface"
        form={{
          label: t('auth.bootstrap.recoveryCodeLabel'),
          name: 'recoveryCode',
          value: recoveryCodeInput,
          onValueChange: setRecoveryCodeInput,
          onSubmit: (e) => { void handleRecoveryLogin(e); },
          submitLabel: loading ? t('auth.bootstrap.checkingCode') : t('auth.bootstrap.signIn'),
          submitDisabled: loading || recoveryCodeInput.trim().length === 0,
          placeholder: t('auth.bootstrap.recoveryCodePlaceholder'),
          inputType: 'text',
          inputTestId: 'recovery-code-input',
          submitTestId: 'recovery-submit-button',
          inputRef,
        }}
        actions={
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', width: '100%', marginTop: '0.5rem' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', marginBottom: '0.5rem' }}>
              <label style={{ fontSize: '0.8rem', color: 'var(--text-tertiary)' }}>{t('auth.bootstrap.usernameLabel')}</label>
              <input
                type="text"
                value={recoveryUsernameInput}
                onChange={(e) => setRecoveryUsernameInput(e.target.value)}
                style={{
                  backgroundColor: 'rgba(255,255,255,0.05)',
                  border: '1px solid rgba(255,255,255,0.1)',
                  borderRadius: '6px',
                  padding: '0.5rem',
                  color: 'var(--text-primary)',
                  fontSize: '0.9rem',
                }}
                data-testid="recovery-username-input"
              />
            </div>
            {errorMsg ? (
              <div style={{ color: 'var(--status-error)', fontSize: '0.875rem' }}>
                <span>{errorMsg}</span>
                {correlationId && (
                  <span data-testid="error-reference" style={{ marginLeft: '8px', opacity: 0.75, fontSize: '11px', fontFamily: 'monospace' }}>
                    ({correlationId})
                  </span>
                )}
              </div>
            ) : null}
            <Button variant="ghost" onClick={() => setStep('passkey')} style={{ fontSize: '0.85rem' }}>
              {t('auth.bootstrap.backToPasskey')}
            </Button>
          </div>
        }
      />
    );
  }

  // DEFAULT RENDER: PASSKEY LOGIN SURFACE WITH SIDE-BY-SIDE API TOKEN FORM
  return (
    <AuthSurface
      testId="auth-surface"
      eyebrow={titles.eyebrow}
      title={titles.title}
      copy={titles.copy}
      form={{
        label: t('auth.tokenLabel'),
        name: 'token',
        value: apiTokenInput,
        onValueChange: setApiTokenInput,
        onSubmit: handleTokenLogin,
        submitLabel: t('auth.authenticate'),
        submitDisabled: apiTokenInput.trim().length === 0,
        placeholder: t('auth.tokenPlaceholder'),
        inputType: isTokenVisible ? 'text' : 'password',
        inputTestId: 'auth-token-input',
        submitTestId: 'auth-submit',
        inputRef,
        inputActions: (
          <>
            <Button
              variant="ghost"
              size="sm"
              aria-pressed={isTokenVisible}
              onClick={() => setIsTokenVisible((current) => !current)}
            >
              {isTokenVisible ? t('auth.hideToken') : t('auth.showToken')}
            </Button>
            {apiTokenInput ? (
              <Button variant="ghost" size="sm" onClick={() => setApiTokenInput('')}>
                {t('auth.clearToken')}
              </Button>
            ) : null}
          </>
        ),
      }}
      actions={
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', width: '100%', marginTop: '1rem' }}>
          {errorMsg ? (
            <div style={{ color: 'var(--status-error)', fontSize: '0.875rem' }}>
              <span>{errorMsg}</span>
              {correlationId && (
                <span data-testid="error-reference" style={{ marginLeft: '8px', opacity: 0.75, fontSize: '11px', fontFamily: 'monospace' }}>
                  ({correlationId})
                </span>
              )}
            </div>
          ) : null}

          <Button
            onClick={() => { void handlePasskeyLogin(); }}
            disabled={loading}
            style={{ width: '100%', padding: '0.75rem', fontSize: '1rem', fontWeight: 600 }}
            data-testid="passkey-login-button"
          >
            {loading ? t('auth.bootstrap.loggingIn') : t('auth.bootstrap.loginWithPasskey')}
          </Button>

          <div style={{ display: 'flex', justifyContent: 'center', marginTop: '0.5rem', fontSize: '0.85rem' }}>
            <Button
              variant="ghost"
              onClick={() => setStep('recovery-login')}
              style={{ fontSize: '0.8rem', padding: '0.25rem 0.5rem', color: 'var(--text-tertiary)' }}
              data-testid="recovery-login-link"
            >
              {t('auth.bootstrap.useRecoveryCode')}
            </Button>
          </div>
        </div>
      }
    />
  );
}
