import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  HouseholdProfilesProvider,
  useHouseholdProfiles,
} from './HouseholdProfilesContext';
import { setClientAuthToken } from '../services/clientWrapper';
import {
  DEFAULT_HOUSEHOLD_PROFILE_ID,
  createDefaultHouseholdProfile,
  createHouseholdProfile,
  normalizeHouseholdProfile,
} from '../features/household/model';

const {
  fetchHouseholdProfiles,
  fetchHouseholdUnlockStatus,
  createRemoteHouseholdProfile,
  unlockHouseholdAccess,
  lockHouseholdAccess,
  updateRemoteHouseholdProfile,
  deleteRemoteHouseholdProfile,
  promptPin,
  toast,
} = vi.hoisted(() => ({
  fetchHouseholdProfiles: vi.fn(),
  fetchHouseholdUnlockStatus: vi.fn(),
  createRemoteHouseholdProfile: vi.fn(),
  unlockHouseholdAccess: vi.fn(),
  lockHouseholdAccess: vi.fn(),
  updateRemoteHouseholdProfile: vi.fn(),
  deleteRemoteHouseholdProfile: vi.fn(),
  promptPin: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../features/household/api', () => ({
  fetchHouseholdProfiles,
  fetchHouseholdUnlockStatus,
  createRemoteHouseholdProfile,
  unlockHouseholdAccess,
  lockHouseholdAccess,
  updateRemoteHouseholdProfile,
  deleteRemoteHouseholdProfile,
}));

vi.mock('./UiOverlayContext', () => ({
  useUiOverlay: () => ({
    promptPin,
    toast,
  }),
}));

const HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY = 'xg2g.household.selected-profile.v1';
const HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY = 'xg2g.household.profiles.sync.v1';

function HouseholdProfilesProbe() {
  const {
    profiles,
    selectedProfile,
    selectedProfileId,
    isReady,
    saveProfile,
    selectProfile,
    deleteProfile,
    toggleFavoriteService,
  } = useHouseholdProfiles();

  return (
    <div>
      <div data-testid="ready-state">{String(isReady)}</div>
      <div data-testid="profile-count">{profiles.length}</div>
      <div data-testid="selected-profile-id">{selectedProfileId}</div>
      <div data-testid="selected-profile-name">{selectedProfile.name}</div>
      <div data-testid="profiles-json">{JSON.stringify(profiles)}</div>
      <button
        type="button"
        onClick={() => {
          void saveProfile({
            ...createHouseholdProfile('child'),
            id: 'child-profile',
            name: 'Kids',
          });
        }}
      >
        Save Child
      </button>
      <button type="button" onClick={() => selectProfile('child-profile')}>
        Select Child
      </button>
      <button type="button" onClick={() => { void selectProfile(DEFAULT_HOUSEHOLD_PROFILE_ID); }}>
        Select Default
      </button>
      <button
        type="button"
        onClick={() => {
          void saveProfile({
            ...createHouseholdProfile('child'),
            id: 'child-profile',
            name: 'Kids Updated',
          });
        }}
      >
        Update Child
      </button>
      <button type="button" onClick={() => toggleFavoriteService('1:0:1:test')}>
        Toggle Favorite
      </button>
      <button
        type="button"
        onClick={() => {
          void deleteProfile('child-profile');
        }}
      >
        Delete Child
      </button>
      <button
        type="button"
        onClick={() => {
          void deleteProfile(DEFAULT_HOUSEHOLD_PROFILE_ID);
        }}
      >
        Delete Default
      </button>
    </div>
  );
}

async function renderProbe() {
  await act(async () => {
    render(
      <HouseholdProfilesProvider>
        <HouseholdProfilesProbe />
      </HouseholdProfilesProvider>
    );
    await Promise.resolve();
  });

  await waitFor(() => {
    expect(screen.getByTestId('ready-state')).toHaveTextContent('true');
  });
}

