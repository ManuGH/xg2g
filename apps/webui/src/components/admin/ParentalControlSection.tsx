// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

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
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchApprovals = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/approvals');
      if (res.ok) {
        const data = await res.json();
        setApprovals(Array.isArray(data) ? data : []);
      }
    } catch (e: any) {
      setError('Freigabe-Anfragen konnten nicht geladen werden.');
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
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${id}/approve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ scope }),
      });
      if (!res.ok) throw new Error('Freigabe fehlgeschlagen.');
      setSuccess(scope === 'always' ? 'Sendung dauerhaft freigegeben.' : 'Sendung 1-malig freigegeben.');
      void fetchApprovals();
    } catch (e: any) {
      setError(e.message || 'Fehler bei der Freigabe.');
    } finally {
      setActionLoading(null);
    }
  };

  const handleDeny = async (id: string) => {
    setActionLoading(id);
    setError(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/approvals/${id}/deny`, {
        method: 'POST',
      });
      if (!res.ok) throw new Error('Ablehnung fehlgeschlagen.');
      setSuccess('Freigabe-Anfrage wurde abgelehnt.');
      void fetchApprovals();
    } catch (e: any) {
      setError(e.message || 'Fehler bei der Ablehnung.');
    } finally {
      setActionLoading(null);
    }
  };

  const pendingRequests = approvals.filter((a) => a.status === 'pending');
  const pastRequests = approvals.filter((a) => a.status !== 'pending');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: '#f8fafc' }}>Jugendschutz & Live Freigaben</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: '#94a3b8' }}>
          Verwalten Sie ausstehende FSK-Freigabeanfragen von Kinderprofilen in Echtzeit.
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: '#fca5a5', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: '#4ade80', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {/* Pending Approval Requests Section */}
      <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h4 style={{ margin: 0, fontSize: '16px', color: '#f8fafc', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span>🔔</span> Ausstehende Freigabeanfragen ({pendingRequests.length})
          </h4>
          <button onClick={fetchApprovals} style={{ padding: '6px 12px', borderRadius: '8px', border: 'none', backgroundColor: '#334155', color: '#cbd5e1', fontSize: '12px', cursor: 'pointer' }}>
            🔄 Aktualisieren
          </button>
        </div>

        {loading ? (
          <div style={{ color: '#94a3b8', fontSize: '13px' }}>Lade Freigaben...</div>
        ) : pendingRequests.length > 0 ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
            {pendingRequests.map((req) => (
              <div
                key={req.id}
                style={{
                  backgroundColor: '#0f172a',
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
                    <span style={{ fontWeight: 600, color: '#f8fafc', fontSize: '15px' }}>{req.profileName || req.requesterName || 'Kinderprofil'}</span>
                    <span style={{ padding: '2px 8px', borderRadius: '8px', backgroundColor: 'rgba(239,68,68,0.2)', color: '#ef4444', fontSize: '11px', fontWeight: 700 }}>
                      FSK {req.eventRating}
                    </span>
                  </div>
                  <div style={{ color: '#cbd5e1', fontSize: '14px', marginTop: '6px', fontWeight: 500 }}>
                    Möchte „{req.eventTitle}“ {req.channelName ? `auf ${req.channelName}` : ''} ansehen
                  </div>
                  <div style={{ color: '#64748b', fontSize: '11px', marginTop: '4px' }}>
                    Angefragt am {new Date(req.createdAt).toLocaleTimeString()} Uhr
                  </div>
                </div>

                <div style={{ display: 'flex', gap: '8px' }}>
                  <button
                    onClick={() => handleDeny(req.id)}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 14px', borderRadius: '10px', border: 'none', backgroundColor: 'rgba(239,68,68,0.15)', color: '#fca5a5', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    Ablehnen
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'once')}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 14px', borderRadius: '10px', border: 'none', backgroundColor: '#334155', color: '#38bdf8', fontSize: '13px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    1-malig erlauben
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'always')}
                    disabled={actionLoading === req.id}
                    style={{ padding: '8px 16px', borderRadius: '10px', border: 'none', backgroundColor: '#22c55e', color: '#0f172a', fontSize: '13px', fontWeight: 700, cursor: 'pointer' }}
                  >
                    Immer freigeben
                  </button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div style={{ color: '#94a3b8', fontSize: '13px', padding: '16px', textAlign: 'center', backgroundColor: '#0f172a', borderRadius: '12px' }}>
            Keine ausstehenden Freigabeanfragen. Alle Kinderprofile halten sich an ihre FSK-Grenzen.
          </div>
        )}
      </div>

      {/* Past Approvals History */}
      {pastRequests.length > 0 && (
        <div style={{ backgroundColor: '#1e293b', padding: '24px', borderRadius: '16px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h4 style={{ margin: '0 0 12px 0', fontSize: '15px', color: '#cbd5e1' }}>Historie vergangener Entscheidungen</h4>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            {pastRequests.slice(0, 5).map((req) => (
              <div key={req.id} style={{ padding: '10px 14px', backgroundColor: '#0f172a', borderRadius: '8px', display: 'flex', justifyContent: 'space-between', fontSize: '13px' }}>
                <span style={{ color: '#f8fafc' }}>{req.profileName || 'Profil'} – „{req.eventTitle}“</span>
                <span style={{ color: req.status === 'approved' ? '#4ade80' : '#ef4444', fontWeight: 600 }}>
                  {req.status === 'approved' ? 'Freigegeben' : 'Abgelehnt'}
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
