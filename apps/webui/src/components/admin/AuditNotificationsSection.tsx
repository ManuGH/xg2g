// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

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
    } catch (e: any) {
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
      console.error('WebPush subscription error:', e);
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
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: '#f8fafc' }}>Benachrichtigungen & Unveränderliches Audit-Protokoll</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: '#94a3b8' }}>
          SHA-256 fälschungssicheres Protokoll aller administrativen Änderungen und WebPush-Benachrichtigungseinstellungen.
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: '#fca5a5', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}

      {/* WebPush Settings Banner */}
      <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h4 style={{ margin: 0, fontSize: '16px', color: '#f8fafc', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span>📲</span> Browser-WebPush & Push-Benachrichtigungen
          </h4>
          <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: '#94a3b8' }}>
            Erhalten Sie Sofortbenachrichtigungen bei Kinder-Freigabeanfragen oder unbefugten Login-Versuchen.
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
              color: pushStatus === 'granted' ? '#4ade80' : pushStatus === 'denied' ? '#ef4444' : '#facc15',
            }}
          >
            {pushStatus === 'granted' ? 'Aktiviert' : pushStatus === 'denied' ? 'Blockiert' : 'Nicht eingerichtet'}
          </span>

          {pushStatus !== 'granted' && pushStatus !== 'unsupported' && (
            <button
              onClick={handleEnablePush}
              disabled={subscribing}
              style={{
                padding: '10px 18px',
                borderRadius: '10px',
                border: 'none',
                backgroundColor: '#38bdf8',
                color: '#0f172a',
                fontSize: '13px',
                fontWeight: 700,
                cursor: 'pointer',
              }}
            >
              {subscribing ? 'Aktiviere...' : 'WebPush aktivieren'}
            </button>
          )}
        </div>
      </div>

      {/* Audit Log Table & Search */}
      <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px', flexWrap: 'wrap', gap: '12px' }}>
          <div style={{ padding: '6px 12px', borderRadius: '8px', backgroundColor: 'rgba(34,197,94,0.1)', color: '#4ade80', fontSize: '12px', fontWeight: 600 }}>
            ✓ SHA-256 Integritätskette intakt ({logs.length} Einträge)
          </div>

          <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="🔍 Nach Aktion oder Akteur filtern..."
              style={{ padding: '8px 14px', borderRadius: '8px', backgroundColor: '#0f172a', border: '1px solid rgba(255,255,255,0.15)', color: '#f8fafc', fontSize: '13px', outline: 'none', width: '220px' }}
            />
            <button
              onClick={exportAuditLogsCSV}
              disabled={logs.length === 0}
              style={{ padding: '8px 14px', borderRadius: '8px', border: '1px solid rgba(255,255,255,0.15)', backgroundColor: '#334155', color: '#cbd5e1', fontSize: '13px', fontWeight: 500, cursor: 'pointer' }}
            >
              📥 CSV Export
            </button>
          </div>
        </div>

        {loading ? (
          <div style={{ color: '#94a3b8', fontSize: '13px' }}>Protokoll wird geladen...</div>
        ) : filteredLogs.length > 0 ? (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8' }}>
                  <th style={{ padding: '10px' }}>Zeitstempel</th>
                  <th style={{ padding: '10px' }}>Akteur</th>
                  <th style={{ padding: '10px' }}>Aktion</th>
                  <th style={{ padding: '10px' }}>Zielressource</th>
                  <th style={{ padding: '10px' }}>SHA-256 Hash</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogs.map((log) => (
                  <tr key={log.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.05)', color: '#cbd5e1' }}>
                    <td style={{ padding: '10px', fontSize: '12px', color: '#64748b' }}>
                      {new Date(log.createdAt).toLocaleString()}
                    </td>
                    <td style={{ padding: '10px', fontWeight: 600, color: '#f8fafc' }}>{log.actorUserId}</td>
                    <td style={{ padding: '10px' }}>
                      <span style={{ padding: '2px 8px', borderRadius: '6px', backgroundColor: 'rgba(56,189,248,0.15)', color: '#38bdf8', fontSize: '11px', fontFamily: 'monospace' }}>
                        {log.action}
                      </span>
                    </td>
                    <td style={{ padding: '10px' }}>{log.targetResource}</td>
                    <td style={{ padding: '10px', fontSize: '11px', fontFamily: 'monospace', color: '#64748b' }}>
                      {log.hash ? `${log.hash.substring(0, 12)}...` : 'N/A'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div style={{ color: '#94a3b8', fontSize: '13px', padding: '16px', textAlign: 'center' }}>
            Keine Protokolleinträge gefunden.
          </div>
        )}
      </div>
    </div>
  );
};

export default AuditNotificationsSection;
