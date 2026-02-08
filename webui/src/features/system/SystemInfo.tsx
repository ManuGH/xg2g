import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemInfo } from '../../client-ts/sdk.gen';
import { debugError, formatError } from '../../utils/logging';
import styles from './SystemInfo.module.css';

interface SystemInfoData {
  hardware: {
    brand: string;
    model: string;
    chipset: string;
    chipsetDescription: string;
  };
  software: {
    oeVersion: string;
    imageDistro: string;
    imageVersion: string;
    enigmaVersion: string;
    kernelVersion: string;
    driverDate: string;
    webifVersion: string;
  };
  tuners: Array<{
    name: string;
    type: string;
    status: string;
  }>;
  network: {
    interfaces: Array<{
      name: string;
      type: string;
      speed: string;
      mac: string;
      ip: string;
      ipv6: string;
      dhcp: boolean;
    }>;
  };
  storage: {
    devices: Array<{
      model: string;
      capacity: string;
      mount: string;
      mountStatus: 'mounted' | 'unmounted' | 'unknown';
      healthStatus: 'ok' | 'timeout' | 'error' | 'unknown';
      access: 'none' | 'ro' | 'rw';
      isNas: boolean;
      fsType?: string;
      checkedAt?: string;
    }>;
    locations: Array<{
      model: string;
      capacity: string;
      mount: string;
      mountStatus: 'mounted' | 'unmounted' | 'unknown';
      healthStatus: 'ok' | 'timeout' | 'error' | 'unknown';
      access: 'none' | 'ro' | 'rw';
      isNas: boolean;
      fsType?: string;
      checkedAt?: string;
    }>;
  };
  runtime: {
    uptime: string;
  };
  resource: {
    memoryTotal: string;
    memoryAvailable: string;
    memoryUsed: string;
  };
}

