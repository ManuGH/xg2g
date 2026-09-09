// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useTranslation } from 'react-i18next';
import Files from '../Files';
import Logs from '../Logs';
import { Button } from '../ui';
import type { SettingsTool } from '../../routes';
import styles from '../Settings.module.css';

interface AdvancedToolsSectionProps {
  activeTool: SettingsTool | null;
  onSelectTool: (tool: SettingsTool) => void;
}

export default function AdvancedToolsSection({
  activeTool,
  onSelectTool,
}: AdvancedToolsSectionProps) {
  const { t } = useTranslation();

  return (
    <div className={styles.section}>
      <h2>{t('settings.advanced.title', { defaultValue: 'Advanced tools' })}</h2>
      <p className={styles.subtitle}>
        {t('settings.advanced.subtitle', {
          defaultValue: 'File browser and diagnostic logs stay available here as expert tools without adding more main navigation.',
        })}
      </p>
      <div className={styles.advancedActions}>
        <Button
          variant="secondary"
          active={activeTool === 'files'}
          onClick={() => onSelectTool('files')}
        >
          {t('nav.files')}
        </Button>
        <Button
          variant="secondary"
          active={activeTool === 'logs'}
          onClick={() => onSelectTool('logs')}
        >
          {t('nav.logs')}
        </Button>
      </div>
      {activeTool === 'files' ? (
        <div className={styles.embeddedTool}>
          <Files showLegacyNotice={false} />
        </div>
      ) : null}
      {activeTool === 'logs' ? (
        <div className={styles.embeddedTool}>
          <Logs showLegacyNotice={false} />
        </div>
      ) : null}
    </div>
  );
}
