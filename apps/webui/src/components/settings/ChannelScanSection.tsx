// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useTranslation } from 'react-i18next';
import { Button } from '../ui';
import styles from '../Settings.module.css';

interface ChannelScanSectionProps {
  scanStatus: {
    state?: string;
    startedAt?: number;
    scannedChannels?: number;
    totalChannels?: number;
    updatedCount?: number;
    finishedAt?: number;
  } | null;
  scanStatusErrorMessage: string | null;
  triggerScanPending: boolean;
  handleStartScan: () => Promise<void>;
}

export default function ChannelScanSection({
  scanStatus,
  scanStatusErrorMessage,
  triggerScanPending,
  handleStartScan,
}: ChannelScanSectionProps) {
  const { t } = useTranslation();

  return (
    <div className={styles.section}>
      <h2>{t('settings.streaming.scan.title')}</h2>
      <p className={styles.subtitle}>{t('settings.streaming.scan.description')}</p>

      <div className={styles.group}>
        <div className={styles.scanControls}>
          <Button
            onClick={() => { void handleStartScan(); }}
            disabled={scanStatus?.state === 'running' || triggerScanPending}
          >
            {scanStatus?.state === 'running' || triggerScanPending
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
  );
}