export function SystemInfo() {
  const { t } = useTranslation();
  const [info, setInfo] = useState<SystemInfoData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;

    const fetchData = async () => {
      try {
        const { data, error: apiError } = await getSystemInfo();

        if (!mounted) return;

        if (apiError) {
          throw new Error('Fehler beim Laden der System-Informationen');
        }

        if (data) {
          setInfo(data as SystemInfoData);
        }
        setLoading(false);
        setError(null);
      } catch (err) {
        if (!mounted) return;
        debugError('Failed to load system info:', formatError(err));
        setError((err as Error).message || 'Unbekannter Fehler');
        setLoading(false);
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 10000);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, []);

  if (loading) {
    return (
      <div className={styles.page}>
        <h1>{t('system.pageTitle')}</h1>
        <div className={styles.loading}>{t('common.loading')}</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className={styles.page}>
        <h1>{t('system.pageTitle')}</h1>
        <div className={styles.error}>Error: {error}</div>
      </div>
    );
  }

  if (!info) return null;

  const ramLevel = getRamLevel(info.resource.memoryUsed, info.resource.memoryTotal);

  return (
    <div className={styles.page}>
      <h1>{t('system.receiverTitle')}</h1>

      <div className={styles.grid}>
        {/* Hardware Card */}
        <div className={styles.card}>
          <h2>📦 {t('system.hardware')}</h2>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.brandModel')}:</span>
            <span className={styles.value}>{info.hardware.brand} {info.hardware.model}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>Chipset:</span>
            <span className={styles.value}>{info.hardware.chipsetDescription}</span>
          </div>
        </div>

        {/* Software Card */}
        <div className={styles.card}>
          <h2>💿 {t('system.software')}</h2>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.distribution')}:</span>
            <span className={styles.value}>{info.software.imageDistro}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.version')}:</span>
            <span className={styles.value}>{info.software.imageVersion}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.kernel')}:</span>
            <span className={styles.value}>{info.software.kernelVersion}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.webif')}:</span>
            <span className={styles.value}>{info.software.webifVersion}</span>
          </div>
        </div>

        {/* Tuners Card */}
        <div className={[styles.card, styles.cardWide].join(' ')}>
          <h2>📡 {t('system.tuners')} ({info.tuners.length}x FBC)</h2>
          <div className={styles.tunerGrid}>
            {info.tuners.map((tuner, idx) => (
              <div key={idx} className={styles.tunerItem}>
                <div className={styles.tunerHeader}>
                  <span className={styles.tunerNumber}>#{idx + 1}</span>
                  <div
                    className={[
                      styles.tunerStatusBadge,
                      tuner.status === 'live'
                        ? styles.statusLive
                        : tuner.status === 'recording'
                          ? styles.statusRecording
                          : tuner.status === 'streaming'
                            ? styles.statusStreaming
                            : styles.statusIdle,
                    ].join(' ')}
                  >
                    {tuner.status === 'live' && '🟢 LIVE'}
                    {tuner.status === 'recording' && '🔴 REC'}
                    {tuner.status === 'streaming' && '🔵 STREAM'}
                    {tuner.status === 'idle' && '⚪ IDLE'}
                  </div>
                </div>
                <div className={styles.tunerType}>{tuner.type.replace('DVB-', '')}</div>
              </div>
            ))}
          </div>
        </div>

        {/* Network Card */}
        {info.network.interfaces.length > 0 && (
          <div className={styles.card}>
            <h2>🌐 {t('system.network')}</h2>
            {info.network.interfaces.map((iface, idx) => (
              <div key={idx} className={styles.section}>
                <div className={styles.row}>
                  <span className={styles.label}>{iface.name}:</span>
                  <span className={styles.value}>{iface.type} ({iface.speed})</span>
                </div>
                <div className={styles.row}>
                  <span className={styles.label}>IPv4:</span>
                  <span className={styles.value}>{iface.ip || 'N/A'}</span>
                </div>
                {iface.ipv6 && (
                  <div className={styles.row}>
                    <span className={styles.label}>IPv6:</span>
                    <span className={[styles.value, styles.small].join(' ')}>{iface.ipv6}</span>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Storage Card */}
        <div className={styles.card}>
          <h2>💾 {t('system.storage')}</h2>
          {info.storage.devices.length > 0 && (
            <div className={styles.section}>
              <h3>{t('system.drives')}</h3>
              {info.storage.devices.map((dev, idx) => (
                <div key={idx} className={styles.row}>
                  <div className={styles.storageStatusHeader}>
                    <span
                      className={[
                        styles.statusDot,
                        dev.healthStatus === 'ok'
                          ? styles.dotOk
                          : dev.healthStatus === 'error'
                            ? styles.dotError
                            : dev.healthStatus === 'timeout'
                              ? styles.dotTimeout
                              : styles.dotUnknown,
                      ].join(' ')}
                      title={`${t('system.healthLabel')}: ${dev.healthStatus}`}
                    />
                    <span className={[styles.label, styles.textTruncate].join(' ')} title={dev.model}>
                      {dev.model}:
                    </span>
                    <span className={[styles.tag, dev.isNas ? styles.tagNas : styles.tagIntern].join(' ')}>
                      {dev.isNas ? 'NAS' : t('system.internal')}
                      {dev.fsType && <small> ({dev.fsType})</small>}
                    </span>
                  </div>
                  <div className={styles.storageSubinfo}>
                    <div className={styles.storageStateRow}>
                      <span className={styles.value}>{dev.capacity || t('common.notAvailable')}</span>
                      <span
                        className={[
                          styles.accessBadge,
                          dev.access === 'rw' ? styles.accessRw : dev.access === 'ro' ? styles.accessRo : null,
                        ].filter(Boolean).join(' ')}
                      >
                        {dev.access === 'none' ? '–' : dev.access.toUpperCase()}
                      </span>
                    </div>
                    {dev.checkedAt && (
                      <span className={styles.checkedAt}>{t('system.check')}: {new Date(dev.checkedAt).toLocaleTimeString()}</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {info.storage.locations.length > 0 && (
            <div className={styles.section}>
              <h3>{t('system.paths')}</h3>
              {info.storage.locations.map((loc, idx) => (
                <div key={idx} className={styles.row}>
                  <div className={styles.storageStatusHeader}>
                    <span
                      className={[
                        styles.statusDot,
                        loc.healthStatus === 'ok'
                          ? styles.dotOk
                          : loc.healthStatus === 'error'
                            ? styles.dotError
                            : loc.healthStatus === 'timeout'
                              ? styles.dotTimeout
                              : styles.dotUnknown,
                      ].join(' ')}
                      title={`${t('system.healthLabel')}: ${loc.healthStatus}`}
                    />
                    <span className={[styles.value, styles.mono, styles.textTruncate].join(' ')} title={loc.mount}>
                      {loc.mount}
                    </span>
                    <span className={[styles.tag, loc.isNas ? styles.tagNas : styles.tagIntern].join(' ')}>
                      {loc.isNas ? 'NAS' : t('system.internal')}
                      {loc.fsType && <small> ({loc.fsType})</small>}
                    </span>
                  </div>
                  <div className={styles.storageSubinfo}>
                    <span
                      className={[
                        styles.statusText,
                        loc.healthStatus === 'ok'
                          ? styles.textOk
                          : loc.healthStatus === 'error'
                            ? styles.textError
                            : loc.healthStatus === 'timeout'
                              ? styles.textTimeout
                              : null,
                      ].filter(Boolean).join(' ')}
                    >
                      {loc.mountStatus === 'mounted' ? (
                        loc.healthStatus === 'ok' ? `${t('system.status.mounted')} (${loc.access.toUpperCase()})` :
                          t(`system.status.${loc.healthStatus}`)
                      ) : t('system.status.unmounted')}
                    </span>
                    {loc.checkedAt && (
                      <span className={styles.checkedAt}>{new Date(loc.checkedAt).toLocaleTimeString()}</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {info.storage.devices.length === 0 && info.storage.locations.length === 0 && (
            <div className={styles.row}>
              <span className={[styles.value, styles.italic].join(' ')}>Keine Informationen verfügbar</span>
            </div>
          )}
        </div>

        {/* Runtime Card */}
        <div className={styles.card}>
          <h2>⏱️ Laufzeit</h2>
          <div className={styles.row}>
            <span className={styles.label}>Uptime:</span>
            <span className={styles.value}>{info.runtime.uptime}</span>
          </div>
        </div>

        {/* Resources Card */}
        <div className={styles.card}>
          <h2>📊 RAM</h2>
          <div className={styles.ramSummary}>
            <div className={styles.ramConsumption}>
              <span className={styles.ramUsed}>{formatBytes(parseMemory(info.resource.memoryUsed))}</span>
              <span className={styles.ramLabel}> verbraucht</span>
            </div>
            <div className={styles.ramBarContainer}>
              <div
                className={[
                  styles.ramBarFill,
                  ramLevel === 'critical'
                    ? styles.ramCritical
                    : ramLevel === 'warning'
                      ? styles.ramWarning
                      : styles.ramNormal
                ].join(' ')}
                style={{
                  width: `${calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%`
                }}
              />
            </div>
            <div className={styles.ramStats}>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.free')}</span>
                <span className={styles.ramStatValue}>{formatBytes(parseMemory(info.resource.memoryAvailable))}</span>
              </span>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.total')}</span>
                <span className={styles.ramStatValue}>{formatBytes(parseMemory(info.resource.memoryUsed) + parseMemory(info.resource.memoryAvailable))}</span>
              </span>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.usage')}</span>
                <span className={styles.ramStatValue}>{calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%</span>
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// Parse memory string like "757824 kB" to bytes
function parseMemory(memStr: string): number {
  const match = memStr.match(/(\d+)\s*(kB|MB|GB)?/i);
  if (!match || !match[1]) return 0;

  const value = parseInt(match[1], 10);
  let unit = 'kb';
  if (match[2]) {
    unit = match[2].toLowerCase();
  }

  switch (unit) {
    case 'kb': return value * 1024;
    case 'mb': return value * 1024 * 1024;
    case 'gb': return value * 1024 * 1024 * 1024;
    default: return value;
  }
}

// Format bytes to human-readable string
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';

  const gb = bytes / (1024 * 1024 * 1024);
  if (gb >= 1) {
    return `${gb.toFixed(2)} GB`;
  }

  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(0)} MB`;
}

// Calculate memory usage percentage
function calculateMemoryPercent(usedStr: string, totalStr: string): number {
  const used = parseMemory(usedStr);
  const total = parseMemory(totalStr);

  if (total === 0) return 0;
  return Math.round((used / total) * 100);
}

type RamLevel = 'normal' | 'warning' | 'critical';

function getRamLevel(usedStr: string, totalStr: string): RamLevel {
  const percent = calculateMemoryPercent(usedStr, totalStr);
  if (percent >= 85) return 'critical';
  if (percent >= 70) return 'warning';
  return 'normal';
}
