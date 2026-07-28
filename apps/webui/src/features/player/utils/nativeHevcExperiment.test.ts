import { afterEach, describe, expect, it } from 'vitest';
import {
  isNativeHevcSafariExperimentEnabled,
  isNativeHevcSafariKillSwitchOn,
} from './nativeHevcExperiment';

afterEach(() => {
  window.localStorage.clear();
});

describe('nativeHevcExperiment flag', () => {
  it('is off by default', () => {
    expect(isNativeHevcSafariExperimentEnabled()).toBe(false);
    expect(isNativeHevcSafariKillSwitchOn()).toBe(false);
  });

  it('enables via localStorage', () => {
    window.localStorage.setItem('XG2G_NATIVE_HEVC_SAFARI', '1');
    expect(isNativeHevcSafariExperimentEnabled()).toBe(true);
  });

  it('respects the kill switch (checked first, forces off)', () => {
    window.localStorage.setItem('XG2G_NATIVE_HEVC_SAFARI', '1');
    window.localStorage.setItem('XG2G_NATIVE_HEVC_SAFARI_KILL', '1');
    expect(isNativeHevcSafariKillSwitchOn()).toBe(true);
    expect(isNativeHevcSafariExperimentEnabled()).toBe(false);
  });
});
