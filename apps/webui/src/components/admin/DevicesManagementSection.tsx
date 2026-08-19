import React, { useState, useEffect } from 'react';
import { request } from '../../lib/api';
import styles from './DevicesManagementSection.module.css';

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
      const data = await request<DeviceData[]>('/api/v3/household/devices');
      setDevices(Array.isArray(data) ? data : []);
    } catch (err: any) {
      setError(err?.message || 'Geräte konnten nicht geladen werden.');
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
      await request(`/api/v3/household/devices/${encodeURIComponent(id)}/revoke`, { method: 'POST' });
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
    <div className={styles.section}>
      <div>
        <h3 className={styles.heading}>Verbundene Geräte &amp; 30-Tage-Vertrauen</h3>
        <p className={styles.subheading}>
          Übersicht aller registrierten Fernseher, Mobilgeräte und Browser mit DPoP-Schlüsselbindung.
        </p>
      </div>

      {error && <div className={`${styles.banner} ${styles.bannerError}`}>⚠️ {error}</div>}
      {success && <div className={`${styles.banner} ${styles.bannerSuccess}`}>✓ {success}</div>}

      {loading ? (
        <div className={styles.loading}>Geräte werden geladen...</div>
      ) : devices.length > 0 ? (
        <div className={styles.grid}>
          {devices.map((dev) => (
            <div key={dev.id} className={styles.card}>
              <div>
                <div className={styles.cardTop}>
                  <div className={styles.deviceIcon}>{getDeviceIcon(dev.deviceType)}</div>
                  <span className={styles.trustBadge}>✓ 30-Tage Vertrauen</span>
                </div>

                <h4 className={styles.deviceName}>{dev.name}</h4>
                <div className={styles.deviceType}>Typ: {dev.deviceType}</div>

                {dev.dpopThumbprint && (
                  <div className={styles.thumbprint}>
                    DPoP JWK: {dev.dpopThumbprint.substring(0, 14)}...
                  </div>
                )}
              </div>

              <div className={styles.cardActions}>
                {revokingId === dev.id ? (
                  <div className={styles.confirmGroup}>
                    <button
                      onClick={() => setRevokingId(null)}
                      className={`${styles.button} ${styles.buttonNeutral}`}
                    >
                      Abbrechen
                    </button>
                    <button
                      onClick={() => handleRevokeDevice(dev.id)}
                      className={`${styles.button} ${styles.buttonDangerSolid}`}
                    >
                      Widerrufen
                    </button>
                  </div>
                ) : (
                  <button
                    onClick={() => setRevokingId(dev.id)}
                    className={`${styles.button} ${styles.buttonDanger}`}
                  >
                    Zugriff widerrufen
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className={styles.emptyState}>
          Aktuell sind keine registrierten Android TV oder Mobilgeräte aktiv.
        </div>
      )}
    </div>
  );
};

export default DevicesManagementSection;
