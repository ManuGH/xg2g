// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemHealth, postSystemRefresh, type SystemHealth } from '../client-ts';
import { toAppError } from '../lib/appErrors';
import { ROUTE_MAP } from '../routes';
import { unwrapClientResultOrThrow } from '../services/clientWrapper';
import type { AppError } from '../types/errors';
import { Button, StatusChip, type ChipState } from './ui';
import ErrorPanel from './ErrorPanel';
import LegacyRouteNotice from './LegacyRouteNotice';
import styles from './Files.module.css';

interface FilesProps {
  showLegacyNotice?: boolean;
}

function Files({ showLegacyNotice = true }: FilesProps) {
  const { t } = useTranslation();
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [regenerating, setRegenerating] = useState<boolean>(false);
  const [error, setError] = useState<AppError | null>(null);

  const fetchStatus = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getSystemHealth();
      const data = unwrapClientResultOrThrow<SystemHealth>(response, { source: 'Files.fetchStatus' });
      setHealth(data);
      setError(null);
    } catch (err) {
      setError(toAppError(err, { fallbackTitle: t('files.loadErrorTitle') }));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  const handleRegenerate = async () => {
    setError(null);
    setRegenerating(true);
    try {
      const response = await postSystemRefresh();
      unwrapClientResultOrThrow(response, { source: 'Files.handleRegenerate' });
      setTimeout(fetchStatus, 1000);
    } catch (err) {
      setError(toAppError(err, { fallbackTitle: t('files.regenerateErrorTitle') }));
    } finally {
      setRegenerating(false);
    }
  };

  if (loading && !health) return <div className={styles.loading}>{t('files.loading')}</div>;
  if (error) {
    return (
      <div className={`${styles.container} animate-enter`.trim()}>
        <ErrorPanel
          error={error}
          onRetry={fetchStatus}
          titleAs="h3"
        />
      </div>
    );
  }

  const healthState: ChipState = health?.status === 'ok' ? 'success' : 'warning';
  const epgState: ChipState = health?.epg?.status === 'ok'
    ? ((health?.epg?.missingChannels ?? 0) > 0 ? 'warning' : 'success')
    : 'warning';
  const missingChannels = health?.epg?.missingChannels ?? 0;
  const guideStatusLabel = health?.epg?.status === 'ok'
    ? (missingChannels > 0
      ? t('files.guidePartial', { count: missingChannels })
      : t('files.guideHealthy'))
    : t('files.guidePending');

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      {showLegacyNotice ? (
        <LegacyRouteNotice
          parentLabel={t('nav.playerSettings')}
          description={t('legacyRoute.filesDescription', {
            defaultValue: 'Regenerating the playlist and guide stays available as an expert tool. You can now also reach it from Settings.',
          })}
          route={ROUTE_MAP.settings}
        />
      ) : null}
      <section className={styles.hero}>
        <div className={styles.heroCopy}>
          <p className={styles.eyebrow}>{t('files.eyebrow')}</p>
          <h1>{t('files.title')}</h1>
          <p className={styles.lead}>{t('files.lead')}</p>
        </div>

        <div className={styles.heroActions}>
          <StatusChip state={healthState} label={t('files.stackReady')} />
          <StatusChip state={epgState} label={guideStatusLabel} />
          <Button onClick={handleRegenerate} disabled={regenerating}>
            {regenerating ? t('files.regenerating') : t('files.regenerate')}
          </Button>
        </div>
      </section>
    </div>
  );
}

export default Files;
