// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import Config, { isConfigured } from './Config';
import Files from './Files';
import Logs from './Logs';
import SectionContextBar from './SectionContextBar';
import {
  approvePairing,
  type AppConfig,
  type ConnectivityContract,
} from '../client-ts';
import {
  useSystemConfig,
  useSystemConnectivity,
  useSystemScanStatus,
  useTriggerSystemScanMutation,
} from '../hooks/useServerQueries';
import { usePendingChanges } from '../context/PendingChangesContext';
import { getClientAuthToken } from '../services/clientWrapper';
import { debugError, formatError } from '../utils/logging';
import {
  buildSettingsRoute,
  type SettingsSection,
  type SettingsTool,
} from '../routes';
import { getSettingsSectionLabel, getSettingsToolLabel } from '../lib/routeContext';
import { Button } from './ui';
import styles from './Settings.module.css';

import SecuritySettingsSection from './settings/SecuritySettingsSection';
import { AdminLayout } from './admin/AdminLayout';

const SETTINGS_SECTIONS: SettingsSection[] = [
  'setup',
  'security',
  'household',
  'android-tv',
  'scan',
  'streaming',
  'advanced',
];

const SETTINGS_TOOLS: SettingsTool[] = ['files', 'logs'];

function isSettingsSection(value: string | null): value is SettingsSection {
  return value !== null && SETTINGS_SECTIONS.includes(value as SettingsSection);
}

function isSettingsTool(value: string | null): value is SettingsTool {
  return value !== null && SETTINGS_TOOLS.includes(value as SettingsTool);
}

// Deployment assumption: published endpoints are ORIGIN-ONLY, so xg2g is not
// supported under a deployment sub-path today. The backend rejects any endpoint
// URL carrying a path ("only origin URLs are allowed", see
// backend/internal/domain/connectivity/published_endpoints.go `parseEndpointURL`)
// and strips the path again during canonicalization, and every reference
// topology in docs/ops/PUBLIC_DEPLOYMENT_CONTRACT.md proxies the site root.
//
// `ui/` is nevertheless derived *relative* to the endpoint rather than by
// resolving the absolute path `/ui/`, so that assumption stays non-load-bearing:
// an absolute path silently replaces the deployment root (`https://host/xg2g/`
// would become `https://host/ui/`), which would hand the native client a URL
// that does not exist. `/ui` and `/api/v3` are siblings below the same root
// (backend/internal/api/server_routes_wiring.go, `V3BaseURL`), so both must be
// derived downward from it — never by overwriting one another's path.
function deriveUiUrl(endpointUrl: string): string {
  try {
    const root = new URL(endpointUrl);
    if (!root.pathname.endsWith('/')) {
      root.pathname = `${root.pathname}/`;
    }
    return new URL('ui/', root).toString();
  } catch {
    return '';
  }
}

export function resolveAndroidTvBaseUrl(
  config: AppConfig | null,
  contract: ConnectivityContract | null,
): string {
  const contractNativeUrl = contract?.public
    ? contract.selections.nativePublic.endpoint?.url
    : contract?.selections.native.endpoint?.url;
  if (contractNativeUrl) {
    return deriveUiUrl(contractNativeUrl);
  }

  const profile = config?.connectivity?.profile ?? 'lan';
  const configuredNativeUrl = config?.connectivity?.publishedEndpoints
    ?.find((endpoint) => endpoint.allowNative && (profile === 'lan' || endpoint.kind === 'public_https'))
    ?.url;
  if (configuredNativeUrl) {
    return deriveUiUrl(configuredNativeUrl);
  }

  if (profile !== 'lan' || contract?.public) {
    return '';
  }

  if (typeof window === 'undefined') {
    return '';
  }
  return deriveUiUrl(window.location.origin);
}

