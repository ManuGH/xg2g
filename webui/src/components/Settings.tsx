// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemScanStatus, triggerSystemScan } from '../client-ts';
import type { ScanStatus } from '../client-ts/types.gen';
import Config from './Config';
import './Settings.css';

function Settings() {
  const { t } = useTranslation();
  const [streamProfile, setStreamProfile] = useState<string>(() => {
    return localStorage.getItem('xg2g_stream_profile') || 'auto';
  });

  const [savedMessage, setSavedMessage] = useState<string>('');
  const [scanStatus, setScanStatus] = useState<ScanStatus | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);

  // Poll scan status
  useEffect(() => {
    let intervalId: number | undefined;

    const fetchStatus = async () => {
      try {
        const { data } = await getSystemScanStatus();
        if (data) {
          setScanStatus(data);
          // We keep polling even in terminal states to catch re-scans or external triggers
        }
      } catch (err) {
        // Only set error if we don't have stale data to show
        if (!scanStatus) {
          setScanError("Failed to load status");
        }
      }
    };

    fetchStatus();
    intervalId = setInterval(fetchStatus, 2000) as unknown as number;
    return () => clearInterval(intervalId);
  }, []); // Eslint: we want this to run once on mount, but technically we might want to restart polling on manual trigger.

  const handleStartScan = async () => {
    setScanError(null);
    try {
      await triggerSystemScan();
      // Force immediate update and restart polling logic if needed (simple re-fetch here)
      const { data } = await getSystemScanStatus();
      if (data) setScanStatus(data);
    } catch (err) {
      console.error("Failed to start scan", err);
      setScanError("Failed to start scan");
    }
  };

  useEffect(() => {
    localStorage.setItem('xg2g_stream_profile', streamProfile);
    setSavedMessage(t('settings.saved'));
    const timer = setTimeout(() => setSavedMessage(''), 2000);
    return () => clearTimeout(timer);
  }, [streamProfile, t]);

  return (
    <div className="settings-page">
      <div className="settings-header">
        <div>
          <p className="settings-kicker">{t('settings.kicker')}</p>
          <h2>{t('settings.title')}</h2>
          <p className="settings-subtitle">
            {t('settings.subtitle')}
          </p>
        </div>
      </div>

      <div className="settings-setup">
        <Config />
      </div>

      <div className="settings-section">
        <h3>{t('settings.scan.title')}</h3>
        <p className="settings-subtitle">{t('settings.scan.description')}</p>

        <div className="settings-group">
          <div className="scan-controls">
            <button
              className="settings-button primary"
              onClick={handleStartScan}
              disabled={scanStatus?.state === 'running'}
            >
              {scanStatus?.state === 'running' ? t('settings.scan.status.running') : t('settings.scan.start')}
            </button>
            {scanError && <span className="settings-error-inline">{scanError}</span>}
          </div>

          {scanStatus && (
            <div className={`scan-card ${scanStatus.state}`}>
              <div className="scan-header">
                <div className="scan-status-badge">
                  <span className={`status-dot ${scanStatus.state}`}></span>
                  <span className="status-text">{t(`settings.scan.status.${scanStatus.state || 'idle'}`)}</span>
                </div>
                {scanStatus.started_at && scanStatus.started_at > 0 && (
                  <div className="scan-time">
                    {new Date(scanStatus.started_at * 1000).toLocaleTimeString()}
                  </div>
                )}
              </div>

              <div className="scan-progress-container">
                <div
                  className="scan-progress-bar"
                  style={{
                    width: `${Math.min(100, Math.max(0, ((scanStatus.scanned_channels || 0) / (scanStatus.total_channels || 1)) * 100))}%`
                  }}
                />
              </div>

              <div className="scan-stats-row">
                <div className="scan-stat-item">
                  <span className="stat-value">{scanStatus.scanned_channels} / {scanStatus.total_channels}</span>
                  <span className="stat-label">{t('settings.scan.stats.scanned')}</span>
                </div>
                <div className="scan-stat-item">
                  <span className="stat-value">{scanStatus.updated_count}</span>
                  <span className="stat-label">{t('settings.scan.stats.updated')}</span>
                </div>
                {scanStatus.finished_at && scanStatus.finished_at > 0 && (
                  <div className="scan-stat-item">
                    <span className="stat-value">{new Date(scanStatus.finished_at * 1000).toLocaleTimeString()}</span>
                    <span className="stat-label">Finished</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      <div className="settings-section">
        <h3>{t('settings.streaming.title')}</h3>

        <div className="settings-group">
          <label htmlFor="stream-profile">
            <strong>{t('settings.streaming.profile.label')}</strong>
            <span className="settings-hint">
              {t('settings.streaming.profile.hint')}
            </span>
          </label>
          <select
            id="stream-profile"
            value={streamProfile}
            onChange={(e) => setStreamProfile(e.target.value)}
            className="settings-select"
          >
            <option value="auto">{t('settings.streaming.profile.options.auto')}</option>
            <option value="safari">{t('settings.streaming.profile.options.safari')}</option>
            <option value="safari_hevc_hw">{t('settings.streaming.profile.options.safariHevcGpu')}</option>
          </select>

          {streamProfile === 'safari_hevc_hw' && (
            <div className="settings-info">
              <strong>{t('settings.streaming.info.gpu.title')}</strong>
              <p>{t('settings.streaming.info.gpu.body')}</p>
            </div>
          )}

          {streamProfile === 'auto' && (
            <div className="settings-info">
              <p>{t('settings.streaming.info.auto')}</p>
            </div>
          )}
        </div>
      </div>

      <div className="settings-section settings-section-disabled">
        <h3>{t('settings.streaming.adaptive.title')}</h3>
        <div className="settings-group">
          <label className="settings-inline">
            <input type="checkbox" disabled />
            {t('settings.streaming.adaptive.toggle')}
          </label>
          <span className="settings-hint">
            {t('settings.streaming.adaptive.hint')}
          </span>
        </div>
      </div>

      {savedMessage && (
        <div className="settings-saved-message">
          ✓ {savedMessage}
        </div>
      )}

      <div className="settings-footer">
        <p>
          <strong>{t('settings.footer.noteTitle')}</strong> {t('settings.footer.noteBody')}
        </p>
      </div>
    </div>
  );
}

export default Settings;
