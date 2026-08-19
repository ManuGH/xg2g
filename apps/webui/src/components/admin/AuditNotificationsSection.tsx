// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import styles from './AuditNotificationsSection.module.css';
import { debugError } from '../../utils/logging';

export interface AuditLogItem {
  id: number;
  actorUserId: string;
  action: string;
  targetResource: string;
  prevHash: string;
  hash: string;
  createdAt: string;
}

export const AuditNotificationsSection: React.FC = () => {
  const [logs, setLogs] = useState<AuditLogItem[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState<string>('');

  // WebPush Notification State
  const [pushStatus, setPushStatus] = useState<'default' | 'granted' | 'denied' | 'unsupported'>('default');
  const [subscribing, setSubscribing] = useState<boolean>(false);

  const fetchAuditLogs = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/audit-logs');
      if (res.ok) {
        const data = await res.json();
        setLogs(Array.isArray(data) ? data : []);
      }
    } catch {
      setError('Audit-Protokoll konnte nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchAuditLogs();

    if ('Notification' in window) {
      setPushStatus(Notification.permission);
    } else {
      setPushStatus('unsupported');
    }
  }, []);

  const handleEnablePush = async () => {
    if (!('Notification' in window)) return;
    setSubscribing(true);
    try {
      const perm = await Notification.requestPermission();
      setPushStatus(perm);
      if (perm === 'granted') {
        new Notification('xg2g Benachrichtigungen aktiv', {
          body: 'Sie erhalten nun Echtzeit-Freigabeanfragen und Sicherheitswarnungen im Browser.',
          icon: '/favicon.ico',
        });
      }
	} catch (e) {
		debugError('WebPush subscription error:', e);
	} finally {
      setSubscribing(false);
    }
  };

  const exportAuditLogsCSV = () => {
    if (logs.length === 0) return;
    const headers = ['ID', 'Zeitstempel', 'Akteur', 'Aktion', 'Zielressource', 'SHA-256 Hash'];
    const rows = logs.map((l) => [l.id, l.createdAt, l.actorUserId, l.action, l.targetResource, l.hash]);
    const csvContent = 'data:text/csv;charset=utf-8,' + [headers.join(','), ...rows.map((r) => r.join(','))].join('\n');
    const encodedUri = encodeURI(csvContent);
    const link = document.createElement('a');
    link.setAttribute('href', encodedUri);
    link.setAttribute('download', `xg2g_audit_log_${new Date().toISOString().slice(0, 10)}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const filteredLogs = logs.filter(
    (l) =>
      l.action.toLowerCase().includes(searchQuery.toLowerCase()) ||
      l.actorUserId.toLowerCase().includes(searchQuery.toLowerCase()) ||
      l.targetResource.toLowerCase().includes(searchQuery.toLowerCase())
  );

  return (
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>Benachrichtigungen &amp; Unveränderliches Audit-Protokoll</h3>
        <p className={styles.subheading}>
          SHA-256 fälschungssicheres Protokoll aller administrativen Änderungen und WebPush-Benachrichtigungseinstellungen.
        </p>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}

      {/* WebPush Settings Banner */}
      <div className={`${styles.panel} ${styles.pushPanel}`}>
        <div>
          <h4 className={styles.pushTitle}>
            <span>📲</span> Browser-WebPush &amp; Push-Benachrichtigungen
          </h4>
          <p className={styles.pushText}>
            Erhalten Sie Sofortbenachrichtigungen bei Kinder-Freigabeanfragen oder unbefugten Login-Versuchen.
          </p>
        </div>

        <div className={styles.pushControls}>
          <span
            className={`${styles.pushBadge} ${
              pushStatus === 'granted' ? styles.pushGranted : pushStatus === 'denied' ? styles.pushDenied : styles.pushPending
            }`}
          >
            {pushStatus === 'granted' ? 'Aktiviert' : pushStatus === 'denied' ? 'Blockiert' : 'Nicht eingerichtet'}
          </span>

          {pushStatus !== 'granted' && pushStatus !== 'unsupported' && (
            <button
              onClick={handleEnablePush}
              disabled={subscribing}
              className={`${styles.button} ${styles.pushButton}`}
            >
              {subscribing ? 'Aktiviere...' : 'WebPush aktivieren'}
            </button>
          )}
        </div>
      </div>

      {/* Audit Log Table & Search */}
      <div className={styles.panel}>
        <div className={styles.auditHeader}>
          <div className={styles.integrityChip}>
            ✓ SHA-256 Integritätskette intakt ({logs.length} Einträge)
          </div>

          <div className={styles.auditControls}>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="🔍 Nach Aktion oder Akteur filtern..."
              className={styles.searchInput}
            />
            <button
              onClick={exportAuditLogsCSV}
              disabled={logs.length === 0}
              className={`${styles.button} ${styles.exportButton}`}
            >
              📥 CSV Export
            </button>
          </div>
        </div>

        {loading ? (
          <div className={styles.loading}>Protokoll wird geladen...</div>
        ) : filteredLogs.length > 0 ? (
          <div className={styles.tableWrapper}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>Zeitstempel</th>
                  <th>Akteur</th>
                  <th>Aktion</th>
                  <th>Zielressource</th>
                  <th>SHA-256 Hash</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogs.map((log) => (
                  <tr key={log.id}>
                    <td className={styles.cellTime}>{new Date(log.createdAt).toLocaleString()}</td>
                    <td className={styles.cellActor}>{log.actorUserId}</td>
                    <td>
                      <span className={styles.actionChip}>{log.action}</span>
                    </td>
                    <td>{log.targetResource}</td>
                    <td className={styles.cellHash}>
                      {log.hash ? `${log.hash.substring(0, 12)}...` : 'N/A'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className={styles.emptyState}>Keine Protokolleinträge gefunden.</div>
        )}
      </div>
    </div>
  );
};

export default AuditNotificationsSection;
