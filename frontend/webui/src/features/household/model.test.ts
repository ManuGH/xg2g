import { describe, expect, it } from 'vitest';
import {
  createDefaultHouseholdProfile,
  filterRecordingsForProfile,
  filterServicesForProfile,
  filterTimersForProfile,
  normalizeHouseholdProfiles,
  sortServicesForProfile,
} from './model';

describe('household profile model', () => {
  it('falls back to a default profile when none exist', () => {
    const profiles = normalizeHouseholdProfiles([]);

    expect(profiles).toHaveLength(1);
    expect(profiles[0]).toMatchObject(createDefaultHouseholdProfile());
  });

  it('filters services by bouquet and explicit sender access', () => {
    const profile = {
      ...createDefaultHouseholdProfile(),
      allowedBouquets: ['kids'],
      allowedServiceRefs: ['1:0:1:adult-news'],
    };

    const services = [
      { serviceRef: '1:0:1:kids-one', name: 'Kids One', group: 'Kids' },
      { serviceRef: '1:0:1:adult-news', name: 'Adult News', group: 'News' },
      { serviceRef: '1:0:1:sports', name: 'Sports', group: 'Sports' },
    ];

    expect(filterServicesForProfile(profile, services as any)).toEqual([
      services[0],
      services[1],
    ]);
  });

  it('sorts favorite services before the rest without losing the original order', () => {
    const profile = {
      ...createDefaultHouseholdProfile(),
      favoriteServiceRefs: ['1:0:1:two'],
    };

    const services = [
      { serviceRef: '1:0:1:one', name: 'One' },
      { serviceRef: '1:0:1:two', name: 'Two' },
      { serviceRef: '1:0:1:three', name: 'Three' },
    ];

    expect(sortServicesForProfile(profile, services as any)).toEqual([
      services[1],
      services[0],
      services[2],
    ]);
  });

  it('filters recordings and timers through the same sender rules', () => {
    const profile = {
      ...createDefaultHouseholdProfile(),
      allowedServiceRefs: ['1:0:1:allowed'],
    };

    const recordings = [
      { serviceRef: '1:0:1:allowed', title: 'Allowed recording' },
      { serviceRef: '1:0:1:blocked', title: 'Blocked recording' },
    ];
    const timers = [
      { serviceRef: '1:0:1:allowed', begin: 1, end: 2 },
      { serviceRef: '1:0:1:blocked', begin: 3, end: 4 },
    ];

    expect(filterRecordingsForProfile(profile, recordings as any)).toEqual([recordings[0]]);
    expect(filterTimersForProfile(profile, timers as any)).toEqual([timers[0]]);
  });
});
