// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useTranslation } from 'react-i18next';
import { Button } from '../ui';
import styles from '../Settings.module.css';

interface DevicePairingSectionProps {
  pairingCodeDraft: string;
  setPairingCodeDraft: (code: string) => void;
  pairingSubmitting: boolean;
  pairingFeedback: { success: boolean; message: string } | null;
  setPairingFeedback: (feedback: { success: boolean; message: string } | null) => void;
  handleApprovePairing: () => Promise<void>;
  androidTvBaseUrlDisplay: string;
  androidTvLaunchUrl: string;
  androidTvLaunchDisabled: boolean;
  androidTvLaunchHint: string;
}

export default function DevicePairingSection({
  pairingCodeDraft,
  setPairingCodeDraft,
  pairingSubmitting,
  pairingFeedback,
  setPairingFeedback,
  handleApprovePairing,
  androidTvBaseUrlDisplay,
  androidTvLaunchUrl,
  androidTvLaunchDisabled,
  androidTvLaunchHint,
}: DevicePairingSectionProps) {
  const { t } = useTranslation();

  return (
    <div className={styles.section}>
      <h2>{t('settings.devices.tabTitle', { defaultValue: t('settings.androidTv.title') })}</h2>
      <p className={styles.subtitle}>{t('settings.androidTv.subtitle')}</p>

      <div className={styles.onboardingCard}>
        <div className={styles.onboardingHero}>
          <div className={styles.onboardingIntro}>
            <p className={styles.onboardingEyebrow}>{t('settings.devices.heroEyebrow')}</p>
            <h3 className={styles.onboardingTitle}>{t('settings.devices.heroTitle')}</h3>
            <p className={styles.onboardingCopy}>
              {t('settings.devices.heroCopy')}
            </p>
          </div>
        </div>

        <div className={styles.onboardingMeta}>
          <div className={styles.group}>
            <label>{t('settings.androidTv.currentServer')}</label>
            <code className={`${styles.launchValue} tabular`.trim()}>{androidTvBaseUrlDisplay}</code>
            <span className={styles.hint}>{t('settings.androidTv.currentServerHint')}</span>
          </div>

          <div className={`${styles.group} ${styles.pairingGroup}`}>
            <label className={styles.pairingLabel} htmlFor="android-tv-pairing-code">
              {t('settings.devices.keyAuthLabel')}
            </label>
            <p className={`${styles.hint} ${styles.pairingHint}`}>
              {t('settings.devices.keyAuthHint')}
            </p>
            <div className={styles.pairingControls}>
              <input
                id="android-tv-pairing-code"
                className={styles.pairingInput}
                type="text"
                placeholder={t('settings.devices.inputPlaceholder')}
                value={pairingCodeDraft}
                autoCapitalize="characters"
                autoCorrect="off"
                spellCheck="false"
                onChange={(e) => { setPairingCodeDraft(e.target.value.toUpperCase()); setPairingFeedback(null); }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    void handleApprovePairing();
                  }
                }}
              />
              <Button
                id="pairing-approve-submit"
                onClick={() => { void handleApprovePairing(); }}
                disabled={pairingSubmitting || !pairingCodeDraft.trim()}
                className={styles.onboardingButton}
              >
                {pairingSubmitting ? t('settings.devices.buttonPairing') : t('settings.devices.buttonPair')}
              </Button>
            </div>
            {pairingFeedback ? (
              <p className={`${styles.pairingFeedback} ${pairingFeedback.success ? styles.pairingFeedbackSuccess : styles.pairingFeedbackError}`}>
                {pairingFeedback.message}
              </p>
            ) : null}
          </div>

          <div className={styles.onboardingActions}>
            {androidTvLaunchDisabled ? (
              <Button
                className={styles.onboardingButton}
                disabled
              >
                {t('settings.androidTv.openApp')}
              </Button>
            ) : (
              <Button
                href={androidTvLaunchUrl}
                className={styles.onboardingButton}
                rel="noopener noreferrer"
              >
                {t('settings.androidTv.openApp')}
              </Button>
            )}
            <p className={androidTvLaunchDisabled ? styles.errorInline : styles.hint}>{androidTvLaunchHint}</p>
          </div>
        </div>
      </div>
    </div>
  );
}
