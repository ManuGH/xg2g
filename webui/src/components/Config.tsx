// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect, FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemConfig, putSystemConfig, type AppConfig, type ConfigUpdate } from '../client-ts';
import { debugError, formatError } from '../utils/logging';
import { getStoredToken } from '../utils/tokenStorage';
import { Button, StatusChip } from './ui';
import styles from './Config.module.css';

interface ValidationResponse {
  valid: boolean;
  message?: string;
  version?: {
    info?: {
      brand?: string;
      model?: string;
    };
  };
  bouquets?: string[];
  [key: string]: any;
}

type ConnectionStatus = 'untested' | 'valid' | 'invalid';

/**
 * UI-INV-001: Pure predicate to determine if the system is configured.
 * Gating logic depends solely on backend state (baseUrl presence).
 */
export const isConfigured = (config: AppConfig | null): boolean => {
  return !!config?.openWebIF?.baseUrl;
};

interface ConfigProps {
  onUpdate?: () => void;
  showTitle?: boolean;
  compact?: boolean;
}

function Config(props: ConfigProps = { showTitle: true }) {
  const cx = (...parts: Array<string | false | null | undefined>) => parts.filter(Boolean).join(' ');
  const { t } = useTranslation();
  // ... (rest of component remains same until return)
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string>('');
  const [configured, setConfigured] = useState<boolean>(false);

  // New: Restarting State
  const [restarting, setRestarting] = useState<boolean>(false);

  // Smart Wizard State
  const [validating, setValidating] = useState<boolean>(false);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('untested');
  const [validationResult, setValidationResult] = useState<ValidationResponse | null>(null);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      setLoading(true);
      const result = await getSystemConfig();
      if (result.data) {
        const data = result.data;
        const normalized: AppConfig = {
          ...data,
          // Normalization for Settings list (NOT API DTO).
          // Legacy check in case backend sends raw strings for config values.
          bouquets: Array.isArray(data.bouquets) ? data.bouquets : []
        };
        setConfig(normalized);
        setConfigured(isConfigured(normalized));
        setError(null);
        // Reset validation state on load
        setConnectionStatus('untested');
        setValidationResult(null);
      } else if (result.error) {
        // @ts-ignore - 'status' might not be on generic error, but likely is
        if (result.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError(t('setup.errors.authRequired'));
        } else {
          setError(t('setup.errors.loadFailed'));
        }
      }
    } catch (err: any) {
      debugError('Failed to load config:', formatError(err));
      setError(t('setup.errors.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  const checkHealthAndReload = async () => {
    const pollInterval = setInterval(async () => {
      try {
        const res = await fetch('/healthz');
        if (res.ok) {
          clearInterval(pollInterval);
          window.location.reload();
        }
      } catch (e) {
        // Still down, ignore
      }
    }, 1000);
  };

  const handleChange = (section: keyof AppConfig, field: string, value: any) => {
    if (!config) return;

    setConfig(prev => {
      if (!prev) return null;
      // Strict safe access
      const currentSection = prev[section];
      const sectionData = (currentSection && typeof currentSection === 'object') ? currentSection : {};

      return {
        ...prev,
        [section]: {
          ...sectionData,
          [field]: value
        }
      };
    });

    // Invalidate connection status when URL/Auth changes
    if (section === 'openWebIF' && (field === 'baseUrl' || field === 'username' || field === 'password')) {
      setConnectionStatus('untested');
    }
  };

  const validateConnection = async () => {
    if (!config) return;
    setValidating(true);
    setError(null);
    setSuccessMsg('');

    try {
      const token = getStoredToken();
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (token) {
        headers.Authorization = `Bearer ${token}`;
      }

      const response = await fetch('/internal/setup/validate', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          baseUrl: config.openWebIF?.baseUrl,
          username: config.openWebIF?.username,
          password: config.openWebIF?.password
        })
      });

      const result = await response.json();

      if (result.valid) {
        setConnectionStatus('valid');
        setValidationResult(result);
        const receiver = [result.version?.info?.brand, result.version?.info?.model]
          .filter(Boolean)
          .join(' ')
          .trim() || t('setup.receiver.unknown');
        setSuccessMsg(t('setup.messages.connected', { receiver }));

        // Auto-populate bouquets if empty
        if ((!config.bouquets || config.bouquets.length === 0) && result.bouquets?.length > 0) {
          // Optional: Auto-select favorites or first bouquet? 
          // For now, let user choose, but show them available.
        }
      } else {
        setConnectionStatus('invalid');
        setError(result.message || t('setup.errors.connectionFailed'));
      }
    } catch (err) {
      debugError('Validation failed:', formatError(err));
      setConnectionStatus('invalid');
      setError(t('setup.errors.networkValidation'));
    } finally {
      setValidating(false);
    }
  };

  const toggleBouquet = (bouquet: string) => {
    if (!config) return;
    const current = config.bouquets || [];
    const exists = current.includes(bouquet);
    let updated: string[];
    if (exists) {
      updated = current.filter(b => b !== bouquet);
    } else {
      updated = [...current, bouquet];
    }
    setConfig(prev => prev ? ({ ...prev, bouquets: updated }) : null);
  };

  const handleSave = async (e: FormEvent) => {
    e.preventDefault();
    if (!config) return;

    if (connectionStatus !== 'valid') {
      setError(t('setup.errors.validateBeforeSave'));
      return;
    }

    setSaving(true);
    setError(null);
    setSuccessMsg('');

    try {
      // UI-INV-002: Construct payload purely from backend state + user edits.
      // No synthesized UI-local defaults (e.g. no streamPort: 8001 fallback).
      // Conditional spreads ensure true field omission (no undefined keys in JSON).
      const payload: ConfigUpdate = {
        ...(config.openWebIF ? { openWebIF: config.openWebIF } : {}),
        ...(config.bouquets ? { bouquets: config.bouquets } : {}),
        ...(config.epg ? { epg: config.epg } : {}),
        ...(config.picons !== undefined ? { picons: config.picons } : {}),
        ...(config.featureFlags ? { featureFlags: config.featureFlags } : {})
      };

      const result = await putSystemConfig({ body: payload });

      if (result.data) {
        if (result.data.restartRequired) {
          setSuccessMsg(t('setup.messages.savedRestart'));
          setRestarting(true);
          checkHealthAndReload();
        } else {
          setSuccessMsg(t('setup.messages.saved'));
          // UI-INV-003: re-fetch from backend and re-render the absolute truth
          await loadConfig();
          // Notify parent of update if callback provided
          if (props.onUpdate) props.onUpdate();
        }
      } else {
        setError(t('setup.errors.saveFailed'));
      }

    } catch (err) {
      debugError('Failed to save config:', formatError(err));
      setError(t('setup.errors.saveFailedLogs'));
    } finally {
      setSaving(false);
    }
  };

  if (restarting) {
    return (
      <div className={cx(styles.container, styles.restartingOverlay)}>
        <div className="loading-spinner"></div>
        <h2>{t('setup.restart.title')}</h2>
        <p>{t('setup.restart.subtitle')}</p>
      </div>
    );
  }

  if (loading) return <div className={styles.loading}>{t('setup.loading')}</div>;
  if (!config) return <div className={styles.error}>{t('setup.loadError')}</div>;

  const availableBouquets = validationResult?.bouquets || [];
  const selectedBouquets = config.bouquets || [];

  return (
    <div
      className={cx(styles.container, props.compact ? styles.containerCompact : undefined)}
      data-testid={configured ? "config-settings" : "config-wizard"}
    >
      {props.showTitle && <h2>{configured ? t('nav.config') : t('setup.title')}</h2>}

      {error && <div className={cx(styles.alert, styles.alertError)}>{error}</div>}
      {successMsg && !restarting && <div className={cx(styles.alert, styles.alertSuccess)}>{successMsg}</div>}

      <form onSubmit={handleSave}>
        <section className={styles.section}>
          <h3>{t('setup.step1.title')}</h3>
          <p className={styles.hintText}>{t('setup.step1.hint')}</p>

          <div className={styles.formGroup}>
            <label>{t('setup.fields.receiverUrl')}</label>
            <div className={styles.inputWithButton}>
              <input
                type="text"
                value={config.openWebIF?.baseUrl || ''}
                onChange={e => handleChange('openWebIF', 'baseUrl', e.target.value)}
                placeholder="http://192.168.1.50"
                className={connectionStatus === 'valid' ? styles.validInput : undefined}
              />
            </div>
          </div>

          <div className={styles.formRow}>
            <div className={styles.formGroup}>
              <label>{t('setup.fields.username')}</label>
              <input
                type="text"
                value={config.openWebIF?.username || ''}
                onChange={e => handleChange('openWebIF', 'username', e.target.value)}
                placeholder={t('setup.placeholders.username')}
              />
            </div>
            <div className={styles.formGroup}>
              <label>{t('setup.fields.password')}</label>
              <input
                type="password"
                value={config.openWebIF?.password || ''}
                onChange={e => handleChange('openWebIF', 'password', e.target.value)}
                placeholder={t('setup.placeholders.password')}
              />
            </div>
          </div>

          <div className={styles.validationActions}>
            <Button
              variant="secondary"
              state={connectionStatus}
              onClick={validateConnection}
              disabled={validating || !config.openWebIF?.baseUrl}
              data-testid="config-validate"
            >
              {validating
                ? t('setup.actions.connecting')
                : (connectionStatus === 'valid' ? `✓ ${t('setup.actions.connectionVerified')}` : t('setup.actions.testConnection'))}
            </Button>
            {connectionStatus === 'valid' && (
              <StatusChip state="success" label={t('setup.actions.statusOnline')} />
            )}
          </div>
        </section>

        {connectionStatus === 'valid' && (
          <section className={cx(styles.section, 'animate-enter')}>
            <h3>{t('setup.step2.title')}</h3>
            <p className={styles.hintText}>{t('setup.step2.hint')}</p>

            <div className={styles.bouquetsGrid}>
              {availableBouquets.length > 0 ? availableBouquets.map(b => (
                <label
                  key={b}
                  className={cx(styles.bouquetCard, selectedBouquets.includes(b) ? styles.bouquetCardSelected : undefined)}
                >
                  <input
                    type="checkbox"
                    checked={selectedBouquets.includes(b)}
                    onChange={() => toggleBouquet(b)}
                  />
                  <span>{b}</span>
                </label>
              )) : (
                <div className={styles.emptyBouquets}>
                  {t('setup.step2.empty')} <small>{t('setup.step2.emptyHint')}</small>
                </div>
              )}
            </div>
          </section>
        )}

        <section className={styles.section}>
          <h3>{t('setup.step3.title')}</h3>
          <div className={cx(styles.formGroup, styles.checkboxGroup)}>
            <label>
              <input
                type="checkbox"
                checked={config.epg?.enabled || false}
                onChange={e => handleChange('epg', 'enabled', e.target.checked)}
              />
              {t('setup.step3.enableEpg')}
            </label>
            <small className={cx(styles.hintInline, styles.epgHintText)}>
              {t('setup.step3.hint')}
            </small>
          </div>
        </section>

        <div className={cx(styles.formActions, styles.stickyFooter)}>
          <Button
            type="submit"
            disabled={saving || connectionStatus !== 'valid'}
            data-testid="config-save"
          >
            {saving
              ? t('common.loading')
              : (configured ? t('setup.actions.saveConfig') : t('setup.actions.finishSetup'))}
          </Button>
        </div>
      </form>
    </div>
  );
}

export default Config;
