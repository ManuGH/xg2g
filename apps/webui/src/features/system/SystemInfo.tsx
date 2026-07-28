import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { type SystemInfoData as ApiSystemInfoData } from '../../client-ts';
import { useSystemInfo } from '../../hooks/useServerQueries';
import { StatusChip, type ChipState } from '../../components/ui';
import styles from './SystemInfo.module.css';

interface SystemInfoViewData {
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
      origin?: string;
      pathType?: string;
      mountStatus: 'mounted' | 'unmounted' | 'unknown';
      healthStatus: 'ok' | 'timeout' | 'error' | 'unknown' | 'skipped';
      access: 'none' | 'ro' | 'rw';
      isNas: boolean;
      fsType?: string;
      checkedAt?: string;
    }>;
    locations: Array<{
      model: string;
      capacity: string;
      mount: string;
      origin?: string;
      pathType?: string;
      mountStatus: 'mounted' | 'unmounted' | 'unknown';
      healthStatus: 'ok' | 'timeout' | 'error' | 'unknown' | 'skipped';
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
  const {
    data,
    error,
    isPending,
  } = useSystemInfo();

  if (isPending && !data) {
    return (
      <div className={styles.page}>
        <h1>{t('system.pageTitle')}</h1>
        <div className={styles.loading}>{t('common.loading')}</div>
      </div>
    );
  }

  if (error && !data) {
    const errorMessage = error instanceof Error ? error.message : 'Unbekannter Fehler';
    return (
      <div className={styles.page}>
        <h1>{t('system.pageTitle')}</h1>
        <div className={styles.error}>{t('system.loadError', { error: errorMessage })}</div>
      </div>
    );
  }

  if (!data) return null;

  const info = normalizeSystemInfo(data);

  return (
    <div className={styles.page}>
      <div className={styles.pageHeader}>
        <h1 className={styles.pageTitle}>{t('system.receiverTitle')}</h1>
      </div>

      <div className={styles.gridContainer}>
        {/* GERÄT */}
        <div className={styles.card}>
          <div className={styles.cardHeader}>
            <div className={styles.cardIcon}>📺</div>
            <h2 className={styles.cardTitle}>{t('system.hardware')}</h2>
          </div>
          <div className={styles.listGroup}>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.brandModel')}</span>
              <span className={styles.listItemValue}>{info.hardware.brand} {info.hardware.model}</span>
            </div>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.chipset')}</span>
              <span className={styles.listItemValue}>{info.hardware.chipsetDescription}</span>
            </div>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.uptime')}</span>
              <span className={styles.listItemValue}>{info.runtime.uptime}</span>
            </div>
          </div>
        </div>

        {/* SOFTWARE */}
        <div className={styles.card}>
          <div className={styles.cardHeader}>
            <div className={styles.cardIcon}>💻</div>
            <h2 className={styles.cardTitle}>{t('system.software')}</h2>
          </div>
          <div className={styles.listGroup}>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.distribution')}</span>
              <span className={styles.listItemValue}>{info.software.imageDistro}</span>
            </div>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.version')}</span>
              <span className={styles.listItemValue}>{info.software.imageVersion}</span>
            </div>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.kernel')}</span>
              <span className={styles.listItemValue}>{info.software.kernelVersion}</span>
            </div>
            <div className={styles.listItem}>
              <span className={styles.listItemLabel}>{t('system.webif')}</span>
              <span className={styles.listItemValue}>{info.software.webifVersion}</span>
            </div>
          </div>
        </div>

        {/* RESSOURCEN (RAM) */}
        <div className={styles.card}>
          <div className={styles.cardHeader}>
            <div className={styles.cardIcon}>⚡</div>
            <h2 className={styles.cardTitle}>{t('system.memory')}</h2>
          </div>
          <div className={styles.listGroup}>
            <div className={styles.listItemStorage}>
              <div className={styles.storageHeaderRow}>
                <span className={styles.listItemLabel}>RAM {t('system.memoryUsed')}</span>
                <span className={styles.listItemValue}>
                  {calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%
                </span>
              </div>
              <div className={styles.progressBarContainer}>
                <div 
                  className={styles.progressBarFill} 
                  style={{ '--xg2g-progress-width': `${calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%` } as CSSProperties}
                />
              </div>
              <div className={styles.storageTags}>
                <span className={styles.tag}>{formatBytes(parseMemory(info.resource.memoryAvailable))} {t('system.free').toLowerCase()}</span>
                <span className={styles.tag}>{formatBytes(parseMemory(info.resource.memoryTotal))} Total</span>
              </div>
            </div>
          </div>
        </div>

        {/* STORAGE */}
        <div className={styles.card}>
          <div className={styles.cardHeader}>
            <div className={styles.cardIcon}>💾</div>
            <h2 className={styles.cardTitle}>{t('system.sectionStorage')}</h2>
          </div>
          <div className={styles.listGroup}>
            {info.storage.devices.map((dev, idx) => (
              <div key={`dev-${idx}`} className={styles.listItemStorage}>
                <div className={styles.storageHeaderRow}>
                  <div className={styles.storageTitleWrap}>
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
                    <span className={[styles.listItemLabel, styles.textTruncate].join(' ')} title={dev.model}>
                      {dev.model}
                    </span>
                  </div>
                  <span className={styles.listItemValue}>{dev.capacity || t('common.notAvailable')}</span>
                </div>
                <div className={styles.storageTags}>
                  <span className={[styles.tag, styles.tagIntern].join(' ')}>
                    {resolveStorageOriginLabel(t, dev.origin)}
                  </span>
                  <span className={[styles.tag, dev.isNas ? styles.tagNas : styles.tagIntern].join(' ')}>
                    {resolveStoragePathTypeLabel(t, dev.pathType, dev.isNas)}
                    {dev.fsType && <small> ({dev.fsType})</small>}
                  </span>
                  <span
                    className={[
                      styles.tag,
                      styles.tagIntern,
                    ].join(' ')}
                  >
                    {dev.mountStatus === 'mounted' ? (
                      dev.healthStatus === 'ok' ? `${t('system.status.mounted')} (${dev.access.toUpperCase()})` :
                        t(`system.status.${dev.healthStatus}`)
                    ) : t('system.status.unmounted')}
                  </span>
                </div>
              </div>
            ))}
            
            {info.storage.locations.map((loc, idx) => (
              <div key={`loc-${idx}`} className={styles.listItemStorage}>
                <div className={styles.storageHeaderRow}>
                  <div className={styles.storageTitleWrap}>
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
                    <span className={[styles.listItemLabel, styles.textTruncate].join(' ')} title={loc.mount}>
                      {loc.mount}
                    </span>
                  </div>
                  <span className={styles.listItemValue}>
                    {loc.mountStatus === 'mounted' ? (
                      loc.healthStatus === 'ok' ? `${t('system.status.mounted')} (${loc.access.toUpperCase()})` :
                        t(`system.status.${loc.healthStatus}`)
                    ) : t('system.status.unmounted')}
                  </span>
                </div>
                <div className={styles.storageTags}>
                  <span className={[styles.tag, styles.tagIntern].join(' ')}>
                    {resolveStorageOriginLabel(t, loc.origin)}
                  </span>
                  <span className={[styles.tag, loc.isNas ? styles.tagNas : styles.tagIntern].join(' ')}>
                    {resolveStoragePathTypeLabel(t, loc.pathType, loc.isNas)}
                    {loc.fsType && <small> ({loc.fsType})</small>}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* NETZWERK */}
        {info.network.interfaces.length > 0 && (
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardIcon}>🌐</div>
              <h2 className={styles.cardTitle}>{t('system.network')}</h2>
            </div>
            <div className={styles.listGroup}>
              {info.network.interfaces.map((iface, idx) => (
                <div key={idx} className={styles.listItemStorage}>
                  <div className={styles.storageHeaderRow}>
                    <span className={styles.listItemLabel}>{iface.name}</span>
                    <span className={styles.listItemValue}>{iface.type} ({iface.speed})</span>
                  </div>
                  <div className={styles.storageTags}>
                    <span className={[styles.tag, styles.tagIntern].join(' ')}>IPv4: {iface.ip || t('common.notAvailable')}</span>
                    {iface.ipv6 && (
                      <span className={[styles.tag, styles.tagIntern].join(' ')}>IPv6: {iface.ipv6}</span>
                    )}
                    <span className={[styles.tag, styles.tagNas].join(' ')}>DHCP: {iface.dhcp ? t('common.yes') : t('common.no')}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* TUNER */}
        <div className={styles.listSection}>
          <h2 className={styles.listSectionTitle}>
            {t('system.tuners')} <span className={styles.sectionBadge}>{info.tuners.length}</span>
          </h2>
          <div className={styles.listGroup}>
            {info.tuners.map((tuner, idx) => (
              <div key={idx} className={styles.listItem}>
                <span className={styles.listItemLabel}>
                  Tuner #{idx + 1}
                  <span className={styles.tunerTypeLabel}>{tuner.type.replace('DVB-', '')}</span>
                </span>
                <span className={styles.listItemValue}>
                  <StatusChip {...getTunerStatusChip(tuner.status, t)} />
                </span>
              </div>
            ))}
          </div>
        </div>

      </div>
    </div>
  );
}

function getTunerStatusChip(status: string, t: (key: string, options?: Record<string, unknown>) => string): { state: ChipState; label: string } {
  switch (status) {
    case 'live':
      return { state: 'live', label: t('system.tunerStatus.live') };
    case 'recording':
      return { state: 'recording', label: t('system.tunerStatus.recording') };
    case 'streaming':
      return { state: 'success', label: t('system.tunerStatus.streaming') };
    case 'idle':
      return { state: 'idle', label: t('system.tunerStatus.idle') };
    default:
      return { state: 'warning', label: t('system.tunerStatus.unknown') };
  }
}

function normalizeSystemInfo(data: ApiSystemInfoData): SystemInfoViewData {
  return {
    hardware: {
      brand: data.hardware?.brand ?? '',
      model: data.hardware?.model ?? '',
      chipset: data.hardware?.chipset ?? '',
      chipsetDescription: data.hardware?.chipsetDescription ?? '',
    },
    software: {
      oeVersion: data.software?.oeVersion ?? '',
      imageDistro: data.software?.imageDistro ?? '',
      imageVersion: data.software?.imageVersion ?? '',
      enigmaVersion: data.software?.enigmaVersion ?? '',
      kernelVersion: data.software?.kernelVersion ?? '',
      driverDate: data.software?.driverDate ?? '',
      webifVersion: data.software?.webifVersion ?? '',
    },
    tuners: (data.tuners ?? []).map((tuner) => ({
      name: tuner.name ?? '',
      type: tuner.type ?? '',
      status: tuner.status ?? '',
    })),
    network: {
      interfaces: (data.network?.interfaces ?? []).map((iface) => ({
        name: iface.name ?? '',
        type: iface.type ?? '',
        speed: iface.speed ?? '',
        mac: iface.mac ?? '',
        ip: iface.ip ?? '',
        ipv6: iface.ipv6 ?? '',
        dhcp: iface.dhcp ?? false,
      })),
    },
    storage: {
      devices: (data.storage?.devices ?? []).map((device) => ({
        model: device.model ?? '',
        capacity: device.capacity ?? '',
        mount: device.mount ?? '',
        origin: device.origin,
        pathType: device.pathType,
        mountStatus: device.mountStatus ?? 'unknown',
        healthStatus: device.healthStatus ?? 'unknown',
        access: device.access ?? 'none',
        isNas: device.isNas ?? false,
        fsType: device.fsType,
        checkedAt: device.checkedAt,
      })),
      locations: (data.storage?.locations ?? []).map((location) => ({
        model: location.model ?? '',
        capacity: location.capacity ?? '',
        mount: location.mount ?? '',
        origin: location.origin,
        pathType: location.pathType,
        mountStatus: location.mountStatus ?? 'unknown',
        healthStatus: location.healthStatus ?? 'unknown',
        access: location.access ?? 'none',
        isNas: location.isNas ?? false,
        fsType: location.fsType,
        checkedAt: location.checkedAt,
      })),
    },
    runtime: {
      uptime: data.runtime?.uptime ?? '',
    },
    resource: {
      memoryTotal: data.resource?.memoryTotal ?? '0 kB',
      memoryAvailable: data.resource?.memoryAvailable ?? '0 kB',
      memoryUsed: data.resource?.memoryUsed ?? '0 kB',
    },
  };
}

function resolveStorageOriginLabel(
  t: ReturnType<typeof useTranslation>['t'],
  origin?: string,
): string {
  return origin === 'xg2g' ? t('system.storageOrigin.xg2g') : t('system.storageOrigin.receiver');
}

function resolveStoragePathTypeLabel(
  t: ReturnType<typeof useTranslation>['t'],
  pathType?: string,
  isNas?: boolean,
): string {
  switch (pathType) {
    case 'receiver_attached':
    case 'receiver_share':
    case 'xg2g_local':
    case 'xg2g_share':
    case 'xg2g_aggregate':
    case 'unknown':
      return t(`system.storageType.${pathType}`);
    default:
      return isNas ? t('system.nas') : t('system.internal');
  }
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
