import {
  deleteHouseholdUnlock,
  deleteHouseholdProfile,
  deleteSession,
  getHouseholdProfiles,
  getHouseholdUnlock,
  postHouseholdProfiles,
  postHouseholdUnlock,
  putHouseholdProfile,
  type HouseholdProfile as ApiHouseholdProfile,
  type HouseholdUnlockStatus as ApiHouseholdUnlockStatus,
} from '../../client-ts';
import {
  HOUSEHOLD_PROFILE_HEADER,
  throwOnClientResultError,
  unwrapClientResultOrThrow,
} from '../../services/clientWrapper';
import {
  normalizeHouseholdProfile,
  normalizeHouseholdProfiles,
  type HouseholdProfile,
} from './model';

export async function fetchHouseholdProfiles(): Promise<HouseholdProfile[] | null> {
  const result = await getHouseholdProfiles({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: null,
    } as Record<string, unknown>,
  });

  if (result.error) {
    const status = result.response?.status;
    if (status === 401 || status === 403) {
      return null;
    }
    throwOnClientResultError(result, { source: 'fetchHouseholdProfiles' });
  }

  return normalizeHouseholdProfiles((result.data || []).map(fromApiHouseholdProfile));
}

export async function createRemoteHouseholdProfile(profile: HouseholdProfile): Promise<HouseholdProfile> {
  const result = await postHouseholdProfiles({
    body: toApiHouseholdProfile(profile),
  });

  return fromApiHouseholdProfile(
    unwrapClientResultOrThrow<ApiHouseholdProfile>(result, { source: 'createRemoteHouseholdProfile' })
  );
}

export async function updateRemoteHouseholdProfile(profile: HouseholdProfile): Promise<HouseholdProfile> {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  const result = await putHouseholdProfile({
    path: { profileId: normalizedProfile.id },
    body: toApiHouseholdProfile(normalizedProfile),
  });

  return fromApiHouseholdProfile(
    unwrapClientResultOrThrow<ApiHouseholdProfile>(result, { source: 'updateRemoteHouseholdProfile' })
  );
}

export async function deleteRemoteHouseholdProfile(profileId: string): Promise<void> {
  const result = await deleteHouseholdProfile({
    path: { profileId },
  });

  unwrapClientResultOrThrow<void>(result, { source: 'deleteRemoteHouseholdProfile' });
}

export interface HouseholdUnlockStatus {
  pinConfigured: boolean;
  unlocked: boolean;
}

export async function fetchHouseholdUnlockStatus(): Promise<HouseholdUnlockStatus> {
  const result = await getHouseholdUnlock({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: null,
    } as Record<string, unknown>,
  });

  return fromApiHouseholdUnlockStatus(
    unwrapClientResultOrThrow<ApiHouseholdUnlockStatus>(result, { source: 'fetchHouseholdUnlockStatus' })
  );
}

export async function unlockHouseholdAccess(pin: string): Promise<HouseholdUnlockStatus> {
  const result = await postHouseholdUnlock({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: null,
    } as Record<string, unknown>,
    body: { pin },
  });

  return fromApiHouseholdUnlockStatus(
    unwrapClientResultOrThrow<ApiHouseholdUnlockStatus>(result, { source: 'unlockHouseholdAccess' })
  );
}

export async function lockHouseholdAccess(): Promise<void> {
  const result = await deleteHouseholdUnlock({
    headers: {
      [HOUSEHOLD_PROFILE_HEADER]: null,
    } as Record<string, unknown>,
  });

  unwrapClientResultOrThrow<void>(result, { source: 'lockHouseholdAccess' });
}

export async function deleteServerSession(): Promise<void> {
  const result = await deleteSession();
  unwrapClientResultOrThrow<void>(result, { source: 'deleteServerSession' });
}

function fromApiHouseholdProfile(profile: ApiHouseholdProfile): HouseholdProfile {
  return normalizeHouseholdProfile({
    id: profile.id,
    name: profile.name,
    kind: profile.kind,
    maxFsk: profile.maxFsk ?? null,
    allowedBouquets: profile.allowedBouquets,
    allowedServiceRefs: profile.allowedServiceRefs,
    favoriteServiceRefs: profile.favoriteServiceRefs,
    permissions: {
      dvrPlayback: profile.permissions.dvrPlayback,
      dvrManage: profile.permissions.dvrManage,
      settings: profile.permissions.settings,
    },
  });
}

function toApiHouseholdProfile(profile: HouseholdProfile): ApiHouseholdProfile {
  const normalizedProfile = normalizeHouseholdProfile(profile);
  return {
    id: normalizedProfile.id,
    name: normalizedProfile.name,
    kind: normalizedProfile.kind,
    maxFsk: normalizedProfile.maxFsk,
    allowedBouquets: [...normalizedProfile.allowedBouquets],
    allowedServiceRefs: [...normalizedProfile.allowedServiceRefs],
    favoriteServiceRefs: [...normalizedProfile.favoriteServiceRefs],
    permissions: {
      dvrPlayback: normalizedProfile.permissions.dvrPlayback,
      dvrManage: normalizedProfile.permissions.dvrManage,
      settings: normalizedProfile.permissions.settings,
    },
  };
}

function fromApiHouseholdUnlockStatus(status: ApiHouseholdUnlockStatus): HouseholdUnlockStatus {
  return {
    pinConfigured: Boolean(status.pinConfigured),
    unlocked: Boolean(status.unlocked),
  };
}
