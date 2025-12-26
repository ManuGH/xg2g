// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { getSystemConfig, putSystemConfig } from '../client-ts';
import './Config.css';

function Config() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [successMsg, setSuccessMsg] = useState('');

  // New: Restarting State
  const [restarting, setRestarting] = useState(false);

  // Smart Wizard State
  const [validating, setValidating] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState('untested'); // untested, valid, invalid
  const [validationResult, setValidationResult] = useState(null); // { version, bouquets }

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      setLoading(true);
      const data = await getSystemConfig();
      const normalized = {
        ...data,
        bouquets: Array.isArray(data.bouquets) ? data.bouquets : []
      };
      setConfig(normalized);
      setError(null);
      // Reset validation state on load
      setConnectionStatus('untested');
      setValidationResult(null);
    } catch (err) {
      console.error('Failed to load config:', err);
      if (err.status === 401) {
        window.dispatchEvent(new Event('auth-required'));
        setError('Authentication required. Please enter your API token.');
      } else {
        setError('Failed to load configuration. Please ensure the backend is running.');
      }
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

  const handleChange = (section, field, value) => {
    setConfig(prev => ({
      ...prev,
      [section]: {
        ...prev[section],
        [field]: value
      }
    }));
    // Invalidate connection status when URL/Auth changes
    if (section === 'openWebIF' && (field === 'baseUrl' || field === 'username' || field === 'password')) {
      setConnectionStatus('untested');
    }
  };

  const validateConnection = async () => {
    setValidating(true);
    setError(null);
    setSuccessMsg('');

    try {
      const response = await fetch('/api/v3/setup/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
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
        setSuccessMsg(`Connected successfully! Receiver: ${result.version?.info?.brand || 'Unknown'} ${result.version?.info?.model || ''}`);

        // Auto-populate bouquets if empty
        if ((!config.bouquets || config.bouquets.length === 0) && result.bouquets?.length > 0) {
          // Optional: Auto-select favorites or first bouquet? 
          // For now, let user choose, but show them available.
        }
      } else {
        setConnectionStatus('invalid');
        setError(result.message || 'Connection failed');
      }
    } catch (err) {
      console.error('Validation failed:', err);
      setConnectionStatus('invalid');
      setError('Network error during validation');
    } finally {
      setValidating(false);
    }
  };

  const toggleBouquet = (bouquet) => {
    const current = config.bouquets || [];
    const exists = current.includes(bouquet);
    let updated;
    if (exists) {
      updated = current.filter(b => b !== bouquet);
    } else {
      updated = [...current, bouquet];
    }
    setConfig(prev => ({ ...prev, bouquets: updated }));
  };

  const handleSave = async (e) => {
    e.preventDefault();

    if (connectionStatus !== 'valid') {
      setError("Please validate the connection before saving.");
      return;
    }

    setSaving(true);
    setError(null);
    setSuccessMsg('');

    try {
      // Smart Defaults for missing advanced fields
      const payload = {
        openWebIF: {
          ...config.openWebIF,
          streamPort: config.openWebIF?.streamPort || 8001 // Default to 8001 if missing
        },
        bouquets: config.bouquets,
        epg: {
          enabled: config.epg?.enabled || false,
          days: 3, // Hardcoded default
          source: 'bouquet' // Hardcoded default optimization
        },
        // picons: Omitted (backend handles default)
        featureFlags: config.featureFlags
      };

      const result = await putSystemConfig({ body: payload });

      if (result.restart_required) {
        setSuccessMsg('Configuration saved. Restarting application...');
        setRestarting(true);
        checkHealthAndReload();
      } else {
        setSuccessMsg('Configuration saved successfully.');
        // Reload to ensure we have the latest state
        await loadConfig();
      }

    } catch (err) {
      console.error('Failed to save config:', err);
      setError('Failed to save configuration. Please check the logs.');
    } finally {
      setSaving(false);
    }
  };

  if (restarting) {
    return (
      <div className="config-container restarting-overlay">
        <div className="loading-spinner"></div>
        <h2>Restarting Application...</h2>
        <p>Please wait while your changes are applied.</p>
      </div>
    );
  }

  if (loading) return <div className="loading">Loading configuration...</div>;
  if (!config) return <div className="error">Could not load configuration</div>;

  const availableBouquets = validationResult?.bouquets || [];
  const selectedBouquets = config.bouquets || [];

  return (
    <div className="config-container">
      <h2>Initial Setup Wizard</h2>

      {error && <div className="alert error">{error}</div>}
      {successMsg && !restarting && <div className="alert success">{successMsg}</div>}

      <form onSubmit={handleSave} className="config-form">
        <section className="config-section">
          <h3>Step 1: Connect to Receiver</h3>
          <p className="hint-text">Enter the IP address of your Enigma2 receiver (e.g., Dreambox, VU+).</p>

          <div className="form-group">
            <label>Receiver URL</label>
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
              <label>Username</label>
              <input
                type="text"
                value={config.openWebIF?.username || ''}
                onChange={e => handleChange('openWebIF', 'username', e.target.value)}
                placeholder="root (optional)"
              />
            </div>
            <div className="form-group">
              <label>Password</label>
              <input
                type="password"
                value={config.openWebIF?.password || ''}
                onChange={e => handleChange('openWebIF', 'password', e.target.value)}
                placeholder="password (optional)"
              />
            </div>
          </div>

          <div className="validation-actions">
            <button
              type="button"
              className={`btn-secondary ${connectionStatus}`}
              onClick={validateConnection}
              disabled={validating || !config.openWebIF?.baseUrl}
            >
              {validating ? 'Connecting...' : (connectionStatus === 'valid' ? '✓ Connection Verified' : 'Test Connection')}
            </button>
            {connectionStatus === 'valid' && <span className="status-badge success">Online</span>}
          </div>
        </section>

        {connectionStatus === 'valid' && (
          <section className="config-section animate-fade-in">
            <h3>Step 2: Select Channels</h3>
            <p className="hint-text">Choose which bouquets (favorites) you want to use.</p>

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
                  No bouquets found. <small>Check your receiver configuration.</small>
                </div>
              )}
            </div>
          </section>
        )}

        <section className="config-section">
          <h3>Step 3: Options</h3>
          <div className="form-group checkbox-group">
            <label>
              <input
                type="checkbox"
                checked={config.epg?.enabled || false}
                onChange={e => handleChange('epg', 'enabled', e.target.checked)}
              />
              Enable EPG (Electronic Program Guide)
            </label>
            <small className="hint-text" style={{ marginTop: '4px', marginLeft: '24px' }}>
              Automatically fetches program info for selected channels (3 days).
            </small>
          </div>
        </section>

        <div className="form-actions sticky-footer">
          <button
            type="submit"
            disabled={saving || connectionStatus !== 'valid'}
            className="btn-primary"
          >
            {saving ? 'Finish Setup' : 'Finish Setup'}
          </button>
        </div>
      </form>
    </div>
  );
}

export default Config;
