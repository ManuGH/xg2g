import type { Bouquet, RecordingItem, Service, Timer } from '../../client-ts';

export type HouseholdProfileKind = 'adult' | 'child';

export interface HouseholdProfilePermissions {
  dvrPlayback: boolean;
  dvrManage: boolean;
  settings: boolean;
}

export interface HouseholdProfile {
  id: string;
  name: string;
  kind: HouseholdProfileKind;
  maxFsk: number | null;
  allowedBouquets: string[];
  allowedServiceRefs: string[];
  favoriteServiceRefs: string[];
  permissions: HouseholdProfilePermissions;
}

type ServiceLike = Pick<Service, 'group' | 'id' | 'serviceRef'>;
type RecordingLike = Pick<RecordingItem, 'serviceRef'>;
type TimerLike = Pick<Timer, 'serviceRef'>;

export const DEFAULT_HOUSEHOLD_PROFILE_ID = 'household-default';

export function createDefaultHouseholdProfile(): HouseholdProfile {
  return {
    id: DEFAULT_HOUSEHOLD_PROFILE_ID,
    name: 'Haushalt',
    kind: 'adult',
    maxFsk: null,
    allowedBouquets: [],
    allowedServiceRefs: [],
    favoriteServiceRefs: [],
    permissions: {
      dvrPlayback: true,
      dvrManage: true,
      settings: true,
    },
  };
}

export function createHouseholdProfile(kind: HouseholdProfileKind = 'adult'): HouseholdProfile {
  return normalizeHouseholdProfile({
    id: createProfileId(),
    name: kind === 'child' ? 'Kinderprofil' : 'Neues Profil',
    kind,
    maxFsk: kind === 'child' ? 12 : null,
    permissions: {
      dvrPlayback: true,
      dvrManage: kind === 'adult',
      settings: kind === 'adult',
    },
  });
}

export function normalizeHouseholdProfiles(profiles: HouseholdProfile[] | null | undefined): HouseholdProfile[] {
  if (!Array.isArray(profiles) || profiles.length === 0) {
    return [createDefaultHouseholdProfile()];
  }

  const normalized = profiles
    .filter((profile): profile is HouseholdProfile => Boolean(profile && typeof profile === 'object'))
    .map(normalizeHouseholdProfile);

  return normalized.length > 0 ? normalized : [createDefaultHouseholdProfile()];
}

export function normalizeHouseholdProfile(profile: Partial<HouseholdProfile> | null | undefined): HouseholdProfile {
  const fallback = createDefaultHouseholdProfile();
  const normalizedKind: HouseholdProfileKind = profile?.kind === 'child' ? 'child' : 'adult';

  return {
    id: normalizeIdentifier(profile?.id) || createProfileId(),
    name: normalizeName(profile?.name) || fallback.name,
    kind: normalizedKind,
    maxFsk: normalizeMaxFsk(profile?.maxFsk),
    allowedBouquets: normalizeStringList(profile?.allowedBouquets),
    allowedServiceRefs: normalizeStringList(profile?.allowedServiceRefs),
    favoriteServiceRefs: normalizeStringList(profile?.favoriteServiceRefs),
    permissions: {
      dvrPlayback: profile?.permissions?.dvrPlayback ?? true,
      dvrManage: profile?.permissions?.dvrManage ?? normalizedKind === 'adult',
      settings: profile?.permissions?.settings ?? normalizedKind === 'adult',
    },
  };
}

export function isServiceAllowedForProfile(profile: HouseholdProfile, service: ServiceLike | null | undefined): boolean {
  return isServiceAllowedForNormalizedProfile(normalizeHouseholdProfile(profile), service);
}

export function filterServicesForProfile(profile: HouseholdProfile, services: Service[]): Service[] {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  return services.filter((service) => isServiceAllowedForNormalizedProfile(normalizedProfile, service));
}

// Currently unused in the web slice. Keep this helper for planned server-side
// bouquet filtering so the household access model stays symmetrical.
export function filterBouquetsForProfile(profile: HouseholdProfile, bouquets: Bouquet[]): Bouquet[] {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  if (normalizedProfile.allowedBouquets.length === 0) {
    return bouquets;
  }

  return bouquets.filter((bouquet) => normalizedProfile.allowedBouquets.includes(normalizeIdentifier(bouquet.name)));
}

export function filterRecordingsForProfile(profile: HouseholdProfile, recordings: RecordingItem[]): RecordingItem[] {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  return recordings.filter((recording) => isRecordingAllowedForNormalizedProfile(normalizedProfile, recording));
}

