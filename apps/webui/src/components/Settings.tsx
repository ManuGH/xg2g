// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useMemo, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { isConfigured } from './Config';
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
import PlaybackSettingsSection from './settings/PlaybackSettingsSection';
import DevicePairingSection from './settings/DevicePairingSection';
import ChannelScanSection from './settings/ChannelScanSection';
import SetupSection from './settings/SetupSection';
import AboutSection from './settings/AboutSection';
import AdvancedToolsSection from './settings/AdvancedToolsSection';

const SETTINGS_SECTIONS: SettingsSection[] = [
  'setup',
  'security',
  'household',
  'android-tv',
  'scan',
  'streaming',
  'advanced',
  'about',
];

const SETTINGS_TOOLS: SettingsTool[] = ['files', 'logs'];

function isSettingsSection(value: string | null): value is SettingsSection {
  return value !== null && SETTINGS_SECTIONS.includes(value as SettingsSection);
}

function isSettingsTool(value: string | null): value is SettingsTool {
  return value !== null && SETTINGS_TOOLS.includes(value as SettingsTool);
}

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

  const showSection = (section: SettingsSection) => activeSection === section;

  const sectionLabelMap: Record<SettingsSection, string> = {
    setup: getSettingsSectionLabel('setup', t),
    household: getSettingsSectionLabel('household', t),
    'android-tv': getSettingsSectionLabel('android-tv', t),
    scan: getSettingsSectionLabel('scan', t),
    streaming: getSettingsSectionLabel('streaming', t),
    security: getSettingsSectionLabel('security', t),
    advanced: getSettingsSectionLabel('advanced', t),
    about: getSettingsSectionLabel('about', t),
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
        setPairingFeedback({ success: true, message: `✅ ${t('settings.devices.successFeedback')}` });
        setPairingCodeDraft('');
      } else {
        setPairingFeedback({ success: false, message: t('settings.devices.errorDefault') });
      }
    } catch (e: any) {
      const msg = e?.message || '';
      if (msg.includes('Authentication required') || e?.status === 401) {
        setPairingFeedback({
          success: false,
          message: `🔐 ${t('settings.devices.errorAuthRequired')}`
        });
      } else {
        setPairingFeedback({
          success: false,
          message: t('settings.devices.errorPrefix', { message: msg || t('settings.devices.errorDefault') })
        });
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

      {configured ? (
        <div className={styles.sectionTabsShell}>
          <div className={styles.navGroup}>
            <span className={styles.navGroupLabel}>Wiedergabe & Geräte</span>
            <div className={styles.sectionTabs} role="tablist" aria-label="Wiedergabe und Geräte">
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
                active={activeSection === 'android-tv'}
                onClick={() => { void handleOpenSettingsSection('android-tv'); }}
                role="tab"
                aria-selected={activeSection === 'android-tv'}
              >
                {t('settings.devices.tabTitle', { defaultValue: 'Geräte & Apps koppeln' })}
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
            </div>
          </div>

          <div className={styles.navGroup}>
            <span className={styles.navGroupLabel}>Server & Verwaltung</span>
            <div className={styles.sectionTabs} role="tablist" aria-label="Server und Verwaltung">
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
                active={activeSection === 'security'}
                onClick={() => { void handleOpenSettingsSection('security'); }}
                role="tab"
                aria-selected={activeSection === 'security'}
              >
                {t('settings.security.tabTitle', { defaultValue: 'Sicherheit & Passkeys' })}
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
              <Button
                variant="secondary"
                size="sm"
                active={activeSection === 'about'}
                onClick={() => { void handleOpenSettingsSection('about'); }}
                role="tab"
                aria-selected={activeSection === 'about'}
              >
                {t('settings.about.tabTitle', { defaultValue: 'Über & Speicher' })}
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      {showSection('setup') ? (
        <SetupSection
          configured={configured}
          showSetup={showSetup}
          setShowSetup={setShowSetup}
          refetchConfig={refetchConfig}
        />
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
        <DevicePairingSection
          pairingCodeDraft={pairingCodeDraft}
          setPairingCodeDraft={setPairingCodeDraft}
          pairingSubmitting={pairingSubmitting}
          pairingFeedback={pairingFeedback}
          setPairingFeedback={setPairingFeedback}
          handleApprovePairing={handleApprovePairing}
          androidTvBaseUrlDisplay={androidTvBaseUrlDisplay}
          androidTvLaunchUrl={androidTvLaunchUrl}
          androidTvLaunchDisabled={androidTvLaunchDisabled}
          androidTvLaunchHint={androidTvLaunchHint}
        />
      ) : null}

      {showSection('scan') ? (
        <ChannelScanSection
          scanStatus={scanStatus}
          scanStatusErrorMessage={scanStatusErrorMessage}
          triggerScanPending={triggerScanMutation.isPending}
          handleStartScan={handleStartScan}
        />
      ) : null}

      {showSection('streaming') ? (
        <PlaybackSettingsSection
          audioMode={audioMode}
          setAudioMode={setAudioMode}
          dvrMode={dvrMode}
          setDvrMode={setDvrMode}
        />
      ) : null}

      {showSection('advanced') ? (
        <AdvancedToolsSection
          activeTool={activeTool}
          onSelectTool={(tool) => { void handleOpenSettingsSection('advanced', tool); }}
        />
      ) : null}

      {showSection('about') ? (
        <AboutSection
          serverConnected={Boolean(connectivity?.status === 'ok' || config)}
          onResetPreferences={() => {
            setAudioMode('stereo');
            setDvrMode('2h');
          }}
        />
      ) : null}
    </div>
  );
}

export default Settings;
