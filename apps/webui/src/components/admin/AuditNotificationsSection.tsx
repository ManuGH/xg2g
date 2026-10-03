// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
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
  const { t, i18n } = useTranslation();
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
      setError(t('admin.audit.loadError'));
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
        new Notification(t('admin.audit.notificationTitle'), {
          body: t('admin.audit.notificationBody'),
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
    const headers = [
      t('admin.audit.colTimestamp'),
      t('admin.audit.colActor'),
      t('admin.audit.colAction'),
      t('admin.audit.colTargetResource'),
      t('admin.audit.colHash'),
    ];
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
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.audit.title')}</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
          {t('admin.audit.subtitle')}
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}

      {/* WebPush Settings Banner */}
      <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h4 style={{ margin: 0, fontSize: '16px', color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span>📲</span> {t('admin.audit.webPushTitle')}
          </h4>
          <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
            {t('admin.audit.webPushSubtitle')}
          </p>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <span
            style={{
              padding: '4px 12px',
              borderRadius: '20px',
              fontSize: '12px',
              fontWeight: 600,
              backgroundColor: pushStatus === 'granted' ? 'rgba(34,197,94,0.2)' : pushStatus === 'denied' ? 'rgba(239,68,68,0.2)' : 'rgba(234,179,8,0.2)',
              color: pushStatus === 'granted' ? 'var(--status-success)' : pushStatus === 'denied' ? 'var(--status-error)' : 'var(--status-warning)',
            }}
          >
            {pushStatus === 'granted' ? t('admin.audit.pushStatusGranted') : pushStatus === 'denied' ? t('admin.audit.pushStatusDenied') : t('admin.audit.pushStatusDefault')}
          </span>

          {pushStatus !== 'granted' && pushStatus !== 'unsupported' && (
            <button
              onClick={handleEnablePush}
              disabled={subscribing}
              style={{
                padding: '10px 18px',
                borderRadius: '10px',
                border: 'none',
                backgroundColor: 'var(--accent-action)',
                color: 'var(--bg-base)',
                fontSize: '13px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              {subscribing ? t('admin.audit.subscribing') : t('admin.audit.enablePush')}
            </button>
          )}
        </div>
      </div>

      {/* Audit Log Table & Search */}
      <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px', flexWrap: 'wrap', gap: '12px' }}>
          <div style={{ padding: '6px 12px', borderRadius: '8px', backgroundColor: 'rgba(34,197,94,0.1)', color: 'var(--status-success)', fontSize: '12px', fontWeight: 600 }}>
            {t('admin.audit.integrityBadge', { count: logs.length })}
          </div>

          <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('admin.audit.filterPlaceholder')}
              style={{ padding: '8px 14px', borderRadius: '8px', backgroundColor: 'var(--bg-base)', border: '1px solid rgba(255,255,255,0.15)', color: 'var(--text-primary)', fontSize: '13px', outline: 'none', width: '220px' }}
            />
            <button
              onClick={exportAuditLogsCSV}
              disabled={logs.length === 0}
              style={{ padding: '8px 14px', borderRadius: '8px', border: '1px solid rgba(255,255,255,0.15)', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '13px', fontWeight: 500, cursor: 'pointer' }}
            >
              {t('admin.audit.exportCsv')}
            </button>
          </div>
        </div>

        {loading ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: '13px' }}>{t('admin.audit.loading')}</div>
        ) : filteredLogs.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.1)', color: 'var(--text-tertiary)' }}>
                  <th style={{ padding: '10px' }}>{t('admin.audit.colTimestamp')}</th>
                  <th style={{ padding: '10px' }}>{t('admin.audit.colActor')}</th>
                  <th style={{ padding: '10px' }}>{t('admin.audit.colAction')}</th>
                  <th style={{ padding: '10px' }}>{t('admin.audit.colTargetResource')}</th>
                  <th style={{ padding: '10px' }}>{t('admin.audit.colHash')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogs.map((log) => (
                  <tr key={log.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.05)', color: 'var(--text-secondary)' }}>
                    <td style={{ padding: '10px', fontSize: '12px', color: 'var(--text-disabled)' }}>
                      {new Date(log.createdAt).toLocaleString(i18n.language)}
                    </td>
                    <td style={{ padding: '10px', fontWeight: 600, color: 'var(--text-primary)' }}>{log.actorUserId}</td>
                    <td style={{ padding: '10px' }}>
                      <span style={{ padding: '2px 8px', borderRadius: '6px', backgroundColor: 'rgba(56,189,248,0.15)', color: 'var(--accent-action)', fontSize: '11px', fontFamily: 'monospace' }}>
                        {log.action}
                      </span>
                    </td>
                    <td style={{ padding: '10px' }}>{log.targetResource}</td>
                    <td style={{ padding: '10px', fontSize: '11px', fontFamily: 'monospace', color: 'var(--text-disabled)' }}>
                      {log.hash ? `${log.hash.substring(0, 12)}...` : 'N/A'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ color: 'var(--text-tertiary)', fontSize: '13px', padding: '16px', textAlign: 'center' }}>
            {t('admin.audit.empty')}
          </div>
        )}
      </div>
    </div>
  );
};

export default AuditNotificationsSection;
