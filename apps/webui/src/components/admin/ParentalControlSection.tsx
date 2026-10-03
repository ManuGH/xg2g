// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';

export interface ApprovalRequest {
  id: string;
  requesterUserId: string;
  requesterName?: string;
  profileId?: string;
  profileName?: string;
  eventTitle: string;
  eventRating: number;
  channelName?: string;
  status: 'pending' | 'approved' | 'denied';
  createdAt: string;
}

export const ParentalControlSection: React.FC = () => {
  const { t, i18n } = useTranslation();
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [errorDetail, setErrorDetail] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchApprovals = async () => {
    setLoading(true);
    setError(null);
    setErrorDetail(null);
    try {
      const res = await fetch('/api/v3/household/approvals');
      if (res.ok) {
        const data = await res.json();
        setApprovals(Array.isArray(data) ? data : []);
      }
    } catch (e: any) {
      setError(t('admin.parental.loadError'));
      setErrorDetail(e?.message && e.message !== t('admin.parental.loadError') ? e.message : null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchApprovals();
  }, []);

  const handleApprove = async (id: string, scope: 'once' | 'always') => {
    setActionLoading(id);
    setError(null);
    setErrorDetail(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${id}/approve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ scope }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setSuccess(scope === 'always' ? t('admin.parental.approvedAlways') : t('admin.parental.approvedOnce'));
      void fetchApprovals();
    } catch (e: any) {
      setError(t('admin.parental.approveError'));
      setErrorDetail(e?.message && e.message !== t('admin.parental.approveError') ? e.message : null);
    } finally {
      setActionLoading(null);
    }
  };

  const handleDeny = async (id: string) => {
    setActionLoading(id);
    setError(null);
    setErrorDetail(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${id}/deny`, {
        method: 'POST',
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setSuccess(t('admin.parental.denied'));
      void fetchApprovals();
    } catch (e: any) {
      setError(t('admin.parental.denyError'));
      setErrorDetail(e?.message && e.message !== t('admin.parental.denyError') ? e.message : null);
    } finally {
      setActionLoading(null);
    }
  };

  const pendingRequests = approvals.filter((a) => a.status === 'pending');
  const pastRequests = approvals.filter((a) => a.status !== 'pending');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>{t('admin.parental.title')}</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
          {t('admin.parental.subtitle')}
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          <div>⚠️ {error}</div>
          {errorDetail && (
            <div data-testid="error-detail" style={{ marginTop: '4px', fontSize: '11px', opacity: 0.85, fontFamily: 'monospace' }}>
              {errorDetail}
            </div>
          )}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: 'var(--status-success)', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {/* Pending Approval Requests Section */}
      <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h4 style={{ margin: 0, fontSize: '16px', color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span>🔔</span> {t('admin.parental.pendingRequests', { count: pendingRequests.length })}
          </h4>
          <button onClick={fetchApprovals} style={{ padding: '6px 12px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '12px', cursor: 'pointer' }}>
            {t('admin.parental.refresh')}
          </button>
        </div>

        {loading ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: '13px' }}>{t('admin.parental.loading')}</div>
        ) : pendingRequests.length > 0 ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            {pendingRequests.map((req) => (
              <div
                key={req.id}
                style={{
                  backgroundColor: 'var(--bg-base)',
                  padding: '16px 20px',
                  borderRadius: '12px',
                  border: '1px solid rgba(234,179,8,0.3)',
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                }}
              >
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                    <span style={{ fontSize: '18px' }}>👦</span>
                    <span style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: '15px' }}>{req.profileName || req.requesterName || t('admin.parental.defaultProfile')}</span>
                    <span style={{ padding: '2px 8px', borderRadius: '8px', backgroundColor: 'rgba(239,68,68,0.2)', color: 'var(--status-error)', fontSize: '11px', fontWeight: 700 }}>
                      {t('admin.parental.fskRating', { rating: req.eventRating })}
                    </span>
                  </div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '14px', marginTop: '6px', fontWeight: 500 }}>
                    {req.channelName ? t('admin.parental.requestWantToWatchWithChannel', { title: req.eventTitle, channel: req.channelName }) : t('admin.parental.requestWantToWatch', { title: req.eventTitle })}
                  </div>
                  <div style={{ color: 'var(--text-disabled)', fontSize: '11px', marginTop: '4px' }}>
                    {t('admin.parental.requestedAt', { time: new Date(req.createdAt).toLocaleTimeString(i18n.language) })}
                  </div>
                </div>

                <div style={{ display: 'flex', gap: '8px' }}>
                  <button
                    onClick={() => handleDeny(req.id)}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 14px', borderRadius: '10px', border: 'none', backgroundColor: 'rgba(239,68,68,0.15)', color: 'var(--status-error)', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    {t('admin.parental.deny')}
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'once')}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 14px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--accent-action)', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    {t('admin.parental.approveOnce')}
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'always')}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 16px', borderRadius: '10px', border: 'none', backgroundColor: 'var(--status-success)', color: 'var(--bg-base)', fontSize: '13px', fontWeight: 700, cursor: 'pointer' }}
                  >
                    {t('admin.parental.approveAlways')}
                  </button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div style={{ color: 'var(--text-tertiary)', fontSize: '13px', padding: '16px', textAlign: 'center', backgroundColor: 'var(--bg-base)', borderRadius: '12px' }}>
            {t('admin.parental.empty')}
          </div>
        )}
      </div>

      {/* Past Approvals History */}
      {pastRequests.length > 0 && (
        <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h4 style={{ margin: '0 0 12px 0', fontSize: '15px', color: 'var(--text-secondary)' }}>{t('admin.parental.historyTitle')}</h4>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            {pastRequests.slice(0, 5).map((req) => (
              <div key={req.id} style={{ padding: '10px 14px', backgroundColor: 'var(--bg-base)', borderRadius: '8px', display: 'flex', justifyContent: 'space-between', fontSize: '13px' }}>
                <span style={{ color: 'var(--text-primary)' }}>{req.profileName || t('admin.parental.fallbackProfile')} – „{req.eventTitle}“</span>
                <span style={{ color: req.status === 'approved' ? 'var(--status-success)' : 'var(--status-error)', fontWeight: 600 }}>
                  {req.status === 'approved' ? t('admin.parental.statusApproved') : t('admin.parental.statusDenied')}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

export default ParentalControlSection;
