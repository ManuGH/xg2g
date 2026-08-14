// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { useState, useEffect } from 'react';

export interface DeviceData {
  id: string;
  name: string;
  deviceType: 'android_tv' | 'mobile' | 'web' | 'unknown';
  dpopThumbprint?: string;
  trustedUntil?: string;
  lastActiveAt?: string;
  ipAddress?: string;
}

export const DevicesManagementSection: React.FC = () => {
  const [devices, setDevices] = useState<DeviceData[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const fetchDevices = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/v3/household/devices');
      if (res.ok) {
        const data = await res.json();
        setDevices(Array.isArray(data) ? data : []);
      }
    } catch {
      setError('Geräte konnten nicht geladen werden.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchDevices();
  }, []);

  const handleRevokeDevice = async (id: string) => {
    setError(null);
    setSuccess(null);
    try {
      const res = await fetch(`/api/v3/household/devices/${id}/revoke`, { method: 'POST' });
      if (!res.ok) throw new Error('Gerätezugriff konnte nicht widerrufen werden.');
      setSuccess('Gerätezugriff wurde erfolgreich widerrufen.');
      setRevokingId(null);
      void fetchDevices();
    } catch (e: any) {
      setError(e.message || 'Fehler beim Widerrufen.');
    }
  };

  const getDeviceIcon = (type: string) => {
    switch (type) {
      case 'android_tv':
        return '📺';
      case 'mobile':
        return '📱';
      case 'web':
        return '💻';
      default:
        return '📟';
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
      <div>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600, color: 'var(--text-primary)' }}>Verbundene Geräte & 30-Tage-Vertrauen</h3>
        <p style={{ margin: '4px 0 0 0', fontSize: '13px', color: 'var(--text-tertiary)' }}>
          Übersicht aller registrierten Fernseher, Mobilgeräte und Browser mit DPoP-Schlüsselbindung.
        </p>
      </div>

      {error && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', color: 'var(--status-error)', fontSize: '13px' }}>
          ⚠️ {error}
        </div>
      )}
      {success && (
        <div style={{ padding: '12px 16px', borderRadius: '10px', backgroundColor: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', color: 'var(--status-success)', fontSize: '13px' }}>
          ✓ {success}
        </div>
      )}

      {loading ? (
        <div style={{ color: 'var(--text-tertiary)', fontSize: '13px', padding: '24px', textAlign: 'center' }}>Geräte werden geladen...</div>
      ) : devices.length > 0 ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: '16px' }}>
          {devices.map((dev) => (
            <div
              key={dev.id}
              style={{
                backgroundColor: 'var(--surface-panel-strong)',
                padding: '20px',
                borderRadius: '16px',
                border: '1px solid rgba(255,255,255,0.08)',
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'space-between',
              }}
            >
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <div style={{ fontSize: '32px', backgroundColor: 'var(--bg-base)', width: '48px', height: '48px', borderRadius: '12px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                    {getDeviceIcon(dev.deviceType)}
                  </div>
                  <span style={{ padding: '4px 10px', borderRadius: '12px', fontSize: '11px', fontWeight: 600, backgroundColor: 'rgba(34,197,94,0.15)', color: 'var(--status-success)' }}>
                    ✓ 30-Tage Vertrauen
                  </span>
                </div>

                <h4 style={{ margin: '12px 0 4px 0', fontSize: '16px', color: 'var(--text-primary)' }}>{dev.name}</h4>
                <div style={{ fontSize: '12px', color: 'var(--text-tertiary)', marginTop: '4px' }}>Typ: {dev.deviceType}</div>

                {dev.dpopThumbprint && (
                  <div style={{ fontSize: '11px', color: 'var(--text-disabled)', fontFamily: 'monospace', marginTop: '8px' }}>
                    DPoP JWK: {dev.dpopThumbprint.substring(0, 14)}...
                  </div>
                )}
              </div>

              <div style={{ marginTop: '16px', paddingTop: '12px', borderTop: '1px solid rgba(255,255,255,0.06)', display: 'flex', justifyContent: 'flex-end' }}>
                {revokingId === dev.id ? (
                  <div style={{ display: 'flex', gap: '6px' }}>
                    <button onClick={() => setRevokingId(null)} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--surface-highlight)', color: 'var(--text-secondary)', fontSize: '12px' }}>Abbrechen</button>
                    <button onClick={() => handleRevokeDevice(dev.id)} style={{ padding: '6px 10px', borderRadius: '8px', border: 'none', backgroundColor: 'var(--status-error)', color: 'var(--text-primary)', fontSize: '12px', fontWeight: 600 }}>Widerrufen</button>
                  </div>
                ) : (
                  <button onClick={() => setRevokingId(dev.id)} style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid rgba(239,68,68,0.3)', backgroundColor: 'rgba(239,68,68,0.1)', color: 'var(--status-error)', fontSize: '12px', cursor: 'pointer' }}>Zugriff widerrufen</button>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div style={{ backgroundColor: 'var(--surface-panel-strong)', padding: '32px', borderRadius: '16px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: '14px', border: '1px dashed rgba(255,255,255,0.1)' }}>
          Aktuell sind keine registrierten Android TV oder Mobilgeräte aktiv.
        </div>
      )}
    </div>
  );
};

export default DevicesManagementSection;
