// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemScanStatus, triggerSystemScan, getSystemConfig } from '../client-ts';
import type { ScanStatus, AppConfig } from '../client-ts';
import Config, { isConfigured } from './Config';
import { debugError, formatError } from '../utils/logging';
import { Button } from './ui';
import styles from './Settings.module.css';

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
    <div className={`${styles.page} animate-enter`.trim()}>
      <div className={styles.header}>
        <div>
          <p className={styles.kicker}>{t('settings.kicker')}</p>
          <h2>{t('settings.title')}</h2>
          <p className={styles.subtitle}>
            {t('settings.subtitle')}
          </p>
        </div>
      </div>

      <div className={styles.setup}>
        {!configured ? (
          <Config onUpdate={fetchConfig} />
        ) : (
          <div className={styles.section}>
            <div className={styles.accordionHeader}>
              <h3>{t('setup.title')}</h3>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setShowSetup(v => !v)}
                data-testid="config-rerun-setup"
                aria-expanded={showSetup}
                aria-controls="settings-setup-details"
              >
                {showSetup ? t('common.hideDetails') : t('setup.actions.rerunSetup') || 'Re-run Setup'}
              </Button>
            </div>
            {showSetup && (
              <div id="settings-setup-details" className="animate-enter">
                <Config onUpdate={fetchConfig} showTitle={false} compact />
              </div>
            )}
          </div>
        )}
      </div>

      <div className={styles.section}>
        <h3>{t('settings.scan.title')}</h3>
        <p className={styles.subtitle}>{t('settings.scan.description')}</p>

        <div className={styles.group}>
          <div className={styles.scanControls}>
            <Button
              onClick={handleStartScan}
              disabled={scanStatus?.state === 'running'}
            >
              {scanStatus?.state === 'running' ? t('settings.scan.status.running') : t('settings.scan.start')}
            </Button>
            {scanError && <span className={styles.errorInline}>{scanError}</span>}
          </div>

          {scanStatus && (
            <div className={styles.scanCard} data-state={scanStatus.state || undefined}>
              <div className={styles.scanHeader}>
                <div className={styles.scanBadge}>
                  <span className={styles.statusDot} data-state={scanStatus.state || undefined}></span>
                  <span className={styles.statusText}>{t(`settings.scan.status.${scanStatus.state || 'idle'}`)}</span>
                </div>
                {scanStatus.startedAt && scanStatus.startedAt > 0 && (
                  <div className={styles.scanTime}>
                    {new Date(scanStatus.startedAt * 1000).toLocaleTimeString()}
                  </div>
                )}
              </div>

              <div className={styles.progressContainer}>
                <div
                  className={styles.progressBar}
                  style={{
                    width: `${Math.min(100, Math.max(0, ((scanStatus.scannedChannels || 0) / (scanStatus.totalChannels || 1)) * 100))}%`
                  }}
                />
              </div>

              <div className={styles.statsRow}>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.scannedChannels} / {scanStatus.totalChannels}</span>
                  <span className={styles.statLabel}>{t('settings.scan.stats.scanned')}</span>
                </div>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.updatedCount}</span>
                  <span className={styles.statLabel}>{t('settings.scan.stats.updated')}</span>
                </div>
                {scanStatus.finishedAt && scanStatus.finishedAt > 0 && (
                  <div className={styles.statItem}>
                    <span className={styles.statValue}>{new Date(scanStatus.finishedAt * 1000).toLocaleTimeString()}</span>
                    <span className={styles.statLabel}>Finished</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </div>

      <div className={styles.section}>
        <h3>{t('settings.streaming.title')}</h3>

        {/* Note: Profile selection removed in favor of Universal Policy */}
        <div className={styles.group}>
          <label>{t('settings.streaming.policy') || 'Delivery Policy'}</label>
          <div className={styles.inputRow}>
            <input
              type="text"
              value={config?.streaming?.deliveryPolicy === 'universal' ? "Universal (H.264/AAC/fMP4)" : (config?.streaming?.deliveryPolicy || "Loading...")}
              disabled
              className={styles.inputReadonly}
            />
            <span className={styles.hint}>Strict Universal-Only</span>
          </div>
        </div>
      </div>

      {/* Adaptive Bitrate removed as per 2026 Design Contract (Trust Hardening) */}

      {/* ADR-00X: Saved message removed (was for profile save feedback) */}


      <div className={styles.footer}>
        <p>
          <strong>{t('settings.footer.noteTitle')}</strong> {t('settings.footer.noteBody')}
        </p>
      </div>
    </div>
  );
}

export default Settings;
