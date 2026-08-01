import { describe, expect, it } from 'vitest';
import { hlsTuningForLink, resolvePlaybackLinkProfile } from './playbackLinkProfile';

describe('resolvePlaybackLinkProfile', () => {
  it('treats the bandwidth rung as a constrained link', () => {
    expect(resolvePlaybackLinkProfile({ requestProfile: 'bandwidth' })).toBe('constrained');
  });

  it('treats a probe that timed out as a constrained link', () => {
    expect(resolvePlaybackLinkProfile({ probeKind: 'constrained' })).toBe('constrained');
  });

  it.each([
    { metered: true },
    { saveData: true },
    { kind: 'cellular' },
  ])('treats declared mobile conditions as constrained (%o)', (network) => {
    expect(resolvePlaybackLinkProfile({ network })).toBe('constrained');
  });

  it('leaves a LAN or measured wifi link on the stable policy', () => {
    expect(resolvePlaybackLinkProfile({
      requestProfile: 'quality',
      probeKind: 'lan',
      network: { kind: 'lan' },
    })).toBe('stable');
  });
});

describe('hlsTuningForLink', () => {
  // Low-latency mode is deliberately intolerant of buffer gaps, and a 12s sync
  // target leaves nothing in hand when a mobile link dips — which is what a
  // train does constantly. Being further behind live costs a viewer nothing and
  // buys the buffer that carries playback through a dead spot.
  it('stops chasing the live edge on a constrained link and buffers instead', () => {
    const stable = hlsTuningForLink('stable');
    const constrained = hlsTuningForLink('constrained');

    expect(stable.lowLatencyMode).toBe(true);
    expect(constrained.lowLatencyMode).toBe(false);
    expect(constrained.liveSyncDuration).toBeGreaterThan(stable.liveSyncDuration);
    expect(constrained.maxBufferLength).toBeGreaterThan(stable.maxBufferLength);
  });

  it('covers a longer outage while still noticing a returning link quickly', () => {
    const stable = hlsTuningForLink('stable');
    const constrained = hlsTuningForLink('constrained');

    expect(constrained.maxNetworkRetries).toBeGreaterThan(stable.maxNetworkRetries);
    expect(constrained.networkBackoffCapMs).toBeLessThan(stable.networkBackoffCapMs);
  });
});
