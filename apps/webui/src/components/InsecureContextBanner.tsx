// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { detectInsecureContext } from '../lib/insecureContext';
import styles from './InsecureContextBanner.module.css';

const DISMISS_KEY = 'xg2g.insecureContextBannerDismissed';
const DOCS_URL = 'https://github.com/ManuGH/xg2g/blob/main/deploy/REVERSE_PROXY.md';

// One-glance hint when the app is opened over plain HTTP on a non-localhost host
// — the configuration in which playback silently does not start. Dismissible for
// the session. Renders nothing over HTTPS or on localhost (see detectInsecureContext).
export function InsecureContextBanner() {
  const { t } = useTranslation();
  const [insecure] = useState(detectInsecureContext);
  const [dismissed, setDismissed] = useState<boolean>(() => {
    try {
      return sessionStorage.getItem(DISMISS_KEY) === '1';
    } catch {
      return false;
    }
  });

  if (!insecure || dismissed) {
    return null;
  }

  const dismiss = () => {
    setDismissed(true);
    try {
      sessionStorage.setItem(DISMISS_KEY, '1');
    } catch {
      /* sessionStorage unavailable — dismiss for this render only */
    }
  };

  return (
    <div className={styles.banner} role="alert">
      <span className={styles.text}>
        {t('insecureContext.message')}{' '}
        <a className={styles.link} href={DOCS_URL} target="_blank" rel="noreferrer noopener">
          {t('insecureContext.learnMore')}
        </a>
      </span>
      <button
        type="button"
        className={styles.dismiss}
        onClick={dismiss}
        aria-label={t('insecureContext.dismiss')}
      >
        ×
      </button>
    </div>
  );
}
