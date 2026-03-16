// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import Config, { isConfigured } from './Config';
import {
  useSystemConfig,
  useSystemScanStatus,
  useTriggerSystemScanMutation,
} from '../hooks/useServerQueries';
import { debugError, formatError } from '../utils/logging';
import { Button } from './ui';
import styles from './Settings.module.css';

function Settings() {
  const { t } = useTranslation();
  // ADR-00X: Profile selection removed (universal policy only)

  // ADR-00X: Unused savedMessage state removed (was for profile save feedback)
  const [scanError, setScanError] = useState<string | null>(null);
  const [showSetup, setShowSetup] = useState<boolean>(false);
  const {
    data: config = null,
    refetch: refetchConfig,
  } = useSystemConfig();
  const {
    data: scanStatus = null,
    error: scanStatusError,
    refetch: refetchScanStatus,
  } = useSystemScanStatus();
  const triggerScanMutation = useTriggerSystemScanMutation();

  const configured = isConfigured(config);
  const scanStatusErrorMessage = !scanStatus
    ? scanError ?? (
      scanStatusError instanceof Error
        ? scanStatusError.message
        : scanStatusError
          ? t('settings.streaming.scan.errors.loadStatus')
          : null
    )
    : scanError;

  const handleStartScan = async () => {
    setScanError(null);
    try {
      await triggerScanMutation.mutateAsync();
      await refetchScanStatus();
    } catch (err) {
      debugError('Failed to start scan', formatError(err));
      setScanError(err instanceof Error ? err.message : t('settings.streaming.scan.errors.start'));
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
          <Config onUpdate={() => { void refetchConfig(); }} />
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
                <Config onUpdate={() => { void refetchConfig(); }} showTitle={false} compact />
              </div>
            )}
          </div>
        )}
      </div>

      <div className={styles.section}>
        <h3>{t('settings.streaming.scan.title')}</h3>
        <p className={styles.subtitle}>{t('settings.streaming.scan.description')}</p>

        <div className={styles.group}>
          <div className={styles.scanControls}>
            <Button
              onClick={handleStartScan}
              disabled={scanStatus?.state === 'running' || triggerScanMutation.isPending}
            >
              {scanStatus?.state === 'running' || triggerScanMutation.isPending
                ? t('settings.streaming.scan.status.running')
                : t('settings.streaming.scan.start')}
            </Button>
            {scanStatusErrorMessage && <span className={styles.errorInline}>{scanStatusErrorMessage}</span>}
          </div>

          {scanStatus && (
            <div className={styles.scanCard} data-state={scanStatus.state || undefined}>
              <div className={styles.scanHeader}>
                <div className={styles.scanBadge}>
                  <span className={styles.statusDot} data-state={scanStatus.state || undefined}></span>
                  <span className={styles.statusText}>{t(`settings.streaming.scan.status.${scanStatus.state || 'idle'}`)}</span>
                </div>
                {scanStatus.startedAt && scanStatus.startedAt > 0 && (
                  <div className={styles.scanTime}>
                    {new Date(scanStatus.startedAt * 1000).toLocaleTimeString()}
                  </div>
                )}
              </div>

              <div className={styles.progressContainer}>
                <svg
                  width="100%"
                  height="100%"
                  viewBox="0 0 100 6"
                  preserveAspectRatio="none"
                  role="img"
                  aria-label={t('settings.streaming.scan.stats.scanned')}
                >
                  <rect
                    x="0"
                    y="0"
                    width={Math.min(100, Math.max(0, ((scanStatus.scannedChannels || 0) / (scanStatus.totalChannels || 1)) * 100))}
                    height="6"
                    rx="3"
                    ry="3"
                    fill="var(--accent-action)"
                  />
                </svg>
              </div>

              <div className={styles.statsRow}>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.scannedChannels} / {scanStatus.totalChannels}</span>
                  <span className={styles.statLabel}>{t('settings.streaming.scan.stats.scanned')}</span>
                </div>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.updatedCount}</span>
                  <span className={styles.statLabel}>{t('settings.streaming.scan.stats.updated')}</span>
                </div>
                {scanStatus.finishedAt && scanStatus.finishedAt > 0 && (
                  <div className={styles.statItem}>
                    <span className={styles.statValue}>{new Date(scanStatus.finishedAt * 1000).toLocaleTimeString()}</span>
                    <span className={styles.statLabel}>{t('settings.streaming.scan.timestamps.finished')}</span>
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
          <label>{t('settings.streaming.policy.label')}</label>
          <div className={styles.inputRow}>
            <input
              type="text"
              value={
                config?.streaming?.deliveryPolicy === 'universal'
                  ? t('settings.streaming.policy.universal')
                  : (config?.streaming?.deliveryPolicy || t('common.loading'))
              }
              disabled
              className={styles.inputReadonly}
            />
            <span className={styles.hint}>{t('settings.streaming.policy.hint')}</span>
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