export function filterTimersForProfile(profile: HouseholdProfile, timers: Timer[]): Timer[] {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  return timers.filter((timer) => isTimerAllowedForNormalizedProfile(normalizedProfile, timer));
}

export function isRecordingAllowedForProfile(profile: HouseholdProfile, recording: RecordingLike | null | undefined): boolean {
  return isRecordingAllowedForNormalizedProfile(normalizeHouseholdProfile(profile), recording);
}

export function isTimerAllowedForProfile(profile: HouseholdProfile, timer: TimerLike | null | undefined): boolean {
  return isTimerAllowedForNormalizedProfile(normalizeHouseholdProfile(profile), timer);
}

export function isFavoriteService(profile: HouseholdProfile, serviceRef: string | null | undefined): boolean {
  return isFavoriteServiceForNormalizedProfile(normalizeHouseholdProfile(profile), serviceRef);
}

export function sortServicesForProfile(profile: HouseholdProfile, services: Service[]): Service[] {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  const favoriteRefs = new Set(normalizedProfile.favoriteServiceRefs);

  return services
    .map((service, index) => ({ service, index }))
    .sort((left, right) => {
      const leftRef = normalizeIdentifier(left.service.serviceRef || left.service.id);
      const rightRef = normalizeIdentifier(right.service.serviceRef || right.service.id);
      const leftFavorite = leftRef ? favoriteRefs.has(leftRef) : false;
      const rightFavorite = rightRef ? favoriteRefs.has(rightRef) : false;

      if (leftFavorite !== rightFavorite) {
        return leftFavorite ? -1 : 1;
      }

      return left.index - right.index;
    })
    .map((entry) => entry.service);
}

export function canProfileAccessDvrPlayback(profile: HouseholdProfile): boolean {
  return normalizeHouseholdProfile(profile).permissions.dvrPlayback;
}

export function canProfileManageDvr(profile: HouseholdProfile): boolean {
  return normalizeHouseholdProfile(profile).permissions.dvrManage;
}

export function canProfileAccessSettings(profile: HouseholdProfile): boolean {
  return normalizeHouseholdProfile(profile).permissions.settings;
}

function isServiceAllowedForNormalizedProfile(profile: HouseholdProfile, service: ServiceLike | null | undefined): boolean {
  const serviceRef = normalizeIdentifier(service?.serviceRef || service?.id);
  const bouquet = normalizeIdentifier(service?.group);
  const hasRestrictions = profile.allowedBouquets.length > 0 || profile.allowedServiceRefs.length > 0;

  if (!hasRestrictions) {
    return true;
  }

  if (serviceRef && profile.allowedServiceRefs.includes(serviceRef)) {
    return true;
  }

  if (bouquet && profile.allowedBouquets.includes(bouquet)) {
    return true;
  }

  return false;
}

function isRecordingAllowedForNormalizedProfile(profile: HouseholdProfile, recording: RecordingLike | null | undefined): boolean {
  const serviceRef = normalizeIdentifier(recording?.serviceRef);
  if (!serviceRef) {
    return true;
  }

  return isServiceAllowedForNormalizedProfile(profile, { serviceRef });
}

function isTimerAllowedForNormalizedProfile(profile: HouseholdProfile, timer: TimerLike | null | undefined): boolean {
  const serviceRef = normalizeIdentifier(timer?.serviceRef);
  if (!serviceRef) {
    return true;
  }

  return isServiceAllowedForNormalizedProfile(profile, { serviceRef });
}

function isFavoriteServiceForNormalizedProfile(profile: HouseholdProfile, serviceRef: string | null | undefined): boolean {
  const normalizedServiceRef = normalizeIdentifier(serviceRef);
  if (!normalizedServiceRef) {
    return false;
  }

  return profile.favoriteServiceRefs.includes(normalizedServiceRef);
}

function normalizeIdentifier(value: string | null | undefined): string {
  return String(value || '').trim().toLowerCase();
}

function normalizeName(value: string | null | undefined): string {
  return String(value || '').trim();
}

function normalizeMaxFsk(value: number | null | undefined): number | null {
  if (typeof value !== 'number' || Number.isNaN(value)) {
    return null;
  }

  const rounded = Math.round(value);
  if (rounded < 0) {
    return 0;
  }

  return rounded;
}

function normalizeStringList(values: string[] | null | undefined): string[] {
  if (!Array.isArray(values) || values.length === 0) {
    return [];
  }

  return Array.from(
    new Set(
      values
        .map((value) => normalizeIdentifier(value))
        .filter(Boolean)
    )
  );
}

function createProfileId(): string {
  try {
    return `profile-${crypto.randomUUID()}`;
  } catch {
    return `profile-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
  }
}
