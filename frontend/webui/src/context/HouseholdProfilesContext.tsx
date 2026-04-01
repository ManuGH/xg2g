import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  CLIENT_AUTH_CHANGED_EVENT,
  getClientAuthToken,
  setClientHouseholdProfileId,
} from '../services/clientWrapper';
import {
  createRemoteHouseholdProfile,
  deleteRemoteHouseholdProfile,
  fetchHouseholdProfiles,
  fetchHouseholdUnlockStatus,
  lockHouseholdAccess,
  unlockHouseholdAccess,
  updateRemoteHouseholdProfile,
} from '../features/household/api';
import {
  DEFAULT_HOUSEHOLD_PROFILE_ID,
  canProfileAccessDvrPlayback,
  canProfileAccessSettings,
  canProfileManageDvr,
  createDefaultHouseholdProfile,
  isFavoriteService as isFavoriteServiceForProfile,
  normalizeHouseholdProfile,
  type HouseholdProfile,
} from '../features/household/model';
import { useUiOverlay } from './UiOverlayContext';
import { formatError } from '../utils/logging';

const HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY = 'xg2g.household.selected-profile.v1';
const HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY = 'xg2g.household.profiles.sync.v1';

interface UnlockPromptOptions {
  title?: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

interface HouseholdProfilesContextValue {
  profiles: HouseholdProfile[];
  selectedProfile: HouseholdProfile;
  selectedProfileId: string;
  isReady: boolean;
  pinConfigured: boolean;
  isUnlocked: boolean;
  selectProfile: (profileId: string) => Promise<boolean>;
  ensureUnlocked: (options?: UnlockPromptOptions) => Promise<boolean>;
  saveProfile: (profile: HouseholdProfile) => Promise<void>;
  deleteProfile: (profileId: string) => Promise<void>;
  toggleFavoriteService: (serviceRef: string) => void;
  isFavoriteService: (serviceRef: string | null | undefined) => boolean;
  canAccessDvrPlayback: boolean;
  canManageDvr: boolean;
  canAccessSettings: boolean;
}

const HouseholdProfilesContext = createContext<HouseholdProfilesContextValue | undefined>(undefined);

export function useHouseholdProfiles(): HouseholdProfilesContextValue {
  const context = useContext(HouseholdProfilesContext);
  if (!context) {
    throw new Error('useHouseholdProfiles must be used within HouseholdProfilesProvider');
  }
  return context;
}

export function HouseholdProfilesProvider({ children }: { children: ReactNode }) {
  const { promptPin, toast } = useUiOverlay();
  const [profiles, setProfiles] = useState<HouseholdProfile[]>(() => [createDefaultHouseholdProfile()]);
  const [selectedProfileId, setSelectedProfileId] = useState<string>(() => readStoredSelectedProfileId());
  const [authToken, setAuthToken] = useState<string | null>(() => getClientAuthToken());
  const [isReady, setIsReady] = useState<boolean>(() => !getClientAuthToken());
  const [pinConfigured, setPinConfigured] = useState<boolean>(false);
  const [isUnlocked, setIsUnlocked] = useState<boolean>(false);
  const profilesRef = useRef(profiles);
  const selectedProfileIdRef = useRef(selectedProfileId);
  const authTokenRef = useRef(authToken);
  const pinConfiguredRef = useRef(pinConfigured);
  const isUnlockedRef = useRef(isUnlocked);

  useEffect(() => {
    profilesRef.current = profiles;
  }, [profiles]);

  useEffect(() => {
    selectedProfileIdRef.current = selectedProfileId;
  }, [selectedProfileId]);

  useEffect(() => {
    authTokenRef.current = authToken;
  }, [authToken]);

  useEffect(() => {
    pinConfiguredRef.current = pinConfigured;
  }, [pinConfigured]);

  useEffect(() => {
    isUnlockedRef.current = isUnlocked;
  }, [isUnlocked]);

  const refreshProfiles = useCallback(async (): Promise<HouseholdProfile[]> => {
    const fetchedProfiles = await fetchHouseholdProfiles();
    const nextProfiles = fetchedProfiles && fetchedProfiles.length > 0
      ? fetchedProfiles
      : [createDefaultHouseholdProfile()];

    setProfiles(nextProfiles);
    return nextProfiles;
  }, []);

  const refreshUnlockState = useCallback(async (): Promise<{ pinConfigured: boolean; unlocked: boolean }> => {
    const status = await fetchHouseholdUnlockStatus();
    setPinConfigured(status.pinConfigured);
    setIsUnlocked(status.unlocked);
    return status;
  }, []);

  useEffect(() => {
    const handleAuthChanged = () => {
      setAuthToken(getClientAuthToken());
    };

    window.addEventListener(CLIENT_AUTH_CHANGED_EVENT, handleAuthChanged as EventListener);
    return () => window.removeEventListener(CLIENT_AUTH_CHANGED_EVENT, handleAuthChanged as EventListener);
  }, []);

  useEffect(() => {
    let cancelled = false;

    if (!authToken) {
      setPinConfigured(false);
      setIsUnlocked(false);
      setIsReady(true);
      return () => {
        cancelled = true;
      };
    }

    setIsReady(false);

    void (async () => {
      try {
        const [nextProfiles, unlockStatus] = await Promise.all([
          refreshProfiles(),
          refreshUnlockState(),
        ]);
        if (cancelled) {
          return;
        }

        const nextSelectedProfileId = resolveSelectedProfileId(nextProfiles, selectedProfileIdRef.current, unlockStatus);
        setSelectedProfileId(nextSelectedProfileId);
      } catch {
        if (cancelled) {
          return;
        }

        const fallbackProfiles = profilesRef.current.length > 0
          ? profilesRef.current
          : [createDefaultHouseholdProfile()];
        setProfiles(fallbackProfiles);
        setSelectedProfileId(resolveSelectedProfileId(
          fallbackProfiles,
          selectedProfileIdRef.current,
          { pinConfigured: pinConfiguredRef.current, unlocked: isUnlockedRef.current }
        ));
      } finally {
        if (!cancelled) {
          setIsReady(true);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [authToken, refreshProfiles, refreshUnlockState]);

  const selectedProfile = useMemo(() => {
    const match = profiles.find((profile) => profile.id === selectedProfileId);
    return match ?? profiles[0] ?? createDefaultHouseholdProfile();
  }, [profiles, selectedProfileId]);

  useEffect(() => {
    const nextSelectedProfileId = resolveSelectedProfileId(
      profiles,
      selectedProfileId,
      { pinConfigured, unlocked: isUnlocked }
    );
    if (nextSelectedProfileId === selectedProfileId) {
      return;
    }

    setSelectedProfileId(nextSelectedProfileId);
  }, [isUnlocked, pinConfigured, profiles, selectedProfileId]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    window.localStorage.setItem(HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY, selectedProfile.id);
  }, [selectedProfile.id]);

  useEffect(() => {
    if (!authToken || !isReady) {
      setClientHouseholdProfileId(null);
      return;
    }

    if (pinConfigured && !isUnlocked && selectedProfile.kind === 'adult') {
      setClientHouseholdProfileId(null);
      return;
    }

    setClientHouseholdProfileId(selectedProfile.id);
  }, [authToken, isReady, isUnlocked, pinConfigured, selectedProfile.id, selectedProfile.kind]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    const handleStorage = (event: StorageEvent) => {
      if (event.storageArea && event.storageArea !== window.localStorage) {
        return;
      }

      if (event.key === HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY || event.key === null) {
        setSelectedProfileId(readStoredSelectedProfileId());
      }

      if ((!event.key || event.key === HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY) && authTokenRef.current) {
        void refreshProfiles().then((nextProfiles) => {
          const nextSelectedProfileId = resolveSelectedProfileId(
            nextProfiles,
            readStoredSelectedProfileId(),
            { pinConfigured: pinConfiguredRef.current, unlocked: isUnlockedRef.current }
          );
          setSelectedProfileId(nextSelectedProfileId);
        }).catch(() => {
          // Cross-tab refresh is best-effort. The current tab keeps its last known state on failure.
        });
      }
    };

    window.addEventListener('storage', handleStorage);
    return () => window.removeEventListener('storage', handleStorage);
  }, [refreshProfiles]);

  const ensureUnlocked = useCallback(async (options?: UnlockPromptOptions): Promise<boolean> => {
    if (!authTokenRef.current || !pinConfiguredRef.current) {
      return true;
    }
    if (isUnlockedRef.current) {
      return true;
    }

    const pin = await promptPin({
      title: options?.title ?? 'Haushalt-PIN',
      message: options?.message ?? 'Dieser Bereich ist mit dem Haushalt-PIN geschützt.',
      confirmLabel: options?.confirmLabel ?? 'Freischalten',
      cancelLabel: options?.cancelLabel ?? 'Abbrechen',
      placeholder: 'PIN',
    });
    if (!pin) {
      return false;
    }

    try {
      const status = await unlockHouseholdAccess(pin);
      setPinConfigured(status.pinConfigured);
      setIsUnlocked(status.unlocked);
      if (!status.unlocked) {
        toast({
          kind: 'error',
          message: 'Haushalt-PIN konnte nicht freigeschaltet werden.',
        });
      }
      return status.unlocked;
    } catch (error) {
      toast({
        kind: 'error',
        message: `PIN ungültig oder Freischaltung fehlgeschlagen: ${formatError(error)}`,
      });
      return false;
    }
  }, [promptPin, toast]);

  const lockActiveSession = useCallback(async () => {
    if (!pinConfiguredRef.current || !isUnlockedRef.current) {
      setIsUnlocked(false);
      return;
    }

    try {
      await lockHouseholdAccess();
    } catch {
      // Fail closed in the UI even if the server-side cleanup could not be confirmed.
    } finally {
      setIsUnlocked(false);
    }
  }, []);

  const selectProfile = useCallback(async (profileId: string): Promise<boolean> => {
    const normalizedProfileId = String(profileId || '').trim();
    if (!normalizedProfileId) {
      return false;
    }

    const targetProfile = profilesRef.current.find((profile) => profile.id === normalizedProfileId);
    if (!targetProfile) {
      return false;
    }
    if (targetProfile.id === selectedProfileIdRef.current) {
      return true;
    }

    if (targetProfile.kind === 'adult') {
      const unlocked = await ensureUnlocked({
        title: 'Erwachsenenprofil',
        message: `Der Wechsel zu "${targetProfile.name}" erfordert den Haushalt-PIN.`,
        confirmLabel: 'Profil öffnen',
      });
      if (!unlocked) {
        return false;
      }
    } else {
      await lockActiveSession();
    }

    selectedProfileIdRef.current = normalizedProfileId;
    setSelectedProfileId(normalizedProfileId);
    return true;
  }, [ensureUnlocked, lockActiveSession]);

  const saveProfile = useCallback(async (profile: HouseholdProfile) => {
    const normalizedProfile = normalizeHouseholdProfile(profile);
    const existingProfile = profilesRef.current.find((entry) => entry.id === normalizedProfile.id);
    const savedProfile = existingProfile
      ? await updateRemoteHouseholdProfile(normalizedProfile)
      : await createRemoteHouseholdProfile(normalizedProfile);

    setProfiles((current) => upsertProfile(current, savedProfile));
    emitProfilesSync();
  }, []);

  const deleteProfile = useCallback(async (profileId: string) => {
    if (profilesRef.current.length <= 1) {
      return;
    }

    await deleteRemoteHouseholdProfile(profileId);

    const nextProfiles = profilesRef.current.filter((profile) => profile.id !== profileId);
    const resolvedProfiles = nextProfiles.length > 0 ? nextProfiles : [createDefaultHouseholdProfile()];
    setProfiles(resolvedProfiles);
    setSelectedProfileId((current) => resolveSelectedProfileId(
      resolvedProfiles,
      current,
      { pinConfigured: pinConfiguredRef.current, unlocked: isUnlockedRef.current }
    ));
    emitProfilesSync();
  }, []);

  const toggleFavoriteService = useCallback((serviceRef: string) => {
    const normalizedServiceRef = String(serviceRef || '').trim().toLowerCase();
    if (!normalizedServiceRef) {
      return;
    }

    const currentProfiles = profilesRef.current;
    const activeProfile = currentProfiles.find((profile) => profile.id === selectedProfileIdRef.current);
    if (!activeProfile) {
      return;
    }

    const nextFavorites = activeProfile.favoriteServiceRefs.includes(normalizedServiceRef)
      ? activeProfile.favoriteServiceRefs.filter((entry) => entry !== normalizedServiceRef)
      : [...activeProfile.favoriteServiceRefs, normalizedServiceRef];
    const nextProfile = normalizeHouseholdProfile({
      ...activeProfile,
      favoriteServiceRefs: nextFavorites,
    });
    const previousProfiles = currentProfiles;

    setProfiles((current) => current.map((profile) => (
      profile.id === nextProfile.id ? nextProfile : profile
    )));

    void updateRemoteHouseholdProfile(nextProfile)
      .then((savedProfile) => {
        setProfiles((current) => upsertProfile(current, savedProfile));
        emitProfilesSync();
      })
      .catch(() => {
        setProfiles(previousProfiles);
        void refreshProfiles().catch(() => {
          // Keep the local rollback if reconciliation fails.
        });
      });
  }, [refreshProfiles]);

  const isFavoriteService = useCallback((serviceRef: string | null | undefined) => {
    return isFavoriteServiceForProfile(selectedProfile, serviceRef);
  }, [selectedProfile]);

  const selectedProfileLocked = pinConfigured && !isUnlocked && selectedProfile.kind === 'adult';

  const value = useMemo<HouseholdProfilesContextValue>(() => ({
    profiles,
    selectedProfile,
    selectedProfileId: selectedProfile.id,
    isReady,
    pinConfigured,
    isUnlocked,
    selectProfile,
    ensureUnlocked,
    saveProfile,
    deleteProfile,
    toggleFavoriteService,
    isFavoriteService,
    canAccessDvrPlayback: !selectedProfileLocked && canProfileAccessDvrPlayback(selectedProfile),
    canManageDvr: !selectedProfileLocked && canProfileManageDvr(selectedProfile),
    canAccessSettings: !selectedProfileLocked && canProfileAccessSettings(selectedProfile),
  }), [
    deleteProfile,
    ensureUnlocked,
    isFavoriteService,
    isReady,
    isUnlocked,
    pinConfigured,
    profiles,
    saveProfile,
    selectProfile,
    selectedProfile,
    selectedProfileLocked,
    toggleFavoriteService,
  ]);

  return (
    <HouseholdProfilesContext.Provider value={value}>
      {children}
    </HouseholdProfilesContext.Provider>
  );
}

function readStoredSelectedProfileId(): string {
  if (typeof window === 'undefined') {
    return DEFAULT_HOUSEHOLD_PROFILE_ID;
  }

  return window.localStorage.getItem(HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY)?.trim() || DEFAULT_HOUSEHOLD_PROFILE_ID;
}

function emitProfilesSync(): void {
  if (typeof window === 'undefined') {
    return;
  }

  window.localStorage.setItem(HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY, String(Date.now()));
}

function resolveSelectedProfileId(
  profiles: HouseholdProfile[],
  profileId: string,
  unlockState: { pinConfigured: boolean; unlocked: boolean }
): string {
  const normalizedProfileId = String(profileId || '').trim();
  const requestedProfile = normalizedProfileId
    ? profiles.find((profile) => profile.id === normalizedProfileId)
    : null;

  if (requestedProfile && (!unlockState.pinConfigured || unlockState.unlocked || requestedProfile.kind !== 'adult')) {
    return requestedProfile.id;
  }

  const unlockedCandidate = profiles.find((profile) => profile.kind !== 'adult');
  if (unlockState.pinConfigured && !unlockState.unlocked && unlockedCandidate) {
    return unlockedCandidate.id;
  }

  if (requestedProfile) {
    return requestedProfile.id;
  }

  return profiles[0]?.id || DEFAULT_HOUSEHOLD_PROFILE_ID;
}

function upsertProfile(profiles: HouseholdProfile[], profile: HouseholdProfile): HouseholdProfile[] {
  const existingIndex = profiles.findIndex((entry) => entry.id === profile.id);
  if (existingIndex === -1) {
    return [...profiles, profile];
  }

  const nextProfiles = [...profiles];
  nextProfiles[existingIndex] = profile;
  return nextProfiles;
}
