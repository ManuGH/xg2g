// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';
import styles from './ParentalControlSection.module.css';

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
    } catch {
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
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>Jugendschutz &amp; Live Freigaben</h3>
        <p className={styles.subheading}>
          Verwalten Sie ausstehende FSK-Freigabeanfragen von Kinderprofilen in Echtzeit.
        </p>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}
      {success && <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>}

      {/* Pending Approval Requests Section */}
      <div className={styles.panel}>
        <div className={styles.panelHeader}>
          <h4 className={styles.panelTitle}>
            <span>🔔</span> Ausstehende Freigabeanfragen ({pendingRequests.length})
          </h4>
          <button onClick={fetchApprovals} className={`${styles.button} ${styles.refreshButton}`}>
            🔄 Aktualisieren
          </button>
        </div>

        {loading ? (
          <div className={styles.loading}>Lade Freigaben...</div>
        ) : pendingRequests.length > 0 ? (
          <div className={styles.requestList}>
            {pendingRequests.map((req) => (
              <div key={req.id} className={styles.requestRow}>
                <div>
                  <div className={styles.requestWho}>
                    <span className={styles.requestAvatar}>👦</span>
                    <span className={styles.requestName}>{req.profileName || req.requesterName || 'Kinderprofil'}</span>
                    <span className={styles.ratingBadge}>FSK {req.eventRating}</span>
                  </div>
                  <div className={styles.requestWhat}>
                    Möchte „{req.eventTitle}“ {req.channelName ? `auf ${req.channelName}` : ''} ansehen
                  </div>
                  <div className={styles.requestWhen}>
                    Angefragt am {new Date(req.createdAt).toLocaleTimeString()} Uhr
                  </div>
                </div>

                <div className={styles.requestActions}>
                  <button
                    onClick={() => handleDeny(req.id)}
                    disabled={actionLoading === req.id}
                    className={`${styles.button} ${styles.buttonDeny}`}
                  >
                    Ablehnen
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'once')}
                    disabled={actionLoading === req.id}
                    className={`${styles.button} ${styles.buttonOnce}`}
                  >
                    1-malig erlauben
                  </button>
                  <button
                    onClick={() => handleApprove(req.id, 'always')}
                    disabled={actionLoading === req.id}
                    className={`${styles.button} ${styles.buttonAlways}`}
                  >
                    Immer freigeben
                  </button>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className={styles.emptyState}>
            Keine ausstehenden Freigabeanfragen. Alle Kinderprofile halten sich an ihre FSK-Grenzen.
          </div>
        )}
      </div>

      {/* Past Approvals History */}
      {pastRequests.length > 0 && (
        <div className={styles.panel}>
          <h4 className={styles.historyTitle}>Historie vergangener Entscheidungen</h4>
          <div className={styles.historyList}>
            {pastRequests.slice(0, 5).map((req) => (
              <div key={req.id} className={styles.historyRow}>
                <span className={styles.historyLabel}>{req.profileName || 'Profil'} – „{req.eventTitle}“</span>
                <span
                  className={`${styles.historyStatus} ${
                    req.status === 'approved' ? styles.historyApproved : styles.historyDenied
                  }`}
                >
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
