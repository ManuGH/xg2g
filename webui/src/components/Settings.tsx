// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemScanStatus, triggerSystemScan, getSystemConfig } from '../client-ts';
import type { ScanStatus, AppConfig } from '../client-ts/types.gen';
import Config, { isConfigured } from './Config';
import { debugError, formatError } from '../utils/logging';
import './Settings.css';

function Settings() {
  const { t } = useTranslation();
  // ADR-00X: Profile selection removed (universal policy only)

  // ADR-00X: Unused savedMessage state removed (was for profile save feedback)
  const [scanStatus, setScanStatus] = useState<ScanStatus | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [showSetup, setShowSetup] = useState<boolean>(false);

  const configured = isConfigured(config);

  const fetchConfig = async () => {
    try {
      const { data } = await getSystemConfig();
      if (data) setConfig(data);
    } catch (err) {
      debugError('Failed to load config', formatError(err));
    }
  };

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

    fetchConfig();

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
      debugError('Failed to start scan', formatError(err));
      setScanError("Failed to start scan");
    }
  };

  // ADR-00X: Profile persistence removed (universal policy only)

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
        {!configured ? (
          <Config onUpdate={fetchConfig} />
        ) : (
          <div className="settings-section accordion-section">
            <div className="section-header-row" onClick={() => setShowSetup(!showSetup)}>
              <h3>{t('setup.title')}</h3>
              <button
                className="settings-button secondary small"
                data-testid="config-rerun-setup"
              >
                {showSetup ? t('common.hideDetails') : t('setup.actions.rerunSetup') || 'Re-run Setup'}
              </button>
            </div>
            {showSetup && (
              <div className="animate-fade-in">
                <Config onUpdate={fetchConfig} showTitle={false} />
              </div>
            )}
          </div>
        )}
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
                {scanStatus.startedAt && scanStatus.startedAt > 0 && (
                  <div className="scan-time">
                    {new Date(scanStatus.startedAt * 1000).toLocaleTimeString()}
                  </div>
                )}
              </div>

              <div className="scan-progress-container">
                <div
                  className="scan-progress-bar"
                  style={{
                    width: `${Math.min(100, Math.max(0, ((scanStatus.scannedChannels || 0) / (scanStatus.totalChannels || 1)) * 100))}%`
                  }}
                />
              </div>

              <div className="scan-stats-row">
                <div className="scan-stat-item">
                  <span className="stat-value tabular">{scanStatus.scannedChannels} / {scanStatus.totalChannels}</span>
                  <span className="stat-label">{t('settings.scan.stats.scanned')}</span>
                </div>
                <div className="scan-stat-item">
                  <span className="stat-value tabular">{scanStatus.updatedCount}</span>
                  <span className="stat-label">{t('settings.scan.stats.updated')}</span>
                </div>
                {scanStatus.finishedAt && scanStatus.finishedAt > 0 && (
                  <div className="scan-stat-item">
                    <span className="stat-value">{new Date(scanStatus.finishedAt * 1000).toLocaleTimeString()}</span>
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

        {/* Note: Profile selection removed in favor of Universal Policy */}
        <div className="settings-group">
          <label>{t('settings.streaming.policy') || 'Delivery Policy'}</label>
          <div className="input-with-button">
            <input
              type="text"
              value={config?.streaming?.deliveryPolicy === 'universal' ? "Universal (H.264/AAC/fMP4)" : (config?.streaming?.deliveryPolicy || "Loading...")}
              disabled
              className="input-readonly"
            />
            <span className="settings-hint">Strict Universal-Only</span>
          </div>
        </div>
      </div>

      {/* Adaptive Bitrate removed as per 2026 Design Contract (Trust Hardening) */}

      {/* ADR-00X: Saved message removed (was for profile save feedback) */}


      <div className="settings-footer">
        <p>
          <strong>{t('settings.footer.noteTitle')}</strong> {t('settings.footer.noteBody')}
        </p>
      </div>
    </div>
  );
}

export default Settings;
