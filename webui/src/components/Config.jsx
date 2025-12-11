import { useState, useEffect } from 'react';
import { DefaultService } from '../client';
import './Config.css';

function Config() {
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [successMsg, setSuccessMsg] = useState('');
  const [restartRequired, setRestartRequired] = useState(false);

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      setLoading(true);
      const data = await DefaultService.getSystemConfig();
      setConfig(data);
      setError(null);
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

  const handleChange = (section, field, value) => {
    setConfig(prev => ({
      ...prev,
      [section]: {
        ...prev[section],
        [field]: value
      }
    }));
  };

  const handleBouquetChange = (value) => {
    setConfig(prev => ({
      ...prev,
      bouquets: value.split(',').map(s => s.trim()).filter(s => s)
    }));
  };

  const handleSave = async (e) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSuccessMsg('');

    try {
      const result = await DefaultService.putSystemConfig({
        openWebIF: config.openWebIF,
        bouquets: config.bouquets,
        epg: config.epg,
        picons: config.picons,
        featureFlags: config.featureFlags
      });

      setSuccessMsg('Configuration saved successfully.');
      if (result.restart_required) {
        setRestartRequired(true);
      }

      // Reload to ensure we have the latest state
      await loadConfig();
    } catch (err) {
      console.error('Failed to save config:', err);
      setError('Failed to save configuration. Please check the logs.');
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <div className="loading">Loading configuration...</div>;
  if (!config) return <div className="error">Could not load configuration</div>;

  return (
    <div className="config-container">
      <h2>System Configuration</h2>

      {error && <div className="alert error">{error}</div>}
      {successMsg && <div className="alert success">{successMsg}</div>}
      {restartRequired && (
        <div className="alert warning">
          Changes saved! A restart is required for some settings to take effect.
        </div>
      )}

      <form onSubmit={handleSave} className="config-form">
        <section className="config-section">
          <h3>OpenWebIF Connection</h3>
          <div className="form-group">
            <label>Base URL</label>
            <input
              type="text"
              value={config.openWebIF?.baseUrl || ''}
              onChange={e => handleChange('openWebIF', 'baseUrl', e.target.value)}
              placeholder="http://192.168.1.x"
            />
            <small>Address of your Enigma2 receiver</small>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label>Username (Optional)</label>
              <input
                type="text"
                value={config.openWebIF?.username || ''}
                onChange={e => handleChange('openWebIF', 'username', e.target.value)}
              />
            </div>
            <div className="form-group">
              <label>Password (Optional)</label>
              <input
                type="password"
                value={config.openWebIF?.password || ''}
                onChange={e => handleChange('openWebIF', 'password', e.target.value)}
              />
            </div>
          </div>
          <div className="form-group">
            <label>Default Stream Port</label>
            <input
              type="number"
              value={config.openWebIF?.streamPort || 8001}
              onChange={e => handleChange('openWebIF', 'streamPort', parseInt(e.target.value))}
            />
            <small>Standard port (8001). Encrypted channels use 17999 automatically.</small>
          </div>
        </section>

        <section className="config-section">
          <h3>Bouquets</h3>
          <div className="form-group">
            <label>Active Bouquets</label>
            <input
              type="text"
              value={config.bouquets?.join(', ') || ''}
              onChange={e => handleBouquetChange(e.target.value)}
              placeholder="Favourites (TV), Movies"
            />
            <small>Comma-separated list of bouquet names to fetch</small>
          </div>
        </section>

        <section className="config-section">
          <h3>EPG Settings</h3>
          <div className="form-group checkbox-group">
            <label>
              <input
                type="checkbox"
                checked={config.epg?.enabled || false}
                onChange={e => handleChange('epg', 'enabled', e.target.checked)}
              />
              Enable EPG Fetching
            </label>
          </div>
          {config.epg?.enabled && (
            <div className="form-row">
              <div className="form-group">
                <label>Days to Fetch</label>
                <input
                  type="number"
                  min="1"
                  max="14"
                  value={config.epg?.days || 3}
                  onChange={e => handleChange('epg', 'days', parseInt(e.target.value))}
                />
              </div>
              <div className="form-group">
                <label>Source Strategy</label>
                <select
                  value={config.epg?.source || 'per-service'}
                  onChange={e => handleChange('epg', 'source', e.target.value)}
                >
                  <option value="per-service">Per Service (Better detail)</option>
                  <option value="bouquet">Bouquet (Faster)</option>
                </select>
              </div>
            </div>
          )}
        </section>

        <section className="config-section">
          <h3>Picons (Channel Logos)</h3>
          <div className="form-group">
            <label>External Picon Source (Optional)</label>
            <input
              type="text"
              value={config.picons?.baseUrl || ''}
              onChange={e => handleChange('picons', 'baseUrl', e.target.value)}
              placeholder="http://picons.example.com/"
            />
            <small>Leave blank to use picons from receiver. Enter URL for external source.</small>
          </div>
        </section>

        <section className="config-section">
          <h3>Feature Flags</h3>
          <div className="form-group checkbox-group">
            <label>
              <input
                type="checkbox"
                checked={config.featureFlags?.instantTune || false}
                onChange={e => handleChange('featureFlags', 'instantTune', e.target.checked)}
              />
              Enable Instant Tune (Experimental)
            </label>
            <small>Pre-buffers streams for faster channel switching.</small>
          </div>
        </section>

        <div className="form-actions">
          <button type="submit" disabled={saving} className="btn-primary">
            {saving ? 'Saving...' : 'Save Configuration'}
          </button>
        </div>
      </form>
    </div>
  );
}

export default Config;
