// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getLogs, type LogEntry } from '../client-ts';
import { toAppError } from '../lib/appErrors';
import { ROUTE_MAP } from '../routes';
import { unwrapClientResultOrThrow } from '../services/clientWrapper';
import type { AppError } from '../types/errors';
import { Button, EmptyState } from './ui';
import ErrorPanel from './ErrorPanel';
import LegacyRouteNotice from './LegacyRouteNotice';
import styles from './Logs.module.css';

interface LogsProps {
  showLegacyNotice?: boolean;
}

export default function Logs({ showLegacyNotice = true }: LogsProps) {
  const { t } = useTranslation();
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<AppError | null>(null);

  const fetchLogs = useCallback(async (): Promise<void> => {
    setLoading(true);
    setError(null);

    try {
      const result = await getLogs();
      const data = unwrapClientResultOrThrow<LogEntry[]>(result, { source: 'Logs.fetchLogs' });
      setLogs(data ?? []);
    } catch (err) {
      setError(toAppError(err, { fallbackTitle: 'Unable to load logs' }));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchLogs();
  }, [fetchLogs]);

  return (
    <div className={`${styles.view} animate-enter`.trim()}>
      {showLegacyNotice ? (
        <LegacyRouteNotice
          parentLabel={t('nav.playerSettings')}
          description={t('legacyRoute.logsDescription', {
            defaultValue: 'Diagnostics stay available for direct access, but most households should enter this area from Settings.',
          })}
          route={ROUTE_MAP.settings}
        />
      ) : null}
      <div className={styles.header}>
        <h3>Recent Logs</h3>
        <Button onClick={fetchLogs} disabled={loading} variant="secondary" size="sm">
          {loading ? t('common.refreshing') : t('common.refresh')}
        </Button>
      </div>

      {error ? <ErrorPanel error={error} onRetry={fetchLogs} titleAs="h3" /> : null}

      {logs.length === 0 ? (
        !loading && !error ? (
          <EmptyState variant="inline" icon="○" title={t('logs.empty')} />
        ) : null
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Time</th>
                <th>Level</th>
                <th>Component</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((log, idx) => (
                <tr
                  key={idx}
                  className={styles.row}
                  data-level={String(log.level || '').toLowerCase() || undefined}
                >
                  <td className={styles.time}>{new Date(log.time || '').toLocaleTimeString()}</td>
                  <td className={styles.level}>{log.level}</td>
                  <td>{(log.fields?.component as string) || ''}</td>
                  <td className={styles.message}>{log.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
