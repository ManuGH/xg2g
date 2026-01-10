import { useEffect, useState } from 'react';
import { getSystemInfo } from '../../client-ts/sdk.gen';
import './SystemInfo.css';

interface SystemInfoData {
  hardware: {
    brand: string;
    model: string;
    chipset: string;
    chipset_description: string;
  };
  software: {
    oe_version: string;
    image_distro: string;
    image_version: string;
    enigma_version: string;
    kernel_version: string;
    driver_date: string;
    webif_version: string;
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
    }>;
  };
  runtime: {
    uptime: string;
  };
  resource: {
    memory_total: string;
    memory_available: string;
    memory_used: string;
  };
}

export function SystemInfo() {
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
        console.error('Failed to load system info:', err);
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
        <h1>📊 System Information</h1>
        <div className="loading-spinner">Lade Receiver-Daten...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="system-info-page">
        <h1>📊 System Information</h1>
        <div className="error-message">⚠️ {error}</div>
      </div>
    );
  }

  if (!info) return null;

  return (
    <div className="system-info-page">
      <h1>📊 Receiver Information</h1>

      <div className="info-grid">
        {/* Hardware Card */}
        <div className="info-card">
          <h2>📦 Hardware</h2>
          <div className="info-row">
            <span className="label">Marke & Modell:</span>
            <span className="value">{info.hardware.brand} {info.hardware.model}</span>
          </div>
          <div className="info-row">
            <span className="label">Chipset:</span>
            <span className="value">{info.hardware.chipset_description}</span>
          </div>
        </div>

        {/* Software Card */}
        <div className="info-card">
          <h2>💿 Software</h2>
          <div className="info-row">
            <span className="label">Distribution:</span>
            <span className="value">{info.software.image_distro}</span>
          </div>
          <div className="info-row">
            <span className="label">Version:</span>
            <span className="value">{info.software.image_version}</span>
          </div>
          <div className="info-row">
            <span className="label">Kernel:</span>
            <span className="value">{info.software.kernel_version}</span>
          </div>
          <div className="info-row">
            <span className="label">WebIF:</span>
            <span className="value">{info.software.webif_version}</span>
          </div>
        </div>

        {/* Tuners Card */}
        <div className="info-card info-card-wide">
          <h2>📡 Tuner ({info.tuners.length}x FBC)</h2>
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
            <h2>🌐 Netzwerk</h2>
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
        {info.storage.devices.length > 0 && (
          <div className="info-card">
            <h2>💾 Speicher</h2>
            {info.storage.devices.map((dev, idx) => (
              <div key={idx} className="info-row">
                <span className="label">{dev.model}:</span>
                <span className="value">{dev.capacity}</span>
              </div>
            ))}
          </div>
        )}

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
              <span className="ram-used">{formatBytes(parseMemory(info.resource.memory_used))}</span>
              <span className="ram-label"> verbraucht</span>
            </div>
            <div className="ram-bar-container">
              <div
                className={`ram-bar-fill ${getRamColorClass(info.resource.memory_used, info.resource.memory_total)}`}
                style={{
                  width: `${calculateMemoryPercent(info.resource.memory_used, info.resource.memory_total)}%`
                }}
              />
            </div>
            <div className="ram-stats">
              <span className="ram-stat">
                <span className="ram-stat-label">Frei</span>
                <span className="ram-stat-value">{formatBytes(parseMemory(info.resource.memory_available))}</span>
              </span>
              <span className="ram-stat">
                <span className="ram-stat-label">Total</span>
                <span className="ram-stat-value">{formatBytes(parseMemory(info.resource.memory_used) + parseMemory(info.resource.memory_available))}</span>
              </span>
              <span className="ram-stat">
                <span className="ram-stat-label">Nutzung</span>
                <span className="ram-stat-value">{calculateMemoryPercent(info.resource.memory_used, info.resource.memory_available)}%</span>
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
