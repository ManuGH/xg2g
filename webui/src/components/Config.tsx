// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect, FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemConfig, putSystemConfig, type AppConfig, type ConfigUpdate } from '../client-ts';
import './Config.css';

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
}

function Config(props: ConfigProps = { showTitle: true }) {
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
      console.error('Failed to load config:', err);
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
      const token = localStorage.getItem('XG2G_API_TOKEN');
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
      console.error('Validation failed:', err);
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
      console.error('Failed to save config:', err);
      setError(t('setup.errors.saveFailedLogs'));
    } finally {
      setSaving(false);
    }
  };

  if (restarting) {
    return (
      <div className="config-container restarting-overlay">
        <div className="loading-spinner"></div>
        <h2>{t('setup.restart.title')}</h2>
        <p>{t('setup.restart.subtitle')}</p>
      </div>
    );
  }

  if (loading) return <div className="loading">{t('setup.loading')}</div>;
  if (!config) return <div className="error">{t('setup.loadError')}</div>;

  const availableBouquets = validationResult?.bouquets || [];
  const selectedBouquets = config.bouquets || [];

  return (
    <div
      className="config-container"
      data-testid={configured ? "config-settings" : "config-wizard"}
    >
      {props.showTitle && <h2>{configured ? t('nav.config') : t('setup.title')}</h2>}

      {error && <div className="alert error">{error}</div>}
      {successMsg && !restarting && <div className="alert success">{successMsg}</div>}

      <form onSubmit={handleSave} className="config-form">
        <section className="config-section">
          <h3>{t('setup.step1.title')}</h3>
          <p className="hint-text">{t('setup.step1.hint')}</p>

          <div className="form-group">
            <label>{t('setup.fields.receiverUrl')}</label>
            <div className="input-with-button">
              <input
                type="text"
                value={config.openWebIF?.baseUrl || ''}
                onChange={e => handleChange('openWebIF', 'baseUrl', e.target.value)}
                placeholder="http://192.168.1.50"
                className={connectionStatus === 'valid' ? 'valid-input' : ''}
              />
            </div>
          </div>

          <div className="form-row">
            <div className="form-group">
              <label>{t('setup.fields.username')}</label>
              <input
                type="text"
                value={config.openWebIF?.username || ''}
                onChange={e => handleChange('openWebIF', 'username', e.target.value)}
                placeholder={t('setup.placeholders.username')}
              />
            </div>
            <div className="form-group">
              <label>{t('setup.fields.password')}</label>
              <input
                type="password"
                value={config.openWebIF?.password || ''}
                onChange={e => handleChange('openWebIF', 'password', e.target.value)}
                placeholder={t('setup.placeholders.password')}
              />
            </div>
          </div>

          <div className="validation-actions">
            <button
              type="button"
              className={`btn-secondary ${connectionStatus}`}
              onClick={validateConnection}
              disabled={validating || !config.openWebIF?.baseUrl}
              data-testid="config-validate"
            >
              {validating
                ? t('setup.actions.connecting')
                : (connectionStatus === 'valid' ? `✓ ${t('setup.actions.connectionVerified')}` : t('setup.actions.testConnection'))}
            </button>
            {connectionStatus === 'valid' && <span className="status-badge success">{t('setup.actions.statusOnline')}</span>}
          </div>
        </section>

        {connectionStatus === 'valid' && (
          <section className="config-section animate-fade-in">
            <h3>{t('setup.step2.title')}</h3>
            <p className="hint-text">{t('setup.step2.hint')}</p>

            <div className="bouquets-grid">
              {availableBouquets.length > 0 ? availableBouquets.map(b => (
                <label key={b} className={`bouquet-card ${selectedBouquets.includes(b) ? 'selected' : ''}`}>
                  <input
                    type="checkbox"
                    checked={selectedBouquets.includes(b)}
                    onChange={() => toggleBouquet(b)}
                  />
                  <span>{b}</span>
                </label>
              )) : (
                <div className="empty-bouquets">
                  {t('setup.step2.empty')} <small>{t('setup.step2.emptyHint')}</small>
                </div>
              )}
            </div>
          </section>
        )}

        <section className="config-section">
          <h3>{t('setup.step3.title')}</h3>
          <div className="form-group checkbox-group">
            <label>
              <input
                type="checkbox"
                checked={config.epg?.enabled || false}
                onChange={e => handleChange('epg', 'enabled', e.target.checked)}
              />
              {t('setup.step3.enableEpg')}
            </label>
            <small className="hint-text" style={{ marginTop: '4px', marginLeft: '24px' }}>
              {t('setup.step3.hint')}
            </small>
          </div>
        </section>

        <div className="form-actions sticky-footer">
          <button
            type="submit"
            disabled={saving || connectionStatus !== 'valid'}
            className="btn-primary"
            data-testid="config-save"
          >
            {saving
              ? t('common.loading')
              : (configured ? t('setup.actions.saveConfig') : t('setup.actions.finishSetup'))}
          </button>
        </div>
      </form>
    </div>
  );
}

export default Config;
