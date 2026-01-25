import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getSystemInfo } from '../../client-ts/sdk.gen';
import { debugError, formatError } from '../../utils/logging';
import './SystemInfo.css';

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
      <div className="system-info-page">
        <h1>📊 {t('system.pageTitle')}</h1>
        <div className="loading-spinner">{t('common.loading')}</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="system-info-page">
        <h1>📊 {t('system.pageTitle')}</h1>
        <div className="error-message">⚠️ {error}</div>
      </div>
    );
  }

  if (!info) return null;

  return (
    <div className="system-info-page">
      <h1>📊 {t('system.receiverTitle')}</h1>

      <div className="info-grid">
        {/* Hardware Card */}
        <div className="info-card">
          <h2>📦 {t('system.hardware')}</h2>
          <div className="info-row">
            <span className="label">{t('system.brandModel')}:</span>
            <span className="value">{info.hardware.brand} {info.hardware.model}</span>
          </div>
          <div className="info-row">
            <span className="label">Chipset:</span>
            <span className="value">{info.hardware.chipsetDescription}</span>
          </div>
        </div>

        {/* Software Card */}
        <div className="info-card">
          <h2>💿 {t('system.software')}</h2>
          <div className="info-row">
            <span className="label">{t('system.distribution')}:</span>
            <span className="value">{info.software.imageDistro}</span>
          </div>
          <div className="info-row">
            <span className="label">{t('system.version')}:</span>
            <span className="value">{info.software.imageVersion}</span>
          </div>
          <div className="info-row">
            <span className="label">{t('system.kernel')}:</span>
            <span className="value">{info.software.kernelVersion}</span>
          </div>
          <div className="info-row">
            <span className="label">{t('system.webif')}:</span>
            <span className="value">{info.software.webifVersion}</span>
          </div>
        </div>

        {/* Tuners Card */}
        <div className="info-card info-card-wide">
          <h2>📡 {t('system.tuners')} ({info.tuners.length}x FBC)</h2>
          <div className="tuner-grid">
            {info.tuners.map((tuner, idx) => (
              <div key={idx} className={`tuner-item tuner-${tuner.status}`}>
                <div className="tuner-header">
                  <span className="tuner-number">#{idx + 1}</span>
                  <div className={`tuner-status-badge status-${tuner.status}`}>
                    {tuner.status === 'live' && '🟢 LIVE'}
                    {tuner.status === 'recording' && '🔴 REC'}
                    {tuner.status === 'streaming' && '🔵 STREAM'}
                    {tuner.status === 'idle' && '⚪ IDLE'}
                  </div>
                </div>
                <div className="tuner-type">{tuner.type.replace('DVB-', '')}</div>
              </div>
            ))}
          </div>
        </div>

        {/* Network Card */}
        {info.network.interfaces.length > 0 && (
          <div className="info-card">
            <h2>🌐 {t('system.network')}</h2>
            {info.network.interfaces.map((iface, idx) => (
              <div key={idx} className="info-section">
                <div className="info-row">
                  <span className="label">{iface.name}:</span>
                  <span className="value">{iface.type} ({iface.speed})</span>
                </div>
                <div className="info-row">
                  <span className="label">IPv4:</span>
                  <span className="value">{iface.ip || 'N/A'}</span>
                </div>
                {iface.ipv6 && (
                  <div className="info-row">
                    <span className="label">IPv6:</span>
                    <span className="value small">{iface.ipv6}</span>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Storage Card */}
        <div className="info-card">
          <h2>💾 {t('system.storage')}</h2>
          {info.storage.devices.length > 0 && (
            <div className="info-section">
              <h3>{t('system.drives')}</h3>
              {info.storage.devices.map((dev, idx) => (
                <div key={idx} className="info-row">
                  <div className="storage-status-header">
                    <span className={`status-dot dot-${dev.healthStatus}`} title={`${t('system.healthLabel')}: ${dev.healthStatus}`}></span>
                    <span className="label text-truncate" title={dev.model}>{dev.model}:</span>
                    <span className={`tag ${dev.isNas ? 'tag-nas' : 'tag-intern'}`}>
                      {dev.isNas ? 'NAS' : t('system.internal')}
                      {dev.fsType && <small> ({dev.fsType})</small>}
                    </span>
                  </div>
                  <div className="storage-subinfo">
                    <div className="storage-state-row">
                      <span className="value">{dev.capacity || t('common.notAvailable')}</span>
                      <span className={`access-badge access-${dev.access}`}>{dev.access === 'none' ? '–' : dev.access.toUpperCase()}</span>
                    </div>
                    {dev.checkedAt && (
                      <span className="checked-at">{t('system.check')}: {new Date(dev.checkedAt).toLocaleTimeString()}</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {info.storage.locations.length > 0 && (
            <div className="info-section">
              <h3>{t('system.paths')}</h3>
              {info.storage.locations.map((loc, idx) => (
                <div key={idx} className="info-row">
                  <div className="storage-status-header">
                    <span className={`status-dot dot-${loc.healthStatus}`} title={`${t('system.healthLabel')}: ${loc.healthStatus}`}></span>
                    <span className="value mono text-truncate" title={loc.mount}>{loc.mount}</span>
                    <span className={`tag ${loc.isNas ? 'tag-nas' : 'tag-intern'}`}>
                      {loc.isNas ? 'NAS' : t('system.internal')}
                      {loc.fsType && <small> ({loc.fsType})</small>}
                    </span>
                  </div>
                  <div className="storage-subinfo">
                    <span className={`status-text ${loc.healthStatus}`}>
                      {loc.mountStatus === 'mounted' ? (
                        loc.healthStatus === 'ok' ? `${t('system.status.mounted')} (${loc.access.toUpperCase()})` :
                          t(`system.status.${loc.healthStatus}`)
                      ) : t('system.status.unmounted')}
                    </span>
                    {loc.checkedAt && (
                      <span className="checked-at">{new Date(loc.checkedAt).toLocaleTimeString()}</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {info.storage.devices.length === 0 && info.storage.locations.length === 0 && (
            <div className="info-row">
              <span className="value italic">Keine Informationen verfügbar</span>
            </div>
          )}
        </div>

        {/* Runtime Card */}
        <div className="info-card">
          <h2>⏱️ Laufzeit</h2>
          <div className="info-row">
            <span className="label">Uptime:</span>
            <span className="value">{info.runtime.uptime}</span>
          </div>
        </div>

        {/* Resources Card */}
        <div className="info-card">
          <h2>📊 RAM</h2>
          <div className="ram-summary">
            <div className="ram-consumption">
              <span className="ram-used">{formatBytes(parseMemory(info.resource.memoryUsed))}</span>
              <span className="ram-label"> verbraucht</span>
            </div>
            <div className="ram-bar-container">
              <div
                className={`ram-bar-fill ${getRamColorClass(info.resource.memoryUsed, info.resource.memoryTotal)}`}
                style={{
                  width: `${calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%`
                }}
              />
            </div>
            <div className="ram-stats">
              <span className="ram-stat">
                <span className="ram-stat-label">{t('system.free')}</span>
                <span className="ram-stat-value">{formatBytes(parseMemory(info.resource.memoryAvailable))}</span>
              </span>
              <span className="ram-stat">
                <span className="ram-stat-label">{t('system.total')}</span>
                <span className="ram-stat-value">{formatBytes(parseMemory(info.resource.memoryUsed) + parseMemory(info.resource.memoryAvailable))}</span>
              </span>
              <span className="ram-stat">
                <span className="ram-stat-label">{t('system.usage')}</span>
                <span className="ram-stat-value">{calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%</span>
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

// Get RAM bar color based on usage
function getRamColorClass(usedStr: string, totalStr: string): string {
  const percent = calculateMemoryPercent(usedStr, totalStr);
  if (percent >= 85) return 'critical';
  if (percent >= 70) return 'warning';
  return 'normal';
}
