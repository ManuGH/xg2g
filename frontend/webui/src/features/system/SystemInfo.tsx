import { useTranslation } from 'react-i18next';
import { type SystemInfoData as ApiSystemInfoData } from '../../client-ts';
import { useSystemInfo } from '../../hooks/useServerQueries';
import { Card, CardBody, StatusChip, type ChipState } from '../../components/ui';
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
  const ramLevel = getRamLevel(info.resource.memoryUsed, info.resource.memoryTotal);

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <p className={styles.kicker}>{t('nav.system')}</p>
          <h1>{t('system.receiverTitle')}</h1>
          <p className={styles.subtitle}>{t('system.subtitle')}</p>
        </div>
      </div>

      <div className={styles.grid}>
        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionOverview')}</p>
                <h2 className={styles.cardTitle}>{t('system.hardware')}</h2>
              </div>
            </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.brandModel')}:</span>
            <span className={styles.value}>{info.hardware.brand} {info.hardware.model}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.chipset')}:</span>
            <span className={styles.value}>{info.hardware.chipsetDescription}</span>
          </div>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionOverview')}</p>
                <h2 className={styles.cardTitle}>{t('system.software')}</h2>
              </div>
            </div>
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
          </CardBody>
        </Card>

        <Card className={[styles.card, styles.cardWide].join(' ')}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionLive')}</p>
                <h2 className={styles.cardTitle}>{t('system.tuners')}</h2>
              </div>
              <StatusChip state="idle" label={t('system.tunersDetected', { count: info.tuners.length })} />
            </div>
          <div className={styles.tunerGrid}>
            {info.tuners.map((tuner, idx) => (
              <div key={idx} className={styles.tunerItem}>
                <div className={styles.tunerHeader}>
                  <span className={styles.tunerNumber}>#{idx + 1}</span>
                  <StatusChip {...getTunerStatusChip(tuner.status, t)} />
                </div>
                <div className={styles.tunerType}>{tuner.type.replace('DVB-', '')}</div>
              </div>
            ))}
          </div>
          </CardBody>
        </Card>

        {info.network.interfaces.length > 0 && (
          <Card className={styles.card}>
            <CardBody className={styles.cardBody}>
              <div className={styles.cardHeader}>
                <div>
                  <p className={styles.cardEyebrow}>{t('system.sectionConnectivity')}</p>
                  <h2 className={styles.cardTitle}>{t('system.network')}</h2>
                </div>
                <StatusChip state="idle" label={t('system.interfaces', { count: info.network.interfaces.length })} />
              </div>
            {info.network.interfaces.map((iface, idx) => (
              <div key={idx} className={styles.section}>
                <div className={styles.row}>
                  <span className={styles.label}>{iface.name}:</span>
                  <span className={styles.value}>{iface.type} ({iface.speed})</span>
                </div>
                <div className={styles.row}>
                  <span className={styles.label}>{t('system.ipv4')}:</span>
                  <span className={styles.value}>{iface.ip || t('common.notAvailable')}</span>
                </div>
                {iface.ipv6 && (
                  <div className={styles.row}>
                    <span className={styles.label}>{t('system.ipv6')}:</span>
                    <span className={[styles.value, styles.small].join(' ')}>{iface.ipv6}</span>
                  </div>
                )}
              </div>
            ))}
            </CardBody>
          </Card>
        )}

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionStorage')}</p>
                <h2 className={styles.cardTitle}>{t('system.storage')}</h2>
              </div>
              <StatusChip
                state="idle"
                label={t('system.storageLocations', { count: info.storage.devices.length + info.storage.locations.length })}
              />
            </div>
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
                      {dev.isNas ? t('system.nas') : t('system.internal')}
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
                        {t(`system.access.${dev.access}`)}
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
                      {loc.isNas ? t('system.nas') : t('system.internal')}
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
              <span className={[styles.value, styles.italic].join(' ')}>{t('system.noInformation')}</span>
            </div>
          )}
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionHealth')}</p>
                <h2 className={styles.cardTitle}>{t('system.runtime')}</h2>
              </div>
            </div>
          <div className={styles.row}>
            <span className={styles.label}>{t('system.uptime')}:</span>
            <span className={styles.value}>{info.runtime.uptime}</span>
          </div>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <div className={styles.cardHeader}>
              <div>
                <p className={styles.cardEyebrow}>{t('system.sectionHealth')}</p>
                <h2 className={styles.cardTitle}>{t('system.memory')}</h2>
              </div>
              <StatusChip state={getRamChipState(ramLevel)} label={t(`system.memoryLevel.${ramLevel}`)} />
            </div>
          <div className={styles.ramSummary}>
            <div className={styles.ramConsumption}>
              <span className={styles.ramUsed}>{formatBytes(parseMemory(info.resource.memoryUsed))}</span>
              <span className={styles.ramLabel}>{t('system.memoryUsed')}</span>
            </div>
            <div className={styles.ramBarContainer}>
              <svg
                width="100%"
                height="100%"
                viewBox="0 0 100 10"
                preserveAspectRatio="none"
                role="img"
                aria-label={t('system.usage')}
              >
                <rect
                  x="0"
                  y="0"
                  width={calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}
                  height="10"
                  rx="5"
                  ry="5"
                  fill={
                    ramLevel === 'critical'
                      ? 'var(--status-error)'
                      : ramLevel === 'warning'
                        ? 'var(--status-warning)'
                        : 'var(--status-success)'
                  }
                />
              </svg>
            </div>
            <div className={styles.ramStats}>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.free')}</span>
                <span className={styles.ramStatValue}>{formatBytes(parseMemory(info.resource.memoryAvailable))}</span>
              </span>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.total')}</span>
                <span className={styles.ramStatValue}>{formatBytes(parseMemory(info.resource.memoryTotal))}</span>
              </span>
              <span className={styles.ramStat}>
                <span className={styles.ramStatLabel}>{t('system.usage')}</span>
                <span className={styles.ramStatValue}>{calculateMemoryPercent(info.resource.memoryUsed, info.resource.memoryTotal)}%</span>
              </span>
            </div>
          </div>
          </CardBody>
        </Card>
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

function getRamChipState(level: RamLevel): ChipState {
  switch (level) {
    case 'critical':
      return 'error';
    case 'warning':
      return 'warning';
    default:
      return 'success';
  }
}

function getRamLevel(usedStr: string, totalStr: string): RamLevel {
  const percent = calculateMemoryPercent(usedStr, totalStr);
  if (percent >= 85) return 'critical';
  if (percent >= 70) return 'warning';
  return 'normal';
}
