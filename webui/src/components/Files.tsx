// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { getSystemHealth, postSystemRefresh, type SystemHealth } from '../client-ts';
import { Button, ButtonLink } from './ui';
import styles from './Files.module.css';

function Files() {
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [regenerating, setRegenerating] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await getSystemHealth();
      if (response.data) {
        setHealth(response.data);
        setError(null);
      } else if (response.error) {
        // @ts-ignore - response.error might be generic, status check is valid runtime
        if (response.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          // @ts-ignore
          setError(response.error.message || 'Failed to fetch health');
        }
      }
    } catch (err: any) {
      if (err.status === 401) {
        window.dispatchEvent(new Event('auth-required'));
        setError('Authentication required. Please enter your API token.');
      } else {
        setError(err.message || 'Failed to fetch status');
      }
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
      await postSystemRefresh();
      setTimeout(fetchStatus, 1000);
    } catch (err: any) {
      if (err.status === 401) {
        window.dispatchEvent(new Event('auth-required'));
        setError('Authentication required. Please enter your API token.');
      } else {
        setError(err.message || 'Failed to regenerate');
      }
    } finally {
      setRegenerating(false);
    }
  };

  if (loading && !health) return <div className={styles.loading}>Loading...</div>;
  if (error) {
    return (
      <div className={`${styles.alert} ${styles.alertError}`.trim()}>
        <div>Error: {error}</div>
        <Button
          variant="secondary"
          size="sm"
          onClick={fetchStatus}
          disabled={loading}
        >
          Retry
        </Button>
      </div>
    );
  }

  const hdhrUrl = `${window.location.protocol}//${window.location.host}/device.xml`;
  const m3uUrl = '/files/playlist.m3u';
  const xmltvUrl = `${window.location.protocol}//${window.location.host}/xmltv.xml`;

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      <div className={styles.header}>
        <h2>Playlist & EPG</h2>
        <Button onClick={handleRegenerate} disabled={regenerating}>
          {regenerating ? 'Regenerating...' : 'Regenerate Files'}
        </Button>
      </div>

      {health?.epg?.status && (
        <div className={styles.subtle}>
          EPG status:{' '}
          <span
            className={[
              styles.status,
              health.epg.status === 'ok' ? styles.statusOk : styles.statusWarn,
            ].filter(Boolean).join(' ')}
          >
            {health.epg.status}
          </span>
        </div>
      )}

      <div className={styles.list}>
        <div className={styles.card}>
          <h3>M3U Playlist</h3>
          <p className={styles.description}>Standard M3U playlist for VLC, Kodi, TiviMate.</p>
          <ButtonLink href={m3uUrl} download size="sm">
            Download M3U
          </ButtonLink>
        </div>

        <div className={styles.card}>
          <h3>XMLTV Guide</h3>
          <p className={styles.description}>EPG Data.</p>
          {health?.epg?.status === 'ok' ? (
            <p className={`${styles.alert} ${styles.alertSuccess}`.trim()}>EPG Loaded</p>
          ) : (
            <p className={`${styles.alert} ${styles.alertWarning}`.trim()}>EPG Missing or Partial</p>
          )}
          <div className={styles.codeBlock} aria-label="XMLTV URL">{xmltvUrl}</div>
          <div className={styles.actionsRow}>
            <Button variant="secondary" size="sm" onClick={() => navigator.clipboard.writeText(xmltvUrl)}>
              Copy URL
            </Button>
            <ButtonLink href="/xmltv.xml" variant="secondary" size="sm" download>
              Download
            </ButtonLink>
          </div>
        </div>

        <div className={styles.card}>
          <h3>HDHomeRun (Plex)</h3>
          <p className={styles.description}>Use this IP/URL to add xg2g as a DVR in Plex or Jellyfin.</p>
          <div className={styles.codeBlock} aria-label="HDHomeRun base URL">{hdhrUrl.replace('/device.xml', '')}</div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => navigator.clipboard.writeText(hdhrUrl.replace('/device.xml', ''))}
          >
            Copy IP
          </Button>
        </div>
      </div>
    </div>
  );
}

export default Files;
