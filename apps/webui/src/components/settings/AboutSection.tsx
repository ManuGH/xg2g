// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useTranslation } from 'react-i18next';
import { Button } from '../ui';
import { useUiOverlay } from '../../context/UiOverlayContext';
import styles from '../Settings.module.css';

interface AboutSectionProps {
  serverConnected: boolean;
  onResetPreferences: () => void;
}

export default function AboutSection({
  serverConnected,
  onResetPreferences,
}: AboutSectionProps) {
  const { t } = useTranslation();
  const { confirm, toast } = useUiOverlay();

  return (
    <div className={styles.section}>
      <h2>{t('settings.about.title')}</h2>
      <p className={styles.subtitle}>{t('settings.about.subtitle')}</p>

      <div className={styles.aboutGrid}>
        <div className={styles.aboutCard}>
          <h3 className={styles.aboutCardTitle}>{t('settings.about.versionTitle')}</h3>
          <div className={styles.aboutList}>
            <div className={styles.aboutItem}>
              <span className={styles.aboutItemLabel}>{t('settings.about.clientVersion')}</span>
              <span className={`${styles.aboutItemValue} tabular`}>v3.5.1 (Broadcast Console)</span>
            </div>
            <div className={styles.aboutItem}>
              <span className={styles.aboutItemLabel}>{t('settings.about.protocol')}</span>
              <span className={`${styles.aboutItemValue} tabular`}>{t('settings.about.protocolVersion')}</span>
            </div>
            <div className={styles.aboutItem}>
              <span className={styles.aboutItemLabel}>{t('settings.about.serverStatus')}</span>
              <span className={styles.aboutItemValue}>
                {serverConnected ? (
                  <span className={styles.statValue} style={{ color: 'var(--status-success)' }}>● {t('settings.about.connected')}</span>
                ) : (
                  <span className={styles.statValue} style={{ color: 'var(--status-warning)' }}>○ {t('settings.about.disconnected')}</span>
                )}
              </span>
            </div>
          </div>
        </div>

        <div className={styles.aboutCard}>
          <h3 className={styles.aboutCardTitle}>{t('settings.about.licenseTitle')}</h3>
          <p className={styles.aboutStorageCopy}>{t('settings.about.licenseText')}</p>
        </div>
      </div>

      <div className={styles.aboutStorageCard}>
        <h3 className={styles.aboutCardTitle}>{t('settings.about.storageTitle')}</h3>
        <p className={styles.aboutStorageCopy}>{t('settings.about.storageDescription')}</p>
        <div>
          <Button
            variant="secondary"
            size="sm"
            onClick={async () => {
              const ok = await confirm({
                title: t('settings.about.storageTitle'),
                message: t('settings.about.storageDescription'),
                confirmLabel: t('settings.about.clearStorage'),
              });
              if (ok) {
                try {
                  localStorage.removeItem('xg2g.settings.audioMode');
                  localStorage.removeItem('xg2g.settings.dvrMode');
                  onResetPreferences();
                  toast({ message: t('settings.about.storageCleared'), kind: 'success' });
                } catch {
                  // ignore localStorage errors
                }
              }
            }}
          >
            {t('settings.about.clearStorage')}
          </Button>
        </div>
      </div>
    </div>
  );
}