describe('HouseholdProfilesProvider', () => {
  beforeEach(() => {
    setClientAuthToken('test-token');
    fetchHouseholdProfiles.mockResolvedValue([createDefaultHouseholdProfile()]);
    fetchHouseholdUnlockStatus.mockResolvedValue({ pinConfigured: false, unlocked: false });
    createRemoteHouseholdProfile.mockImplementation(async (profile) => normalizeHouseholdProfile(profile));
    unlockHouseholdAccess.mockResolvedValue({ pinConfigured: true, unlocked: true });
    lockHouseholdAccess.mockResolvedValue(undefined);
    updateRemoteHouseholdProfile.mockImplementation(async (profile) => normalizeHouseholdProfile(profile));
    deleteRemoteHouseholdProfile.mockResolvedValue(undefined);
    promptPin.mockResolvedValue('1234');
  });

  afterEach(() => {
    vi.clearAllMocks();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  it('hydrates a default profile from the backend bootstrap', async () => {
    await renderProbe();

    await waitFor(() => {
      expect(screen.getByTestId('ready-state')).toHaveTextContent('true');
      expect(screen.getByTestId('profile-count')).toHaveTextContent('1');
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent(DEFAULT_HOUSEHOLD_PROFILE_ID);
      expect(screen.getByTestId('selected-profile-name')).toHaveTextContent('Haushalt');
    });

    expect(fetchHouseholdProfiles).toHaveBeenCalledTimes(1);
  });

  it('toggles favorites on the currently selected profile', async () => {
    await renderProbe();

    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Save Child' }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('profile-count')).toHaveTextContent('2');
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Select Child' }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent('child-profile');
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Toggle Favorite' }));
    });

    await waitFor(() => {
      expect(updateRemoteHouseholdProfile).toHaveBeenCalledWith(expect.objectContaining({
        id: 'child-profile',
        favoriteServiceRefs: ['1:0:1:test'],
      }));
    });

    const profiles = JSON.parse(screen.getByTestId('profiles-json').textContent || '[]');
    const defaultProfile = profiles.find((profile: { id: string }) => profile.id === DEFAULT_HOUSEHOLD_PROFILE_ID);
    const childProfile = profiles.find((profile: { id: string }) => profile.id === 'child-profile');

    expect(defaultProfile.favoriteServiceRefs).toEqual([]);
    expect(childProfile.favoriteServiceRefs).toEqual(['1:0:1:test']);
  });

  it('does not delete the last remaining profile', async () => {
    await renderProbe();

    await screen.findByTestId('selected-profile-id');
    fireEvent.click(screen.getByRole('button', { name: 'Delete Default' }));

    expect(deleteRemoteHouseholdProfile).not.toHaveBeenCalled();
    expect(screen.getByTestId('profile-count')).toHaveTextContent('1');
    expect(screen.getByTestId('selected-profile-id')).toHaveTextContent(DEFAULT_HOUSEHOLD_PROFILE_ID);
  });

  it('updates an existing profile instead of inserting a duplicate', async () => {
    await renderProbe();

    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Save Child' }));
    });
    await waitFor(() => {
      expect(createRemoteHouseholdProfile).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId('profile-count')).toHaveTextContent('2');
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Update Child' }));
    });

    await waitFor(() => {
      expect(updateRemoteHouseholdProfile).toHaveBeenCalledWith(expect.objectContaining({
        id: 'child-profile',
        name: 'Kids Updated',
      }));
    });

    const profiles = JSON.parse(screen.getByTestId('profiles-json').textContent || '[]');
    const childProfiles = profiles.filter((profile: { id: string }) => profile.id === 'child-profile');

    expect(childProfiles).toHaveLength(1);
    expect(childProfiles[0].name).toBe('Kids Updated');
  });

  it('falls back to the remaining profile when the selected profile is deleted', async () => {
    await renderProbe();

    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Save Child' }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('profile-count')).toHaveTextContent('2');
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Select Child' }));
    });
    await waitFor(() => {
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent('child-profile');
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete Child' }));
    });

    await waitFor(() => {
      expect(deleteRemoteHouseholdProfile).toHaveBeenCalledWith('child-profile');
      expect(screen.getByTestId('profile-count')).toHaveTextContent('1');
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent(DEFAULT_HOUSEHOLD_PROFILE_ID);
      expect(screen.getByTestId('selected-profile-name')).toHaveTextContent('Haushalt');
    });
  });

  it('syncs profile changes from storage events across tabs', async () => {
    await renderProbe();

    await screen.findByTestId('selected-profile-id');

    const syncedChildProfile = {
      ...createHouseholdProfile('child'),
      id: 'synced-child',
      name: 'Synced Child',
    };

    fetchHouseholdProfiles.mockResolvedValueOnce([
      createDefaultHouseholdProfile(),
      syncedChildProfile,
    ]);

    act(() => {
      window.localStorage.setItem(HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY, syncedChildProfile.id);
      window.dispatchEvent(new StorageEvent('storage', {
        key: HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY,
        storageArea: window.localStorage,
      }));
      window.localStorage.setItem(HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY, String(Date.now()));
      window.dispatchEvent(new StorageEvent('storage', {
        key: HOUSEHOLD_PROFILES_SYNC_STORAGE_KEY,
        storageArea: window.localStorage,
      }));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-count')).toHaveTextContent('2');
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent('synced-child');
      expect(screen.getByTestId('selected-profile-name')).toHaveTextContent('Synced Child');
    });
  });

  it('prompts for the household pin before switching to an adult profile when locked', async () => {
    fetchHouseholdProfiles.mockResolvedValue([
      createDefaultHouseholdProfile(),
      {
        ...createHouseholdProfile('child'),
        id: 'child-profile',
        name: 'Kids',
      },
    ]);
    fetchHouseholdUnlockStatus.mockResolvedValue({ pinConfigured: true, unlocked: false });
    window.localStorage.setItem(HOUSEHOLD_SELECTED_PROFILE_STORAGE_KEY, 'child-profile');

    await renderProbe();

    await waitFor(() => {
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent('child-profile');
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Select Default' }));
    });

    await waitFor(() => {
      expect(promptPin).toHaveBeenCalledTimes(1);
      expect(unlockHouseholdAccess).toHaveBeenCalledWith('1234');
      expect(screen.getByTestId('selected-profile-id')).toHaveTextContent(DEFAULT_HOUSEHOLD_PROFILE_ID);
    });
  });
});
