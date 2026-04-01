// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemHealth, postSystemRefresh, type SystemHealth } from '../client-ts';
import { toAppError } from '../lib/appErrors';
import { unwrapClientResultOrThrow } from '../services/clientWrapper';
import type { AppError } from '../types/errors';
import { Button, ButtonLink, Card, CardBody, CardHeader, CardSubtitle, CardTitle, StatusChip, type ChipState } from './ui';
import ErrorPanel from './ErrorPanel';
import styles from './Files.module.css';

function Files() {
  const { t } = useTranslation();
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [regenerating, setRegenerating] = useState<boolean>(false);
  const [error, setError] = useState<AppError | null>(null);

  const fetchStatus = async () => {
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
  };

  useEffect(() => {
    fetchStatus();
  }, []);

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

  const origin = window.location.origin;
  const hdhrUrl = `${origin}/device.xml`;
  const hdhrBaseUrl = hdhrUrl.replace('/device.xml', '');
  const m3uUrl = '/files/playlist.m3u';
  const m3uAbsoluteUrl = `${origin}${m3uUrl}`;
  const xmltvUrl = `${origin}/xmltv.xml`;
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
  const metrics = [
    {
      label: t('files.metricFeeds'),
      value: t('files.metricFeedsValue'),
      detail: t('files.metricFeedsDetail'),
    },
    {
      label: t('files.metricGuide'),
      value: health?.epg?.status === 'ok'
        ? t('files.metricGuideReady')
        : t('files.metricGuideWaiting'),
      detail: guideStatusLabel,
    },
    {
      label: t('files.metricHost'),
      value: window.location.host,
      detail: t('files.metricHostDetail'),
    },
  ];

  const copyToClipboard = (value: string) => {
    void navigator.clipboard.writeText(value);
  };

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      <section className={styles.hero}>
        <div className={styles.heroCopy}>
          <p className={styles.eyebrow}>{t('files.eyebrow')}</p>
          <h2>{t('files.title')}</h2>
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

      <div className={styles.metricGrid}>
        {metrics.map((metric) => (
          <div key={metric.label} className={styles.metricCard}>
            <span className={styles.metricLabel}>{metric.label}</span>
            <span className={styles.metricValue}>{metric.value}</span>
            <span className={styles.metricDetail}>{metric.detail}</span>
          </div>
        ))}
      </div>

      <div className={styles.list}>
        <Card variant="action" className={[styles.card, styles.cardWide].join(' ')}>
          <CardHeader className={styles.cardHeader}>
            <div>
              <CardSubtitle>{t('files.m3uEyebrow')}</CardSubtitle>
              <CardTitle>{t('files.m3uTitle')}</CardTitle>
            </div>
            <StatusChip state="success" label={t('files.clientReady')} />
          </CardHeader>
          <CardBody className={styles.cardBody}>
            <p className={styles.description}>{t('files.m3uCopy')}</p>
            <div className={styles.codeBlock} aria-label="M3U URL">{m3uAbsoluteUrl}</div>
            <div className={styles.actionsRow}>
              <ButtonLink href={m3uUrl} download size="sm">
                {t('files.downloadM3u')}
              </ButtonLink>
              <Button variant="secondary" size="sm" onClick={() => copyToClipboard(m3uAbsoluteUrl)}>
                {t('files.copyM3u')}
              </Button>
            </div>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardHeader className={styles.cardHeader}>
            <div>
              <CardSubtitle>{t('files.xmltvEyebrow')}</CardSubtitle>
              <CardTitle>{t('files.xmltvTitle')}</CardTitle>
            </div>
            <StatusChip
              state={epgState}
              label={health?.epg?.status === 'ok'
                ? t('files.epgLoaded')
                : t('files.epgPending')}
            />
          </CardHeader>
          <CardBody className={styles.cardBody}>
            <p className={styles.description}>{guideStatusLabel}</p>
            <div className={styles.codeBlock} aria-label="XMLTV URL">{xmltvUrl}</div>
            <div className={styles.actionsRow}>
              <Button variant="secondary" size="sm" onClick={() => copyToClipboard(xmltvUrl)}>
                {t('files.copyUrl')}
              </Button>
              <ButtonLink href="/xmltv.xml" variant="secondary" size="sm" download>
                {t('files.downloadXmltv')}
              </ButtonLink>
            </div>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardHeader className={styles.cardHeader}>
            <div>
              <CardSubtitle>{t('files.hdhrEyebrow')}</CardSubtitle>
              <CardTitle>{t('files.hdhrTitle')}</CardTitle>
            </div>
            <StatusChip state="idle" label={t('files.discovery')} />
          </CardHeader>
          <CardBody className={styles.cardBody}>
            <p className={styles.description}>{t('files.hdhrCopy')}</p>
            <div className={styles.codeBlock} aria-label="HDHomeRun base URL">{hdhrBaseUrl}</div>
            <div className={styles.actionsRow}>
              <Button variant="secondary" size="sm" onClick={() => copyToClipboard(hdhrBaseUrl)}>
                {t('files.copyIp')}
              </Button>
              <ButtonLink href={hdhrUrl} variant="ghost" size="sm">
                {t('files.openDeviceXml')}
              </ButtonLink>
            </div>
          </CardBody>
        </Card>
      </div>
    </div>
  );
}

export default Files;
