import type { CSSProperties, ReactElement } from 'react';
import type { TFunction } from 'i18next';
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
  const { t, i18n } = useTranslation();
  const {
    data,
    error,
    isPending,
  } = useSystemInfo();

  if (isPending && !data) {
    return (
      <div className={styles.page}>
        <div className={styles.pageHeader}>
          <h1 className={styles.pageTitle}>{t('system.pageTitle')}</h1>
        </div>
        <div className={styles.loading}>{t('common.loading')}</div>
      </div>
    );
  }

  if (error && !data) {
    const errorMessage = error instanceof Error ? error.message : 'Unbekannter Fehler';
    return (
      <div className={styles.page}>
        <div className={styles.pageHeader}>
          <h1 className={styles.pageTitle}>{t('system.pageTitle')}</h1>
        </div>
        <div className={styles.error}>{t('system.loadError', { error: errorMessage })}</div>
      </div>
    );
  }

  if (!data) return null;

  const info = normalizeSystemInfo(data);
  const memUsedBytes = parseMemory(info.resource.memoryUsed);
  const memAvailBytes = parseMemory(info.resource.memoryAvailable);
  const memTotalBytes = parseMemory(info.resource.memoryTotal);
  const memPercent = calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal);
  const hasStorage = info.storage.devices.length > 0 || info.storage.locations.length > 0;
  const firstTunerType = info.tuners[0]?.type;
  const commonTunerType = firstTunerType && info.tuners.every((tu) => tu.type === firstTunerType)
    ? firstTunerType
    : '';

  return (
    <div className={styles.page}>
      <div className={styles.pageHeader}>
        <div className={styles.titleRow}>
          <h1 className={styles.pageTitle}>{t('system.receiverTitle')}</h1>
        </div>
        <div className={styles.summaryBadge}>
          <span className={styles.summaryDot} />
          <span>
            {t('system.summaryOnline')} · RAM {memPercent}% · {info.tuners.length} {t('system.tuners')} · {hasStorage ? t('system.summaryStorageConnected') : t('system.summaryStorageNotConnected')}
          </span>
        </div>
      </div>

      <div className={styles.columnsContainer}>
        {/* LINKER BLOCK: Hardware, RAM, Netzwerk */}
        <div className={styles.column}>
          {/* HARDWARE */}
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardHeaderLeft}>
                <div className={styles.cardIcon}>
                  <TvIcon />
                </div>
                <h2 className={styles.cardTitle}>{t('system.hardware')}</h2>
              </div>
            </div>
            <div className={styles.listGroup}>
              <div className={styles.listItem}>
                <span className={styles.listItemLabel}>{t('system.brandModel')}</span>
                <span className={styles.listItemValue}>
                  {formatHardwareModel(info.hardware.brand, info.hardware.model)}
                </span>
              </div>
              <div className={styles.listItem}>
                <span className={styles.listItemLabel}>{t('system.chipset')}</span>
                <span className={styles.listItemValue}>{info.hardware.chipsetDescription || info.hardware.chipset}</span>
              </div>
              <div className={styles.listItem}>
                <span className={styles.listItemLabel}>{t('system.uptime')}</span>
                <span className={styles.listItemValue}>{formatUptime(info.runtime.uptime, t)}</span>
              </div>
            </div>
          </div>

          {/* ARBEITSSPEICHER (RAM) */}
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardHeaderLeft}>
                <div className={styles.cardIcon}>
                  <CpuChipIcon />
                </div>
                <h2 className={styles.cardTitle}>{t('system.memory')}</h2>
              </div>
            </div>
            <div className={styles.ramCardContent}>
              <div className={styles.ramTopRow}>
                <span className={styles.ramPercent}>
                  {memPercent}% {t('system.memoryUsed')}
                </span>
                <span className={styles.ramSubtext}>
                  {formatBytes(memUsedBytes, i18n.language)} {t('system.used')} · {formatBytes(memAvailBytes, i18n.language)} {t('system.free').toLowerCase()}
                </span>
              </div>
              <div className={styles.progressBarContainer}>
                <div
                  className={styles.progressBarFill}
                  style={{ '--xg2g-progress-width': `${memPercent}%` } as CSSProperties}
                />
              </div>
              <div className={styles.ramFooterRow}>
                <span>{formatBytes(memUsedBytes, i18n.language)} {t('system.used')}</span>
                <span>{formatBytes(memAvailBytes, i18n.language)} {t('system.free').toLowerCase()}</span>
                <span>{formatBytes(memTotalBytes, i18n.language)} {t('system.total').toLowerCase()}</span>
              </div>
            </div>
          </div>

          {/* NETZWERK */}
          {info.network.interfaces.length > 0 && (
            <div className={styles.card}>
              <div className={styles.cardHeader}>
                <div className={styles.cardHeaderLeft}>
                  <div className={styles.cardIcon}>
                    <GlobeIcon />
                  </div>
                  <h2 className={styles.cardTitle}>{t('system.network')}</h2>
                </div>
              </div>
              <div className={styles.listGroup}>
                {info.network.interfaces.map((iface, idx) => (
                  <div key={idx} className={styles.networkCardItem}>
                    <div className={styles.networkHeaderRow}>
                      <span className={styles.networkName}>
                        {iface.type ? iface.type : iface.name}
                      </span>
                      <span className={styles.networkStatusPill}>
                        ● {t('system.networkConnected')}{iface.speed ? ` · ${iface.speed}` : ''}
                      </span>
                    </div>
                    <div className={styles.networkTable}>
                      <div className={styles.networkRow}>
                        <span className={styles.networkLabel}>{t('system.ipv4')}</span>
                        <span className={styles.networkValue}>{iface.ip || t('common.notAvailable')}</span>
                      </div>
                      {iface.ipv6 && (
                        <div className={styles.networkRow}>
                          <span className={styles.networkLabel}>{t('system.ipv6')}</span>
                          <span className={styles.networkValue}>{iface.ipv6}</span>
                        </div>
                      )}
                      <div className={styles.networkRow}>
                        <span className={styles.networkLabel}>{t('system.dhcp')}</span>
                        <span className={styles.networkValue}>{iface.dhcp ? t('common.yes') : t('common.no')}</span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* RECHTER BLOCK: Software, Speicher, Tuner */}
        <div className={styles.column}>
          {/* SOFTWARE */}
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardHeaderLeft}>
                <div className={styles.cardIcon}>
                  <CodeIcon />
                </div>
                <h2 className={styles.cardTitle}>{t('system.software')}</h2>
              </div>
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

          {/* STORAGE */}
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardHeaderLeft}>
                <div className={styles.cardIcon}>
                  <DatabaseIcon />
                </div>
                <h2 className={styles.cardTitle}>{t('system.sectionStorage')}</h2>
              </div>
            </div>
            <div className={styles.listGroup}>
              {info.storage.devices.map((dev, idx) => (
                <div key={`dev-${idx}`} className={styles.storageCardItem}>
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
                      <span className={[styles.storageName, styles.textTruncate].join(' ')} title={dev.model}>
                        {dev.model}
                      </span>
                    </div>
                    <span className={styles.storageCapacity} title={dev.capacity}>
                      {formatStorageCapacity(dev.capacity, i18n.language, t)}
                    </span>
                  </div>
                  {dev.healthStatus === 'error' && (
                    <div className={styles.storageErrorNotice}>
                      <span>⚠</span> {t('system.overlayError')}
                    </div>
                  )}
                  <div className={styles.storageTags}>
                    <span className={[styles.tag, styles.tagIntern].join(' ')}>
                      {resolveStorageOriginLabel(t, dev.origin)}
                    </span>
                    <span className={[styles.tag, dev.isNas ? styles.tagNas : styles.tagIntern].join(' ')}>
                      {resolveStoragePathTypeLabel(t, dev.pathType, dev.isNas)}
                      {dev.fsType && dev.fsType !== 'overlay' && <small> ({dev.fsType})</small>}
                    </span>
                    <span className={[styles.tag, styles.tagIntern].join(' ')}>
                      {dev.mountStatus === 'mounted' ? (
                        dev.healthStatus === 'ok' ? `${t('system.status.mounted')} (${dev.access.toUpperCase()})` :
                          t(`system.status.${dev.healthStatus}`)
                      ) : t('system.status.unmounted')}
                    </span>
                  </div>
                </div>
              ))}

              {info.storage.locations.map((loc, idx) => (
                <div key={`loc-${idx}`} className={styles.storageCardItem}>
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
                      <span className={[styles.storageName, styles.textTruncate].join(' ')} title={loc.mount}>
                        {loc.mount}
                      </span>
                    </div>
                    <span className={styles.storageCapacity}>
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

          {/* TUNER */}
          <div className={styles.card}>
            <div className={styles.cardHeader}>
              <div className={styles.cardHeaderLeft}>
                <div className={styles.cardIcon}>
                  <SignalIcon />
                </div>
                <h2 className={styles.cardTitle}>
                  {t('system.tuners')} <span className={styles.countBadge}>{info.tuners.length}</span>
                </h2>
              </div>
              {commonTunerType && (
                <span className={styles.subHardwareTag}>
                  {formatTunerHardware(commonTunerType)}
                </span>
              )}
            </div>
            <div className={styles.listGroup}>
              {info.tuners.map((tuner, idx) => (
                <div key={idx} className={styles.listItem}>
                  <span className={styles.listItemLabel}>
                    <span className={styles.tunerNumber}>
                      {t('system.tunerNumber', { number: idx + 1})}
                    </span>
                    <span className={styles.tunerTypeLabel}>
                      {tuner.type.replace('DVB-', '') || tuner.name}
                    </span>
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

const STORAGE_PATH_TYPE_KEYS = {
  receiver_attached: 'system.storageType.receiver_attached',
  receiver_share: 'system.storageType.receiver_share',
  xg2g_local: 'system.storageType.xg2g_local',
  xg2g_share: 'system.storageType.xg2g_share',
  xg2g_aggregate: 'system.storageType.xg2g_aggregate',
  unknown: 'system.storageType.unknown',
} as const;

function resolveStorageOriginLabel(
  t: TFunction,
  origin?: string,
): string {
  return origin === 'xg2g' ? t('system.storageOrigin.xg2g') : t('system.storageOrigin.receiver');
}

function resolveStoragePathTypeLabel(
  t: TFunction,
  pathType?: string,
  isNas?: boolean,
): string {
  if (pathType && pathType in STORAGE_PATH_TYPE_KEYS) {
    return t(STORAGE_PATH_TYPE_KEYS[pathType as keyof typeof STORAGE_PATH_TYPE_KEYS]);
  }
  return isNas ? t('system.nas') : t('system.internal');
}

export function formatHardwareModel(brand: string, model: string): string {
  let normalizedBrand = brand.trim();
  if (normalizedBrand.toLowerCase() === 'vu+') {
    normalizedBrand = 'VU+';
  }
  let normalizedModel = model.trim();
  normalizedModel = normalizedModel
    .replace(/^Uno4K(\b|se)?/i, (_m, se) => se ? 'Uno 4K SE' : 'Uno 4K')
    .replace(/^Duo4K(\b|se)?/i, (_m, se) => se ? 'Duo 4K SE' : 'Duo 4K')
    .replace(/^Zero4K/i, 'Zero 4K')
    .replace(/^Ultimo4K/i, 'Ultimo 4K');

  if (!normalizedBrand) return normalizedModel;
  if (!normalizedModel) return normalizedBrand;
  if (normalizedModel.toLowerCase().startsWith(normalizedBrand.toLowerCase())) {
    return normalizedModel;
  }
  return `${normalizedBrand} ${normalizedModel}`;
}

export function formatUptime(uptimeStr: string, t: TFunction): string {
  if (!uptimeStr) return '';
  const trimmed = uptimeStr.trim();
  const timeMatch = trimmed.match(/^(\d{1,2}):(\d{2})(?::\d{2})?$/);
  if (timeMatch && timeMatch[1] !== undefined && timeMatch[2] !== undefined) {
    const hours = parseInt(timeMatch[1], 10);
    const minutes = parseInt(timeMatch[2], 10);
    if (hours === 0) {
      return `${minutes} ${t('system.minutes')}`;
    }
    return `${hours} ${t('system.hours')} ${minutes} ${t('system.minutes')}`;
  }
  const dayMatch = trimmed.match(/^(\d+)\s*(?:d|days?|tage?)?[,\s]+(\d{1,2}):(\d{2})/i);
  if (dayMatch && dayMatch[1] !== undefined && dayMatch[2] !== undefined) {
    const days = parseInt(dayMatch[1], 10);
    const hours = parseInt(dayMatch[2], 10);
    return `${days} ${t('system.days')} ${hours} ${t('system.hours')}`;
  }
  return trimmed;
}

export function formatStorageCapacity(rawCapacity: string, locale: string, t: TFunction): string {
  if (!rawCapacity) return t('common.notAvailable');
  const match = rawCapacity.match(/([\d.,]+)\s*([GMK]B)\s*(?:frei|free)?\s*[/:]\s*([\d.,]+)\s*([GMK]B)/i);
  if (match && match[1] && match[2] && match[3] && match[4]) {
    let freeNum = match[1];
    const freeUnit = match[2];
    let totalNum = match[3];
    const totalUnit = match[4];
    if (locale.startsWith('de')) {
      freeNum = freeNum.replace('.', ',');
      totalNum = totalNum.replace('.', ',');
    } else {
      freeNum = freeNum.replace(',', '.');
      totalNum = totalNum.replace(',', '.');
    }
    return t('system.freeOf', { free: `${freeNum} ${freeUnit}`, total: `${totalNum} ${totalUnit}` });
  }
  if (locale.startsWith('de')) {
    return rawCapacity.replace(/(\d+)\.(\d+)/g, '$1,$2');
  }
  return rawCapacity;
}

export function formatTunerHardware(typeStr: string): string {
  if (typeStr.includes('FBC') && typeStr.includes('DVB-S')) {
    return 'DVB-S2 · FBC Multi-Tuner';
  }
  if (typeStr.includes('FBC') && typeStr.includes('DVB-C')) {
    return 'DVB-C · FBC Multi-Tuner';
  }
  return typeStr.replace('DVB-', '');
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

// Format bytes to human-readable string with locale formatting
function formatBytes(bytes: number, locale = 'de'): string {
  if (bytes === 0) return '0 B';

  const gb = bytes / (1024 * 1024 * 1024);
  if (gb >= 1) {
    const num = locale.startsWith('de') ? gb.toFixed(1).replace('.', ',') : gb.toFixed(1);
    return `${num} GB`;
  }

  const mb = Math.round(bytes / (1024 * 1024));
  return `${mb} MB`;
}

// Calculate memory usage percentage
function calculateMemoryPercent(usedStr: string, totalStr: string): number {
  const used = parseMemory(usedStr);
  const total = parseMemory(totalStr);

  if (total === 0) return 0;
  return Math.round((used / total) * 100);
}

// SVG Line Icons
function TvIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <rect x="2" y="7" width="20" height="15" rx="2" ry="2" />
      <polyline points="17 2 12 7 7 2" />
    </svg>
  );
}

function CodeIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <polyline points="16 18 22 12 16 6" />
      <polyline points="8 6 2 12 8 18" />
    </svg>
  );
}

function CpuChipIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <rect x="9" y="9" width="6" height="6" />
      <line x1="9" y1="1" x2="9" y2="4" />
      <line x1="15" y1="1" x2="15" y2="4" />
      <line x1="9" y1="20" x2="9" y2="23" />
      <line x1="15" y1="20" x2="15" y2="23" />
      <line x1="20" y1="9" x2="23" y2="9" />
      <line x1="20" y1="15" x2="23" y2="15" />
      <line x1="1" y1="9" x2="4" y2="9" />
      <line x1="1" y1="15" x2="4" y2="15" />
    </svg>
  );
}

function DatabaseIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <ellipse cx="12" cy="5" rx="9" ry="3" />
      <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
      <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
    </svg>
  );
}

function GlobeIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <circle cx="12" cy="12" r="10" />
      <line x1="2" y1="12" x2="22" y2="12" />
      <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
    </svg>
  );
}

function SignalIcon(): ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={styles.headerIcon} aria-hidden="true">
      <path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9" />
      <path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5" />
      <circle cx="12" cy="12" r="2" />
      <path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5" />
      <path d="M19.1 4.9C23 8.8 23 15.2 19.1 19.1" />
    </svg>
  );
}
