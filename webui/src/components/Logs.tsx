// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { getLogs, type LogEntry } from '../client-ts';
import { Button } from './ui';
import styles from './Logs.module.css';

export default function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchLogs = async (): Promise<void> => {
    setLoading(true);
    setError(null);

    try {
      const result = await getLogs();

      if (result.error) {
        if (result.response?.status === 401) {
          window.dispatchEvent(new Event('auth-required'));
          setError('Authentication required. Please enter your API token.');
        } else {
          setError('Failed to fetch logs');
        }
      } else if (result.data) {
        setLogs(result.data);
      }
    } catch (err) {
      const error = err as Error;
      setError(error.message || 'Failed to fetch logs');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchLogs();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className={`${styles.view} animate-enter`.trim()}>
      <div className={styles.header}>
        <h3>Recent Logs</h3>
        <Button onClick={fetchLogs} disabled={loading} variant="secondary" size="sm">
          {loading ? 'Refreshing...' : 'Refresh'}
        </Button>
      </div>

      {error && <div className={`${styles.alert} ${styles.alertError}`.trim()} role="alert">Error: {error}</div>}

      {logs.length === 0 && !loading ? (
        <p className={styles.empty}>No logs available.</p>
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