function Settings() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { search } = useLocation();
  const { confirmPendingChanges } = usePendingChanges();

  const [scanError, setScanError] = useState<string | null>(null);
  const [showSetup, setShowSetup] = useState<boolean>(false);
  const [pairingCodeDraft, setPairingCodeDraft] = useState<string>('');
  const [pairingSubmitting, setPairingSubmitting] = useState<boolean>(false);
  const [pairingFeedback, setPairingFeedback] = useState<{ success: boolean; message: string } | null>(null);
  const {
    data: config = null,
    refetch: refetchConfig,
  } = useSystemConfig();
  const { data: connectivity = null } = useSystemConnectivity();
  const {
    data: scanStatus = null,
    error: scanStatusError,
    refetch: refetchScanStatus,
  } = useSystemScanStatus();
  const triggerScanMutation = useTriggerSystemScanMutation();
  const androidTvBaseUrl = useMemo(() => {
    return resolveAndroidTvBaseUrl(config, connectivity);
  }, [config, connectivity]);
  const androidTvLaunchUrl = useMemo(() => {
    if (!androidTvBaseUrl) {
      return '';
    }
    const params = new URLSearchParams({ base_url: androidTvBaseUrl });
    const authToken = getClientAuthToken();
    if (authToken) {
      params.set('auth_token', authToken);
    }
    return `xg2g://connect?${params.toString()}`;
  }, [androidTvBaseUrl]);
  const androidTvBlockingFinding = useMemo(() => {
    if (!connectivity?.pairingBlocked) {
      return null;
    }
    return connectivity.findings.find((finding) => {
      return (finding.severity === 'fatal' || finding.severity === 'degraded')
        && finding.scopes.includes('pairing');
    }) ?? null;
  }, [connectivity]);
  const androidTvPublicMode = connectivity?.public ?? ((config?.connectivity?.profile ?? 'lan') !== 'lan');
  const androidTvBaseUrlDisplay = androidTvBaseUrl || t('settings.androidTv.unavailableValue', {
    defaultValue: 'No published native endpoint',
  });
  const androidTvLaunchDisabled = !androidTvLaunchUrl || Boolean(androidTvBlockingFinding);
  const androidTvLaunchHint = androidTvBlockingFinding?.detail
    ?? androidTvBlockingFinding?.summary
    ?? (
      !androidTvLaunchUrl && androidTvPublicMode
        ? t('settings.androidTv.unavailableReason', {
          defaultValue: 'No published native endpoint is available for the current deployment contract.',
        })
        : t('settings.androidTv.hint')
    );

  const configured = isConfigured(config);
  const [audioMode, setAudioMode] = useState<'stereo' | 'surround'>(() => {
    try {
      return (localStorage.getItem('xg2g.settings.audioMode') as 'stereo' | 'surround') || 'stereo';
    } catch {
      return 'stereo';
    }
  });
  const [dvrMode, setDvrMode] = useState<'live_only' | '1h' | '2h' | '4h'>(() => {
    try {
      const stored = localStorage.getItem('xg2g.settings.dvrMode');
      if (stored === 'live_only' || stored === '1h' || stored === '2h' || stored === '4h') {
        return stored;
      }
      return '2h';
    } catch {
      return '2h';
    }
  });
  const searchParams = useMemo(() => new URLSearchParams(search), [search]);
  const requestedSection = searchParams.get('section');
  const requestedTool = searchParams.get('tool');
  const activeSection: SettingsSection = !configured
    ? 'setup'
    : isSettingsSection(requestedSection)
      ? requestedSection
      : 'setup';
   const activeTool: SettingsTool | null = configured
    && activeSection === 'advanced'
    && isSettingsTool(requestedTool)
    ? requestedTool
    : null;
  const scanStatusErrorMessage = !scanStatus
    ? scanError ?? (
      scanStatusError instanceof Error
        ? scanStatusError.message
        : scanStatusError
          ? t('settings.streaming.scan.errors.loadStatus')
          : null
    )
    : scanError;
  const showContextBar = true;
  const showSection = (section: SettingsSection) => {
    return activeSection === section;
  };
  const sectionLabelMap: Record<SettingsSection, string> = {
    setup: getSettingsSectionLabel('setup', t),
    household: getSettingsSectionLabel('household', t),
    'android-tv': getSettingsSectionLabel('android-tv', t),
    scan: getSettingsSectionLabel('scan', t),
    streaming: getSettingsSectionLabel('streaming', t),
    security: getSettingsSectionLabel('security', t),
    advanced: getSettingsSectionLabel('advanced', t),
  };
  const toolLabelMap: Record<SettingsTool, string> = {
    files: getSettingsToolLabel('files', t),
    logs: getSettingsToolLabel('logs', t),
  };
  const headerTitle = activeTool
    ? toolLabelMap[activeTool]
    : sectionLabelMap[activeSection];
  const headerSubtitle = activeTool
    ? t(`settings.context.tool.${activeTool}`, {
      defaultValue: activeTool === 'files'
        ? 'Playlist, guide and compatibility feeds now live under the advanced settings area.'
        : 'Diagnostics and recent server events now live under the advanced settings area.',
    })
    : t(`settings.context.section.${activeSection}`, {
      defaultValue: 'This area is part of Settings and can also be reached directly by URL.',
    });
  const handleApprovePairing = async () => {
    const code = pairingCodeDraft.trim().toUpperCase();
    if (!code || pairingSubmitting) return;
    setPairingSubmitting(true);
    setPairingFeedback(null);
    try {
      const res = await approvePairing({
        path: { pairingId: code },
        body: {}
      });
      if (res.data?.status === 'approved') {
        setPairingFeedback({ success: true, message: '✅ Gerät erfolgreich autorisiert! Der TV-Stream öffnet sich jetzt auf dem Gerät.' });
        setPairingCodeDraft('');
      } else {
        setPairingFeedback({ success: false, message: 'Kopplung konnte nicht autorisiert werden.' });
      }
    } catch (e: any) {
      const msg = e?.message || '';
      if (msg.includes('Authentication required') || e?.status === 401) {
        setPairingFeedback({
          success: false,
          message: '🔐 Admin-Authentifizierung erforderlich. Bitte gehe zuerst auf den Tab "Sicherheit" und melde dich mit dem Admin-Token (test04) an.'
        });
      } else {
        setPairingFeedback({ success: false, message: `Fehler: ${msg || 'Ungültiger Code oder abgelaufen'}` });
      }
    } finally {
      setPairingSubmitting(false);
    }
  };

  const handleStartScan = async () => {
    setScanError(null);
    try {
      await triggerScanMutation.mutateAsync();
      await refetchScanStatus();
    } catch (err) {
      debugError('Failed to start scan', formatError(err));
      setScanError(err instanceof Error ? err.message : t('settings.streaming.scan.errors.start'));
    }
  };

  const handleOpenSettingsSection = async (
    nextSection: SettingsSection,
    nextTool?: SettingsTool,
  ) => {
    const normalizedTool = nextSection === 'advanced' ? nextTool : undefined;
    const activeToolValue = activeTool ?? undefined;

    if (activeSection === nextSection && activeToolValue === normalizedTool) {
      return;
    }

    const ok = await confirmPendingChanges();
    if (!ok) {
      return;
    }

    navigate(buildSettingsRoute({
      section: nextSection,
      tool: normalizedTool,
    }));
  };

  // ADR-00X: Profile persistence removed (universal policy only)

  return (
    <div className={`${styles.page} animate-enter`.trim()}>
      <div className={styles.header}>
        <div>
          <p className={styles.kicker}>{t('settings.kicker')}</p>
          <h1>{headerTitle}</h1>
          <p className={styles.subtitle}>
            {headerSubtitle}
          </p>
        </div>
      </div>

      {showContextBar ? (
        <SectionContextBar
          segments={[
            {
              label: t('settings.title'),
              onClick: () => { void handleOpenSettingsSection('setup'); },
            },
            {
              label: sectionLabelMap[activeSection],
              onClick: activeTool
                ? () => { void handleOpenSettingsSection(activeSection); }
                : undefined,
            },
            ...(activeTool ? [{ label: toolLabelMap[activeTool] }] : []),
          ]}
          actionLabel={activeTool
            ? t('settings.backToSection', {
              defaultValue: 'Back to {{section}}',
              section: sectionLabelMap[activeSection],
            })
            : t('settings.backToOverview', { defaultValue: 'Back to overview' })}
          onAction={activeTool
            ? () => { void handleOpenSettingsSection(activeSection); }
            : () => { void handleOpenSettingsSection('setup'); }}
        />
      ) : null}

      {configured ? (
        <>
          <div className={styles.sectionTabsShell}>
            <div className={styles.sectionTabs} role="tablist" aria-label={t('settings.sectionNavLabel', { defaultValue: 'Settings sections' })}>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'setup'}
                onClick={() => { void handleOpenSettingsSection('setup'); }}
                role="tab"
                aria-selected={activeSection === 'setup'}
              >
                {t('setup.title')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'security'}
                onClick={() => { void handleOpenSettingsSection('security'); }}
                role="tab"
                aria-selected={activeSection === 'security'}
              >
                Sicherheit & Passkeys
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'household'}
                onClick={() => { void handleOpenSettingsSection('household'); }}
                role="tab"
                aria-selected={activeSection === 'household'}
              >
                {t('settings.household.title', { defaultValue: 'Household profiles' })}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'android-tv'}
                onClick={() => { void handleOpenSettingsSection('android-tv'); }}
                role="tab"
                aria-selected={activeSection === 'android-tv'}
              >
                📱 Geräte & Apps koppeln
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'scan'}
                onClick={() => { void handleOpenSettingsSection('scan'); }}
                role="tab"
                aria-selected={activeSection === 'scan'}
              >
                {t('settings.streaming.scan.title')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'streaming'}
                onClick={() => { void handleOpenSettingsSection('streaming'); }}
                role="tab"
                aria-selected={activeSection === 'streaming'}
              >
                {t('settings.streaming.title')}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'advanced'}
                onClick={() => { void handleOpenSettingsSection('advanced'); }}
                role="tab"
                aria-selected={activeSection === 'advanced'}
              >
                {t('settings.advanced.title', { defaultValue: 'Advanced tools' })}
              </Button>
            </div>
          </div>
        </>
      ) : null}

      {showSection('setup') ? (
        <div className={styles.setup}>
        {!configured ? (
          <Config onUpdate={() => { void refetchConfig(); }} />
        ) : (
          <div className={styles.section}>
            <div className={styles.accordionHeader}>
              <h2>{t('setup.title')}</h2>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setShowSetup(v => !v)}
                data-testid="config-rerun-setup"
                aria-expanded={showSetup}
                aria-controls="settings-setup-details"
              >
                {showSetup ? t('common.hideDetails') : t('setup.actions.rerunSetup') || 'Re-run Setup'}
              </Button>
            </div>
            {showSetup && (
              <div id="settings-setup-details" className="animate-enter">
                <Config onUpdate={() => { void refetchConfig(); }} showTitle={false} compact />
              </div>
            )}
          </div>
        )}
        </div>
      ) : null}

      {showSection('security') ? (
        <div className={styles.section}>
          <SecuritySettingsSection />
        </div>
      ) : null}

      {showSection('household') ? (
        <div className={styles.section}>
          <AdminLayout initialSection="family" />
        </div>
      ) : null}

      {showSection('android-tv') ? (
        <div className={styles.section}>
        <h2>{t('settings.androidTv.title')}</h2>
        <p className={styles.subtitle}>{t('settings.androidTv.subtitle')}</p>

        <div className={styles.onboardingCard}>
          <div className={styles.onboardingHero}>
            <div className={styles.onboardingIntro}>
              <p className={styles.onboardingEyebrow}>UNIVERSAL ZERO-TRUST ENROLLMENT</p>
              <h3 className={styles.onboardingTitle}>📱 iPhone, Apple TV & Smart-TV mit PIN koppeln</h3>
              <p className={styles.onboardingCopy}>
                Starte die xg2g-App auf deinem iPhone, iPad, Apple TV oder Android TV und gib den auf dem Bildschirm angezeigten Code hier ein.
              </p>
            </div>
          </div>

          <div className={styles.onboardingMeta}>
            <div className={styles.group}>
              <label>{t('settings.androidTv.currentServer')}</label>
              <code className={`${styles.launchValue} tabular`.trim()}>{androidTvBaseUrlDisplay}</code>
              <span className={styles.hint}>{t('settings.androidTv.currentServerHint')}</span>
            </div>

            <div className={`${styles.group} ${styles.pairingGroup}`}>
              <label className={styles.pairingLabel} htmlFor="android-tv-pairing-code">
                🔑 Geräteschlüssel freigeben (Device Authorization)
              </label>
              <p className={`${styles.hint} ${styles.pairingHint}`}>
                Gib den 8-stelligen Code ein, der in deiner xg2g App angezeigt wird:
              </p>
              <div className={styles.pairingControls}>
                <input
                  id="android-tv-pairing-code"
                  className={styles.pairingInput}
                  type="text"
                  placeholder="z.B. MCDA-R2GC oder MCDAR2GC"
                  value={pairingCodeDraft}
                  autoCapitalize="characters"
                  autoCorrect="off"
                  spellCheck="false"
                  onChange={(e) => { setPairingCodeDraft(e.target.value.toUpperCase()); setPairingFeedback(null); }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      void handleApprovePairing();
                    }
                  }}
                />
                <Button
                  id="pairing-approve-submit"
                  onClick={() => { void handleApprovePairing(); }}
                  disabled={pairingSubmitting || !pairingCodeDraft.trim()}
                  className={styles.onboardingButton}
                >
                  {pairingSubmitting ? 'Autorisiere…' : 'Gerät Jetzt Koppeln'}
                </Button>
              </div>
              {pairingFeedback ? (
                <p className={`${styles.pairingFeedback} ${pairingFeedback.success ? styles.pairingFeedbackSuccess : styles.pairingFeedbackError}`}>
                  {pairingFeedback.message}
                </p>
              ) : null}
            </div>

            <div className={styles.onboardingActions}>
              {androidTvLaunchDisabled ? (
                <Button
                  className={styles.onboardingButton}
                  disabled
                >
                  {t('settings.androidTv.openApp')}
                </Button>
              ) : (
                <Button
                  href={androidTvLaunchUrl}
                  className={styles.onboardingButton}
                  rel="noopener noreferrer"
                >
                  {t('settings.androidTv.openApp')}
                </Button>
              )}
              <p className={androidTvLaunchDisabled ? styles.errorInline : styles.hint}>{androidTvLaunchHint}</p>
            </div>
          </div>
        </div>
        </div>
      ) : null}

      {showSection('scan') ? (
        <div className={styles.section}>
        <h2>{t('settings.streaming.scan.title')}</h2>
        <p className={styles.subtitle}>{t('settings.streaming.scan.description')}</p>

        <div className={styles.group}>
          <div className={styles.scanControls}>
            <Button
              onClick={handleStartScan}
              disabled={scanStatus?.state === 'running' || triggerScanMutation.isPending}
            >
              {scanStatus?.state === 'running' || triggerScanMutation.isPending
                ? t('settings.streaming.scan.status.running')
                : t('settings.streaming.scan.start')}
            </Button>
            {scanStatusErrorMessage && <span className={styles.errorInline}>{scanStatusErrorMessage}</span>}
          </div>

          {scanStatus && (
            <div className={styles.scanCard} data-state={scanStatus.state || undefined}>
              <div className={styles.scanHeader}>
                <div className={styles.scanBadge}>
                  <span className={styles.statusDot} data-state={scanStatus.state || undefined}></span>
                  <span className={styles.statusText}>{t(`settings.streaming.scan.status.${scanStatus.state || 'idle'}`)}</span>
                </div>
                {scanStatus.startedAt && scanStatus.startedAt > 0 && (
                  <div className={styles.scanTime}>
                    {new Date(scanStatus.startedAt * 1000).toLocaleTimeString()}
                  </div>
                )}
              </div>

              <div className={styles.progressContainer}>
                <svg
                  width="100%"
                  height="100%"
                  viewBox="0 0 100 6"
                  preserveAspectRatio="none"
                  role="img"
                  aria-label={t('settings.streaming.scan.stats.scanned')}
                >
                  <rect
                    x="0"
                    y="0"
                    width={Math.min(100, Math.max(0, ((scanStatus.scannedChannels || 0) / (scanStatus.totalChannels || 1)) * 100))}
                    height="6"
                    rx="3"
                    ry="3"
                    fill="var(--accent-action)"
                  />
                </svg>
              </div>

              <div className={styles.statsRow}>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.scannedChannels} / {scanStatus.totalChannels}</span>
                  <span className={styles.statLabel}>{t('settings.streaming.scan.stats.scanned')}</span>
                </div>
                <div className={styles.statItem}>
                  <span className={`${styles.statValue} tabular`.trim()}>{scanStatus.updatedCount}</span>
                  <span className={styles.statLabel}>{t('settings.streaming.scan.stats.updated')}</span>
                </div>
                {scanStatus.finishedAt && scanStatus.finishedAt > 0 && (
                  <div className={styles.statItem}>
                    <span className={styles.statValue}>{new Date(scanStatus.finishedAt * 1000).toLocaleTimeString()}</span>
                    <span className={styles.statLabel}>{t('settings.streaming.scan.timestamps.finished')}</span>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
        </div>
      ) : null}

      {showSection('streaming') ? (
        <div className={styles.section}>
        <h2>{t('settings.streaming.title')}</h2>
        <p className={styles.subtitle}>
          {t('settings.streaming.engineSubtitle', {
            defaultValue: 'xg2g regelt Video-Bitrate, Auflösung und Codecs vollautomatisch und adaptiv anhand deiner aktuellen Verbindung. Es ist kein starres manuelles Profil mehr nötig.'
          })}
        </p>

        <div className={styles.capabilityGrid}>
          <div className={styles.capabilityCard}>
            <div className={styles.capabilityCardHeader}>
              <span className={[styles.capabilityBadge, styles.capabilityBadgeInfo].join(' ')}>
                ADAPTIVE ENGINE
              </span>
              <strong className={styles.capabilityCardTitle}>CPU & GPU Transcoding</strong>
            </div>
            <p className={styles.capabilityCardCopy}>
              {t('settings.streaming.cardEngineText', {
                defaultValue: 'Erkennt automatisch Hardware-Beschleunigung (GPU: VAAPI, NVENC, QSV, VideoToolbox) auf dem Server und greift bei Bedarf nahtlos auf optimiertes CPU-Encoding zurück. Passt Bitrate und Auflösung dynamisch an (strikte 720p-Untergrenze bis hin zu nativem 1080p. Hinweis: 4K/UHD ist aktuell vorübergehend pausiert – wir arbeiten bereits daran!).'
              })}
            </p>
          </div>

          <div className={styles.capabilityCard}>
            <div className={styles.capabilityCardHeader}>
              <span className={[styles.capabilityBadge, styles.capabilityBadgeSuccess].join(' ')}>
                CODECS
              </span>
              <strong className={styles.capabilityCardTitle}>AV1 · HEVC · H.264 · MPEG-2</strong>
            </div>
            <p className={styles.capabilityCardCopy}>
              {t('settings.streaming.cardCodecsText', {
                defaultValue: 'Dynamische Aushandlung der besten Codecs: AV1 & HEVC (H.265) für maximale Qualität bei kleinstem Datenvolumen, H.264 für universelle Kompatibilität und MPEG-2 für verlustfreies Direct Play im LAN.'
              })}
            </p>
          </div>

          <div className={styles.capabilityCard}>
            <div className={styles.capabilityCardHeader}>
              <span className={[styles.capabilityBadge, styles.capabilityBadgeInfo].join(' ')}>
                CONTAINER
              </span>
              <strong className={styles.capabilityCardTitle}>fMP4 · CMAF · MPEG-TS</strong>
            </div>
            <p className={styles.capabilityCardCopy}>
              {t('settings.streaming.cardContainersText', {
                defaultValue: 'Modernes Low-Latency HLS (fMP4 / CMAF) für blitzschnelles Umschalten bei HD-Sendern. Schutz bei 4K-Sendern: Da 4K/UHD-Streams in fMP4 im Browser häufig zu Pufferstaus/Rucklern führen, sind 4K/UHD-Sender momentan vorübergehend pausiert, bis die Pipeline dafür vollständig optimiert ist (wir arbeiten daran!).'
              })}
            </p>
          </div>

          <div className={styles.capabilityCard}>
            <div className={styles.capabilityCardHeader}>
              <span className={[styles.capabilityBadge, styles.capabilityBadgeWarning].join(' ')}>
                DEINTERLACING
              </span>
              <strong className={styles.capabilityCardTitle}>Interlaced vs. Progressive</strong>
            </div>
            <p className={styles.capabilityCardCopy}>
              {t('settings.streaming.cardInterlacedText', {
                defaultValue: 'Progressive Sender (720p, 1080p) können 1:1 durchgereicht werden (4K/UHD in Vorbereitung – wir arbeiten daran!). Interlaced Sender (z. B. 1080i / 576i) werden von Browsern/Smartphones nicht nativ unterstützt (Zeilenflimmern) und daher von xg2g immer automatisch in flüssiges 50-fps-Progressive-Video konvertiert.'
              })}
            </p>
          </div>
        </div>

        <div className={[styles.group, styles.optionGroup].join(' ')}>
          <label className={styles.optionGroupTitle}>{t('settings.streaming.audioMode.title')}</label>
          <div className={styles.optionList}>
            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="audioMode"
                value="stereo"
                checked={audioMode === 'stereo'}
                onChange={() => {
                  setAudioMode('stereo');
                  try { localStorage.setItem('xg2g.settings.audioMode', 'stereo'); } catch { /* localStorage may throw in private browsing */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.audioMode.stereo.label')}</div>
                <div className={styles.hint}>
                  {t('settings.streaming.audioMode.stereo.hint')}
                </div>
              </div>
            </label>

            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="audioMode"
                value="surround"
                checked={audioMode === 'surround'}
                onChange={() => {
                  setAudioMode('surround');
                  try { localStorage.setItem('xg2g.settings.audioMode', 'surround'); } catch { /* localStorage may throw in private browsing */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.audioMode.surround.label')}</div>
                <div className={styles.hint}>
                  {t('settings.streaming.audioMode.surround.hint')}
                  <div className={styles.warningHint}>
                    ⚠️ <strong>{t('settings.streaming.audioMode.surround.warningTitle')}</strong> {t('settings.streaming.audioMode.surround.warningText')}
                  </div>
                </div>
              </div>
            </label>
          </div>
        </div>

        <div className={[styles.group, styles.optionGroup].join(' ')}>
          <label className={styles.optionGroupTitle}>{t('settings.streaming.dvrMode.title')}</label>
          <div className={styles.optionList}>
            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="dvrMode"
                value="live_only"
                checked={dvrMode === 'live_only'}
                onChange={() => {
                  setDvrMode('live_only');
                  try { localStorage.setItem('xg2g.settings.dvrMode', 'live_only'); } catch { /* localStorage unavailable */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.liveOnly.label')}</div>
                <div className={styles.hint}>{t('settings.streaming.dvrMode.liveOnly.hint')}</div>
              </div>
            </label>

            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="dvrMode"
                value="1h"
                checked={dvrMode === '1h'}
                onChange={() => {
                  setDvrMode('1h');
                  try { localStorage.setItem('xg2g.settings.dvrMode', '1h'); } catch { /* localStorage unavailable */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr1h.label')}</div>
                <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr1h.hint')}</div>
              </div>
            </label>

            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="dvrMode"
                value="2h"
                checked={dvrMode === '2h'}
                onChange={() => {
                  setDvrMode('2h');
                  try { localStorage.setItem('xg2g.settings.dvrMode', '2h'); } catch { /* localStorage unavailable */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr2h.label')}</div>
                <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr2h.hint')}</div>
              </div>
            </label>

            <label className={styles.optionChoice}>
              <input
                type="radio"
                name="dvrMode"
                value="4h"
                checked={dvrMode === '4h'}
                onChange={() => {
                  setDvrMode('4h');
                  try { localStorage.setItem('xg2g.settings.dvrMode', '4h'); } catch { /* localStorage unavailable */ }
                }}
                className={styles.optionInput}
              />
              <div>
                <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr4h.label')}</div>
                <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr4h.hint')}</div>
              </div>
            </label>
          </div>
        </div>
        </div>
      ) : null}

      {showSection('advanced') ? (
        <div className={styles.section}>
        <h2>{t('settings.advanced.title', { defaultValue: 'Advanced tools' })}</h2>
        <p className={styles.subtitle}>
          {t('settings.advanced.subtitle', {
            defaultValue: 'File browser and diagnostic logs stay available here as expert tools without adding more main navigation.',
          })}
        </p>
        <div className={styles.advancedActions}>
          <Button
            variant="secondary"
            active={activeTool === 'files'}
            onClick={() => { void handleOpenSettingsSection('advanced', 'files'); }}
          >
            {t('nav.files')}
          </Button>
          <Button
            variant="secondary"
            active={activeTool === 'logs'}
            onClick={() => { void handleOpenSettingsSection('advanced', 'logs'); }}
          >
            {t('nav.logs')}
          </Button>
        </div>
        {activeTool === 'files' ? (
          <div className={styles.embeddedTool}>
            <Files showLegacyNotice={false} />
          </div>
        ) : null}
        {activeTool === 'logs' ? (
          <div className={styles.embeddedTool}>
            <Logs showLegacyNotice={false} />
          </div>
        ) : null}
        </div>
      ) : null}

      {/* Adaptive Bitrate removed as per 2026 Design Contract (Trust Hardening) */}

      {/* ADR-00X: Saved message removed (was for profile save feedback) */}



    </div>
  );
}

export default Settings;
