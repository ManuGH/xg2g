// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import Config, { isConfigured } from './Config';
import { putSystemConfig, type ConfigUpdate } from '../client-ts';
import {
  useSystemConfig,
  useSystemScanStatus,
  useTriggerSystemScanMutation,
} from '../hooks/useServerQueries';
import { useAppContext } from '../context/AppContext';
import { useHouseholdProfiles } from '../context/HouseholdProfilesContext';
import { usePendingChanges } from '../context/PendingChangesContext';
import { useUiOverlay } from '../context/UiOverlayContext';
import {
  createHouseholdProfile,
  normalizeHouseholdProfile,
  type HouseholdProfile,
} from '../features/household/model';
import { unwrapClientResultOrThrow } from '../services/clientWrapper';
import { debugError, formatError } from '../utils/logging';
import { Button } from './ui';
import styles from './Settings.module.css';

function Settings() {
  const { t } = useTranslation();
  const { channels, loadChannels } = useAppContext();
  const { confirm, toast } = useUiOverlay();
  const { confirmPendingChanges, setPendingChangesGuard } = usePendingChanges();
  const {
    profiles,
    selectedProfile,
    saveProfile,
    deleteProfile,
    selectProfile,
  } = useHouseholdProfiles();
  // ADR-00X: Profile selection removed (universal policy only)

  // ADR-00X: Unused savedMessage state removed (was for profile save feedback)
  const [scanError, setScanError] = useState<string | null>(null);
  const [showSetup, setShowSetup] = useState<boolean>(false);
  const [editingProfileId, setEditingProfileId] = useState<string>(() => selectedProfile.id);
  const [profileDraft, setProfileDraft] = useState<HouseholdProfile>(() => selectedProfile);
  const [channelQuery, setChannelQuery] = useState<string>('');
  const [pinDraft, setPinDraft] = useState<string>('');
  const [pinConfirmDraft, setPinConfirmDraft] = useState<string>('');
  const [pinSaving, setPinSaving] = useState<boolean>(false);
  const {
    data: config = null,
    refetch: refetchConfig,
  } = useSystemConfig();
  const {
    data: scanStatus = null,
    error: scanStatusError,
    refetch: refetchScanStatus,
  } = useSystemScanStatus();
  const triggerScanMutation = useTriggerSystemScanMutation();
  const androidTvBaseUrl = useMemo(() => {
    if (typeof window === 'undefined') {
      return '';
    }
    return new URL('/ui/', window.location.origin).toString();
  }, []);
  const androidTvLaunchUrl = useMemo(() => {
    if (!androidTvBaseUrl) {
      return '';
    }
    const params = new URLSearchParams({ base_url: androidTvBaseUrl });
    return `xg2g://connect?${params.toString()}`;
  }, [androidTvBaseUrl]);

  const configured = isConfigured(config);
  const householdPinConfigured = Boolean(config?.household?.pinConfigured);
  const persistedEditingProfile = profiles.find((profile) => profile.id === editingProfileId) ?? null;
  const editingProfile = persistedEditingProfile ?? selectedProfile;
  const editingProfilePersisted = profiles.some((profile) => profile.id === editingProfile.id);
  const normalizedDraft = normalizeHouseholdProfile(profileDraft);
  const isProfileDirty = editingProfilePersisted
    ? JSON.stringify(normalizedDraft) !== JSON.stringify(persistedEditingProfile)
    : true;
  const scanStatusErrorMessage = !scanStatus
    ? scanError ?? (
      scanStatusError instanceof Error
        ? scanStatusError.message
        : scanStatusError
          ? t('settings.streaming.scan.errors.loadStatus')
          : null
    )
    : scanError;
  const pinDraftValid = /^\d{4,12}$/.test(pinDraft);
  const pinDraftsMatch = pinDraft === pinConfirmDraft;

  useEffect(() => {
    const persistedProfile = profiles.find((profile) => profile.id === editingProfileId);
    if (persistedProfile) {
      setProfileDraft(persistedProfile);
      return;
    }

    if (profileDraft.id === editingProfileId) {
      return;
    }

    setEditingProfileId(selectedProfile.id);
    setProfileDraft(selectedProfile);
  }, [editingProfileId, profileDraft.id, profiles, selectedProfile]);

  useEffect(() => {
    if (!isProfileDirty) {
      setPendingChangesGuard(null);
      return;
    }

    setPendingChangesGuard({
      isDirty: true,
      confirmDiscard: async () => {
        const ok = await confirm({
          title: t('settings.household.unsavedTitle', { defaultValue: 'Ungespeicherte Aenderungen verwerfen?' }),
          message: t('settings.household.unsavedMessage', {
            defaultValue: 'Dieses Profil hat ungespeicherte Aenderungen. Willst du sie wirklich verwerfen?',
          }),
          confirmLabel: t('settings.household.unsavedConfirm', { defaultValue: 'Verwerfen' }),
          cancelLabel: t('common.cancel', { defaultValue: 'Abbrechen' }),
          tone: 'danger',
        });
        if (!ok) {
          return false;
        }

        if (persistedEditingProfile) {
          setProfileDraft(persistedEditingProfile);
        } else {
          setEditingProfileId(selectedProfile.id);
          setProfileDraft(selectedProfile);
        }
        setChannelQuery('');
        return true;
      },
    });
  }, [
    confirm,
    isProfileDirty,
    persistedEditingProfile,
    selectedProfile,
    setPendingChangesGuard,
    t,
  ]);

  useEffect(() => {
    return () => {
      setPendingChangesGuard(null);
    };
  }, [setPendingChangesGuard]);

  const visibleChannels = useMemo(() => {
    const query = channelQuery.trim().toLowerCase();
    if (!query) {
      return channels.channels;
    }

    return channels.channels.filter((channel) => {
      const haystack = [
        channel.name,
        channel.group,
        channel.number,
      ].join(' ').toLowerCase();
      return haystack.includes(query);
    });
  }, [channelQuery, channels.channels]);

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

  const updateDraft = (updates: Partial<HouseholdProfile>) => {
    setProfileDraft((current) => normalizeHouseholdProfile({
      ...current,
      ...updates,
    }));
  };

  const toggleDraftListValue = (field: 'allowedBouquets' | 'allowedServiceRefs' | 'favoriteServiceRefs', value: string) => {
    const normalizedValue = value.trim().toLowerCase();
    if (!normalizedValue) {
      return;
    }

    setProfileDraft((current) => {
      const currentList = current[field];
      const nextList = currentList.includes(normalizedValue)
        ? currentList.filter((entry) => entry !== normalizedValue)
        : [...currentList, normalizedValue];

      return normalizeHouseholdProfile({
        ...current,
        [field]: nextList,
      });
    });
  };

  const handleCreateProfile = (kind: 'adult' | 'child') => {
    const nextProfile = createHouseholdProfile(kind);
    setEditingProfileId(nextProfile.id);
    setProfileDraft(nextProfile);
    setChannelQuery('');
  };

  const handleOpenProfile = async (profile: HouseholdProfile) => {
    if (profile.id === editingProfileId) {
      return;
    }

    const ok = await confirmPendingChanges();
    if (!ok) {
      return;
    }

    setEditingProfileId(profile.id);
    setProfileDraft(profile);
    setChannelQuery('');
  };

  const handleCreateProfileWithGuard = async (kind: 'adult' | 'child') => {
    const ok = await confirmPendingChanges();
    if (!ok) {
      return;
    }

    handleCreateProfile(kind);
  };

  const handleUseProfileNow = async () => {
    if (selectedProfile.id === editingProfile.id) {
      return;
    }

    const ok = await confirmPendingChanges();
    if (!ok) {
      return;
    }

    await selectProfile(editingProfile.id);
  };

  const handleSaveProfile = async () => {
    try {
      await saveProfile(normalizedDraft);
      toast({
        kind: 'success',
        message: t('settings.household.saveSuccess', { defaultValue: 'Profil gespeichert' }),
      });
    } catch (err) {
      debugError('Failed to save household profile', formatError(err));
      toast({
        kind: 'error',
        message: t('settings.household.saveError', { defaultValue: 'Profil konnte nicht gespeichert werden' }),
      });
    }
  };

  const handleDeleteProfile = async () => {
    if (profiles.length <= 1) {
      return;
    }

    const ok = await confirm({
      title: t('settings.household.deleteTitle', { defaultValue: 'Profil löschen' }),
      message: t('settings.household.deleteMessage', {
        defaultValue: `Soll "${editingProfile.name}" wirklich entfernt werden?`,
      }),
      confirmLabel: t('settings.household.deleteConfirm', { defaultValue: 'Löschen' }),
      cancelLabel: t('common.cancel', { defaultValue: 'Abbrechen' }),
      tone: 'danger',
    });
    if (!ok) {
      return;
    }

    try {
      await deleteProfile(editingProfile.id);
      toast({
        kind: 'info',
        message: t('settings.household.deleteSuccess', { defaultValue: 'Profil entfernt' }),
      });
    } catch (err) {
      debugError('Failed to delete household profile', formatError(err));
      toast({
        kind: 'error',
        message: t('settings.household.deleteError', { defaultValue: 'Profil konnte nicht entfernt werden' }),
      });
    }
  };

  const handleSaveHouseholdPin = async () => {
    if (!pinDraftValid) {
      toast({
        kind: 'warning',
        message: t('settings.household.pin.invalid', { defaultValue: 'Der Haushalt-PIN muss aus 4 bis 12 Ziffern bestehen.' }),
      });
      return;
    }
    if (!pinDraftsMatch) {
      toast({
        kind: 'warning',
        message: t('settings.household.pin.mismatch', { defaultValue: 'PIN und PIN-Bestaetigung stimmen nicht ueberein.' }),
      });
      return;
    }

    setPinSaving(true);
    try {
      const payload: ConfigUpdate = {
        household: {
          pin: pinDraft,
        },
      };
      const result = await putSystemConfig({ body: payload });
      const data = unwrapClientResultOrThrow<{ restartRequired?: boolean }>(result, {
        source: 'Settings.handleSaveHouseholdPin',
      });
      await refetchConfig();
      setPinDraft('');
      setPinConfirmDraft('');
      toast({
        kind: 'success',
        message: data.restartRequired
          ? t('settings.household.pin.savedRestart', { defaultValue: 'Haushalt-PIN gespeichert. Ein Neustart wurde angefordert.' })
          : t('settings.household.pin.saved', { defaultValue: 'Haushalt-PIN gespeichert.' }),
      });
    } catch (err) {
      debugError('Failed to save household pin', formatError(err));
      toast({
        kind: 'error',
        message: t('settings.household.pin.saveError', { defaultValue: 'Haushalt-PIN konnte nicht gespeichert werden.' }),
      });
    } finally {
      setPinSaving(false);
    }
  };

  const handleClearHouseholdPin = async () => {
    const ok = await confirm({
      title: t('settings.household.pin.clearTitle', { defaultValue: 'Haushalt-PIN entfernen?' }),
      message: t('settings.household.pin.clearMessage', {
        defaultValue: 'Danach sind Erwachsenenprofile und Household-Settings nicht mehr per PIN geschützt.',
      }),
      confirmLabel: t('settings.household.pin.clearConfirm', { defaultValue: 'PIN entfernen' }),
      cancelLabel: t('common.cancel', { defaultValue: 'Abbrechen' }),
      tone: 'danger',
    });
    if (!ok) {
      return;
    }

    setPinSaving(true);
    try {
      const payload: ConfigUpdate = {
        household: {
          pin: '',
        },
      };
      const result = await putSystemConfig({ body: payload });
      unwrapClientResultOrThrow<{ restartRequired?: boolean }>(result, {
        source: 'Settings.handleClearHouseholdPin',
      });
      await refetchConfig();
      setPinDraft('');
      setPinConfirmDraft('');
      toast({
        kind: 'info',
        message: t('settings.household.pin.cleared', { defaultValue: 'Haushalt-PIN entfernt.' }),
      });
    } catch (err) {
      debugError('Failed to clear household pin', formatError(err));
      toast({
        kind: 'error',
        message: t('settings.household.pin.clearError', { defaultValue: 'Haushalt-PIN konnte nicht entfernt werden.' }),
      });
    } finally {
      setPinSaving(false);
    }
  };

  // ADR-00X: Profile persistence removed (universal policy only)

  return (
    <div className={`${styles.page} animate-enter`.trim()}>
      <div className={styles.header}>
        <div>
          <p className={styles.kicker}>{t('settings.kicker')}</p>
          <h2>{t('settings.title')}</h2>
          <p className={styles.subtitle}>
            {t('settings.subtitle')}
          </p>
        </div>
      </div>

      <div className={styles.setup}>
        {!configured ? (
          <Config onUpdate={() => { void refetchConfig(); }} />
        ) : (
          <div className={styles.section}>
            <div className={styles.accordionHeader}>
              <h3>{t('setup.title')}</h3>
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

      <div className={styles.section}>
        <h3>{t('settings.household.title', { defaultValue: 'Haushaltsprofile' })}</h3>
        <p className={styles.subtitle}>
          {t('settings.household.subtitle', {
            defaultValue: 'Lege getrennte Profile fuer Erwachsene und Kinder an, speichere Senderfavoriten und steuere pro Profil den Zugriff auf DVR und Einstellungen.',
          })}
        </p>

        <div className={styles.profileEditor}>
          <div className={styles.profileEditorHeader}>
            <div>
              <p className={styles.kicker}>{t('settings.household.pin.eyebrow', { defaultValue: 'PIN-Schutz' })}</p>
              <h4 className={styles.profileEditorTitle}>{t('settings.household.pin.title', { defaultValue: 'Erwachsenenprofile absichern' })}</h4>
            </div>
            <div className={styles.profileEditorActions}>
              <span className={styles.profileBadge}>
                {householdPinConfigured
                  ? t('settings.household.pin.configured', { defaultValue: 'PIN aktiv' })
                  : t('settings.household.pin.unconfigured', { defaultValue: 'Kein PIN' })}
              </span>
            </div>
          </div>

          <div className={styles.profileGrid}>
            <label className={styles.profileField}>
              <span>{t('settings.household.pin.label', { defaultValue: 'Neuer Haushalt-PIN' })}</span>
              <input
                className={styles.profileInput}
                type="password"
                inputMode="numeric"
                pattern="[0-9]*"
                autoComplete="new-password"
                value={pinDraft}
                onChange={(event) => setPinDraft(event.target.value)}
                placeholder={t('settings.household.pin.placeholder', { defaultValue: '4 bis 12 Ziffern' })}
              />
            </label>

            <label className={styles.profileField}>
              <span>{t('settings.household.pin.confirmLabel', { defaultValue: 'PIN bestätigen' })}</span>
              <input
                className={styles.profileInput}
                type="password"
                inputMode="numeric"
                pattern="[0-9]*"
                autoComplete="new-password"
                value={pinConfirmDraft}
                onChange={(event) => setPinConfirmDraft(event.target.value)}
                placeholder={t('settings.household.pin.placeholder', { defaultValue: '4 bis 12 Ziffern' })}
              />
            </label>
          </div>

          <div className={styles.profilePanel}>
            <span className={styles.hint}>
              {t('settings.household.pin.hint', {
                defaultValue: 'Mit gesetztem PIN brauchen Erwachsenenprofile, Household-Settings und Logout aus dem Kinderprofil die Freigabe. Die Freischaltung endet bei Logout, Browserende oder Ablauf der Server-Entsperrung. Ohne PIN bleibt Household reines Profil-Scoping.',
              })}
            </span>
            <div className={styles.profilePanelActions}>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => { void handleSaveHouseholdPin(); }}
                disabled={pinSaving || !pinDraftValid || !pinDraftsMatch}
              >
                {t('settings.household.pin.save', { defaultValue: 'PIN speichern' })}
              </Button>
              {householdPinConfigured ? (
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => { void handleClearHouseholdPin(); }}
                  disabled={pinSaving}
                >
                  {t('settings.household.pin.clear', { defaultValue: 'PIN entfernen' })}
                </Button>
              ) : null}
            </div>
          </div>
        </div>

        <div className={styles.profileCards}>
          {profiles.map((profile) => (
            <button
              key={profile.id}
              type="button"
              className={[
                styles.profileCard,
                editingProfileId === profile.id ? styles.profileCardActive : null,
              ].filter(Boolean).join(' ')}
              onClick={() => { void handleOpenProfile(profile); }}
            >
              <div className={styles.profileCardHeader}>
                <div>
                  <p className={styles.profileCardEyebrow}>
                    {profile.kind === 'child'
                      ? t('settings.household.kind.child', { defaultValue: 'Kinderprofil' })
                      : t('settings.household.kind.adult', { defaultValue: 'Erwachsenenprofil' })}
                  </p>
                  <strong className={styles.profileCardTitle}>{profile.name}</strong>
                </div>
                {selectedProfile.id === profile.id && (
                  <span className={styles.profileBadge}>
                    {t('settings.household.active', { defaultValue: 'Aktiv' })}
                  </span>
                )}
              </div>
              <p className={styles.profileCardMeta}>
                {profile.favoriteServiceRefs.length} {t('settings.household.favoritesShort', { defaultValue: 'Favoriten' })}
                {' · '}
                {profile.allowedServiceRefs.length > 0 || profile.allowedBouquets.length > 0
                  ? t('settings.household.restricted', { defaultValue: 'eingeschraenkt' })
                  : t('settings.household.unrestricted', { defaultValue: 'alle Sender' })}
              </p>
            </button>
          ))}
        </div>

        <div className={styles.profileToolbar}>
          <Button size="sm" onClick={() => { void handleCreateProfileWithGuard('adult'); }}>
            {t('settings.household.newAdult', { defaultValue: 'Erwachsenenprofil' })}
          </Button>
          <Button variant="secondary" size="sm" onClick={() => { void handleCreateProfileWithGuard('child'); }}>
            {t('settings.household.newChild', { defaultValue: 'Kinderprofil' })}
          </Button>
          {channels.selectedBouquet ? (
            <Button variant="ghost" size="sm" onClick={() => { void loadChannels(''); }}>
              {t('settings.household.loadAllChannels', { defaultValue: 'Alle Sender laden' })}
            </Button>
          ) : null}
        </div>

        <div className={styles.profileEditor}>
          <div className={styles.profileEditorHeader}>
            <div>
              <p className={styles.kicker}>{t('settings.household.editorEyebrow', { defaultValue: 'Profil bearbeiten' })}</p>
              <h4 className={styles.profileEditorTitle}>{editingProfile.name}</h4>
            </div>
            <div className={styles.profileEditorActions}>
              <Button
                variant={selectedProfile.id === editingProfile.id ? 'secondary' : 'ghost'}
                size="sm"
                onClick={() => { void handleUseProfileNow(); }}
                disabled={!editingProfilePersisted}
              >
                {selectedProfile.id === editingProfile.id
                  ? t('settings.household.active', { defaultValue: 'Aktiv' })
                  : t('settings.household.useNow', { defaultValue: 'Jetzt nutzen' })}
              </Button>
              <Button variant="secondary" size="sm" onClick={handleSaveProfile}>
                {t('common.save', { defaultValue: 'Speichern' })}
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => { void handleDeleteProfile(); }}
                disabled={profiles.length <= 1}
              >
                {t('common.delete', { defaultValue: 'Loeschen' })}
              </Button>
            </div>
          </div>

          <div className={styles.profileGrid}>
            <label className={styles.profileField}>
              <span>{t('settings.household.name', { defaultValue: 'Profilname' })}</span>
              <input
                className={styles.profileInput}
                type="text"
                value={normalizedDraft.name}
                onChange={(event) => updateDraft({ name: event.target.value })}
              />
            </label>

            <label className={styles.profileField}>
              <span>{t('settings.household.kind.label', { defaultValue: 'Typ' })}</span>
              <select
                className={styles.profileInput}
                value={normalizedDraft.kind}
                onChange={(event) => updateDraft({ kind: event.target.value === 'child' ? 'child' : 'adult' })}
              >
                <option value="adult">{t('settings.household.kind.adult', { defaultValue: 'Erwachsenenprofil' })}</option>
                <option value="child">{t('settings.household.kind.child', { defaultValue: 'Kinderprofil' })}</option>
              </select>
            </label>

            <label className={styles.profileField}>
              <span>{t('settings.household.maxFsk', { defaultValue: 'Maximale FSK' })}</span>
              <select
                className={styles.profileInput}
                value={normalizedDraft.maxFsk ?? -1}
                onChange={(event) => updateDraft({
                  maxFsk: Number.parseInt(event.target.value, 10) < 0 ? null : Number.parseInt(event.target.value, 10),
                })}
              >
                <option value={-1}>{t('settings.household.maxFskAny', { defaultValue: 'ohne Grenze' })}</option>
                <option value={0}>FSK 0</option>
                <option value={6}>FSK 6</option>
                <option value={12}>FSK 12</option>
                <option value={16}>FSK 16</option>
                <option value={18}>FSK 18</option>
              </select>
              <span className={styles.hint}>
                {t('settings.household.maxFskHint', {
                  defaultValue: 'Vorbereitet fuer spaetere EPG-Altersfreigaben. Aktuell hat die App noch keine verifizierten FSK-Daten im Feed.',
                })}
              </span>
            </label>
          </div>

          <div className={styles.permissionGrid}>
            <label className={styles.permissionToggle}>
              <input
                type="checkbox"
                checked={normalizedDraft.permissions.dvrPlayback}
                onChange={(event) => updateDraft({
                  permissions: {
                    ...normalizedDraft.permissions,
                    dvrPlayback: event.target.checked,
                  },
                })}
              />
              <span>{t('settings.household.permissions.dvrPlayback', { defaultValue: 'Aufnahmen ansehen' })}</span>
            </label>

            <label className={styles.permissionToggle}>
              <input
                type="checkbox"
                checked={normalizedDraft.permissions.dvrManage}
                onChange={(event) => updateDraft({
                  permissions: {
                    ...normalizedDraft.permissions,
                    dvrManage: event.target.checked,
                  },
                })}
              />
              <span>{t('settings.household.permissions.dvrManage', { defaultValue: 'DVR bedienen' })}</span>
            </label>

            <label className={styles.permissionToggle}>
              <input
                type="checkbox"
                checked={normalizedDraft.permissions.settings}
                onChange={(event) => updateDraft({
                  permissions: {
                    ...normalizedDraft.permissions,
                    settings: event.target.checked,
                  },
                })}
              />
              <span>{t('settings.household.permissions.settings', { defaultValue: 'System & Einstellungen' })}</span>
            </label>
          </div>

          <div className={styles.profilePanel}>
            <div className={styles.profilePanelHeader}>
              <div>
                <label>{t('settings.household.allowedBouquets', { defaultValue: 'Erlaubte Bouquets' })}</label>
                <span className={styles.hint}>
                  {t('settings.household.allowedBouquetsHint', { defaultValue: 'Leer bedeutet: keine Einschraenkung auf Bouquet-Ebene.' })}
                </span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => updateDraft({ allowedBouquets: [] })}
              >
                {t('settings.household.clearBouquets', { defaultValue: 'Alle erlauben' })}
              </Button>
            </div>

            <div className={styles.choiceGrid}>
              {channels.bouquets.map((bouquet) => {
                const bouquetName = String(bouquet.name || '').trim();
                if (!bouquetName) {
                  return null;
                }

                return (
                  <label key={bouquetName} className={styles.choiceChip}>
                    <input
                      type="checkbox"
                      checked={normalizedDraft.allowedBouquets.includes(bouquetName.toLowerCase())}
                      onChange={() => toggleDraftListValue('allowedBouquets', bouquetName)}
                    />
                    <span>{bouquetName}</span>
                  </label>
                );
              })}
            </div>
          </div>

          <div className={styles.profilePanel}>
            <div className={styles.profilePanelHeader}>
              <div>
                <label>{t('settings.household.channels', { defaultValue: 'Senderzugriff & Favoriten' })}</label>
                <span className={styles.hint}>
                  {t('settings.household.channelsHint', {
                    defaultValue: 'Mit Zugriff markierte Sender bleiben sichtbar. Favoriten werden im Guide nach vorne gezogen und koennen separat gefiltert werden.',
                  })}
                </span>
              </div>
              <div className={styles.profilePanelActions}>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => updateDraft({ allowedServiceRefs: [] })}
                >
                  {t('settings.household.clearAccess', { defaultValue: 'Alle Sender erlauben' })}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => updateDraft({ favoriteServiceRefs: [] })}
                >
                  {t('settings.household.clearFavorites', { defaultValue: 'Favoriten leeren' })}
                </Button>
              </div>
            </div>

            <input
              className={styles.profileInput}
              type="search"
              value={channelQuery}
              onChange={(event) => setChannelQuery(event.target.value)}
              placeholder={t('settings.household.channelSearch', { defaultValue: 'Sender suchen' })}
            />

            {channels.channels.length === 0 ? (
              <div className={styles.profileEmptyState}>
                {t('settings.household.noChannels', { defaultValue: 'Noch keine Sender geladen. Lade zuerst die Senderliste, um Profilrechte pro Sender zu pflegen.' })}
              </div>
            ) : (
              <div className={styles.channelChecklist}>
                {visibleChannels.map((channel) => {
                  const serviceRef = String(channel.serviceRef || channel.id || '').trim().toLowerCase();
                  if (!serviceRef) {
                    return null;
                  }

                  return (
                    <div key={serviceRef} className={styles.channelRow}>
                      <label className={styles.channelAccess}>
                        <input
                          type="checkbox"
                          checked={normalizedDraft.allowedServiceRefs.includes(serviceRef)}
                          onChange={() => toggleDraftListValue('allowedServiceRefs', serviceRef)}
                        />
                        <span>
                          <strong>{channel.name || serviceRef}</strong>
                          <small>{[channel.number, channel.group].filter(Boolean).join(' · ')}</small>
                        </span>
                      </label>

                      <label className={styles.channelFavorite}>
                        <input
                          type="checkbox"
                          checked={normalizedDraft.favoriteServiceRefs.includes(serviceRef)}
                          onChange={() => toggleDraftListValue('favoriteServiceRefs', serviceRef)}
                        />
                        <span>{t('settings.household.favorite', { defaultValue: 'Favorit' })}</span>
                      </label>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className={styles.section}>
        <h3>{t('settings.androidTv.title')}</h3>
        <p className={styles.subtitle}>{t('settings.androidTv.subtitle')}</p>

        <div className={styles.onboardingCard}>
          <div className={styles.onboardingHero}>
            <div className={styles.onboardingIntro}>
              <p className={styles.onboardingEyebrow}>{t('settings.androidTv.eyebrow')}</p>
              <h4 className={styles.onboardingTitle}>{t('settings.androidTv.headline')}</h4>
              <p className={styles.onboardingCopy}>{t('settings.androidTv.subtitle')}</p>
            </div>

            <div className={styles.onboardingSteps} aria-label={t('settings.androidTv.eyebrow')}>
              <div className={styles.stepCard}>
                <span className={`${styles.stepNumber} tabular`.trim()}>1</span>
                <p className={styles.stepLabel}>{t('settings.androidTv.steps.browser')}</p>
              </div>
              <div className={styles.stepCard}>
                <span className={`${styles.stepNumber} tabular`.trim()}>2</span>
                <p className={styles.stepLabel}>{t('settings.androidTv.steps.launch')}</p>
              </div>
              <div className={styles.stepCard}>
                <span className={`${styles.stepNumber} tabular`.trim()}>3</span>
                <p className={styles.stepLabel}>{t('settings.androidTv.steps.confirm')}</p>
              </div>
            </div>
          </div>

          <div className={styles.onboardingMeta}>
            <div className={styles.group}>
              <label>{t('settings.androidTv.currentServer')}</label>
              <code className={`${styles.launchValue} tabular`.trim()}>{androidTvBaseUrl}</code>
              <span className={styles.hint}>{t('settings.androidTv.currentServerHint')}</span>
            </div>

            <div className={styles.onboardingActions}>
              <Button
                href={androidTvLaunchUrl}
                className={styles.onboardingButton}
                rel="noopener noreferrer"
              >
                {t('settings.androidTv.openApp')}
              </Button>
              <p className={styles.hint}>{t('settings.androidTv.hint')}</p>
            </div>
          </div>
        </div>
      </div>

      <div className={styles.section}>
        <h3>{t('settings.streaming.scan.title')}</h3>
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

      <div className={styles.section}>
        <h3>{t('settings.streaming.title')}</h3>

        {/* Note: Profile selection removed in favor of Universal Policy */}
        <div className={styles.group}>
          <label>{t('settings.streaming.policy.label')}</label>
          <div className={styles.inputRow}>
            <input
              type="text"
              value={
                config?.streaming?.deliveryPolicy === 'universal'
                  ? t('settings.streaming.policy.universal')
                  : (config?.streaming?.deliveryPolicy || t('common.loading'))
              }
              disabled
              className={styles.inputReadonly}
            />
            <span className={styles.hint}>{t('settings.streaming.policy.hint')}</span>
          </div>
        </div>
      </div>

      {/* Adaptive Bitrate removed as per 2026 Design Contract (Trust Hardening) */}

      {/* ADR-00X: Saved message removed (was for profile save feedback) */}


      <div className={styles.footer}>
        <p>
          <strong>{t('settings.footer.noteTitle')}</strong> {t('settings.footer.noteBody')}
        </p>
      </div>
    </div>
  );
}

export default Settings;
