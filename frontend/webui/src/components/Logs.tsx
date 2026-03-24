// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useCallback, useEffect, useState } from 'react';
import { getLogs, type LogEntry } from '../client-ts';
import { toAppError } from '../lib/appErrors';
import { unwrapClientResultOrThrow } from '../services/clientWrapper';
import type { AppError } from '../types/errors';
import { Button } from './ui';
import ErrorPanel from './ErrorPanel';
import styles from './Logs.module.css';

export default function Logs() {
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
      <div className={styles.header}>
        <h3>Recent Logs</h3>
        <Button onClick={fetchLogs} disabled={loading} variant="secondary" size="sm">
          {loading ? 'Refreshing...' : 'Refresh'}
        </Button>
      </div>

      {error ? <ErrorPanel error={error} onRetry={fetchLogs} titleAs="h3" /> : null}

      {logs.length === 0 ? (
        !loading && !error ? <p className={styles.empty}>No logs available.</p> : null
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
