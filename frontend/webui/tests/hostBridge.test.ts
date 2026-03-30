import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  applyHostEnvironmentToDocument,
  HOST_MEDIA_KEY_EVENT,
  onHostMediaKey,
  resolveHostEnvironment,
  setHostPlaybackActive,
  requestHostInputFocus,
} from '../src/lib/hostBridge';

describe('hostBridge', () => {
  afterEach(() => {
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;
    delete document.documentElement.dataset.xg2gHostPlatform;
    delete document.documentElement.dataset.xg2gHostTv;
    delete document.documentElement.dataset.xg2gHostMediaKeys;
    vi.restoreAllMocks();
  });

  it('resolves the injected Android host capabilities and applies them to the document', () => {
    window.Xg2gHost = {
      getCapabilitiesJson: () => JSON.stringify({
        platform: 'android-tv',
        isTv: true,
        supportsKeepScreenAwake: true,
        supportsHostMediaKeys: true,
        supportsInputFocus: true,
      }),
    };

    const environment = resolveHostEnvironment();
    applyHostEnvironmentToDocument(environment);

    expect(environment.platform).toBe('android-tv');
    expect(environment.isTv).toBe(true);
    expect(document.documentElement.dataset.xg2gHostPlatform).toBe('android-tv');
    expect(document.documentElement.dataset.xg2gHostTv).toBe('true');
    expect(document.documentElement.dataset.xg2gHostMediaKeys).toBe('true');
  });

  it('proxies playback activity and focus requests to the Android bridge', () => {
    const setPlaybackActiveSpy = vi.fn();
    const requestInputFocusSpy = vi.fn();
    window.Xg2gHost = {
      setPlaybackActive: setPlaybackActiveSpy,
      requestInputFocus: requestInputFocusSpy,
    };

    setHostPlaybackActive(true);
    requestHostInputFocus();

    expect(setPlaybackActiveSpy).toHaveBeenCalledWith(true);
    expect(requestInputFocusSpy).toHaveBeenCalledTimes(1);
  });

  it('subscribes to host media-key events', () => {
    const handler = vi.fn();
    const unsubscribe = onHostMediaKey(handler);

    window.dispatchEvent(new CustomEvent(HOST_MEDIA_KEY_EVENT, {
      detail: { action: 'seekForward', ts: Date.now() },
    }));

    expect(handler).toHaveBeenCalledWith('seekForward');
    unsubscribe();
  });
});
