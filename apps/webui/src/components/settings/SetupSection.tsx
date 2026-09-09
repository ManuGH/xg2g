// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React from 'react';
import { useTranslation } from 'react-i18next';
import Config from '../Config';
import { Button } from '../ui';
import styles from '../Settings.module.css';

interface SetupSectionProps {
  configured: boolean;
  showSetup: boolean;
  setShowSetup: React.Dispatch<React.SetStateAction<boolean>>;
  refetchConfig: () => void;
}

export default function SetupSection({
  configured,
  showSetup,
  setShowSetup,
  refetchConfig,
}: SetupSectionProps) {
  const { t } = useTranslation();

  return (
    <div className={styles.setup}>
      {!configured ? (
        <Config onUpdate={() => { void refetchConfig(); }} />
      ) : (
        <div className={styles.section}>
          <div className={styles.accordionHeader}>
            <h2>{t('setup.title')}</h2>
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
  );
}
