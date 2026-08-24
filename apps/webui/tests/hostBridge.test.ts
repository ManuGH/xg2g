import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  applyHostEnvironmentToDocument,
  HOST_MEDIA_KEY_EVENT,
  HOST_NATIVE_PLAYBACK_STATE_EVENT,
  getNativePlaybackCapabilities,
  getNativePlaybackState,
  initializeHostBridge,
  onHostMediaKey,
  onNativePlaybackState,
  requestHostInputFocus,
  resetHostBridgeForTests,
  resolveHostEnvironment,
  setHostPlaybackActive,
  startNativePlayback,
  stopNativePlayback,
} from '../src/lib/hostBridge';

describe('hostBridge', () => {
  afterEach(() => {
    resetHostBridgeForTests();
    delete window.Xg2gHost;
    delete window.Xg2gHostBridge;
    delete document.documentElement.dataset.xg2gHostPlatform;
    delete document.documentElement.dataset.xg2gHostTv;
    delete document.documentElement.dataset.xg2gHostMediaKeys;
    vi.restoreAllMocks();
  });

  it('handshakes over the origin-bound message bridge and caches its snapshot', async () => {
    const postMessage = vi.fn();
    window.Xg2gHostBridge = { postMessage, onmessage: null };

    const initialized = initializeHostBridge();
    expect(JSON.parse(postMessage.mock.calls[0][0])).toEqual({
      protocolVersion: 1,
      type: 'hello',
    });

    window.Xg2gHostBridge.onmessage?.({
      data: JSON.stringify({
        protocolVersion: 1,
        type: 'snapshot',
        host: {
          platform: 'android-tv',
          isTv: true,
          supportsKeepScreenAwake: true,
          supportsHostMediaKeys: true,
          supportsInputFocus: true,
          supportsNativePlayback: true,
        },
        playbackCapabilities: {
          capabilitiesVersion: 3,
          clientIdentity: { platform: 'android_tv', surface: 'native_app' },
          videoCodecs: ['h264'],
        },
        nativePlaybackState: {
          playerState: 3,
          playWhenReady: true,
          isInPip: false,
        },
      }),
    });
    await initialized;

    expect(resolveHostEnvironment().platform).toBe('android-tv');
    expect(getNativePlaybackCapabilities()).toEqual(expect.objectContaining({
      clientIdentity: { platform: 'android_tv', surface: 'native_app' },
      videoCodecs: ['h264'],
    }));
    expect(getNativePlaybackState()?.playerState).toBe(3);
  });

  it('sends structured commands through the message bridge', () => {
    const postMessage = vi.fn();
    window.Xg2gHostBridge = { postMessage, onmessage: null };

    setHostPlaybackActive(true);
    requestHostInputFocus();
    startNativePlayback({ kind: 'live', serviceRef: '1:0:1:AA' });
    stopNativePlayback();

    const messages = postMessage.mock.calls.map(([raw]) => JSON.parse(raw));
    expect(messages).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'hello' }),
      expect.objectContaining({
        type: 'command',
        command: 'setPlaybackActive',
        payload: { active: true },
      }),
      expect.objectContaining({
        type: 'command',
        command: 'startNativePlayback',
        payload: { request: { kind: 'live', serviceRef: '1:0:1:AA' } },
      }),
      expect.objectContaining({ type: 'command', command: 'stopNativePlayback' }),
    ]));
  });

  it('resolves the injected Android host capabilities and applies them to the document', () => {
    window.Xg2gHost = {
      getCapabilitiesJson: () => JSON.stringify({
        platform: 'android-tv',
        isTv: true,
        supportsKeepScreenAwake: true,
        supportsHostMediaKeys: true,
        supportsInputFocus: true,
        supportsNativePlayback: true,
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

  it('proxies native playback calls and reads native playback state', () => {
    const startNativePlaybackSpy = vi.fn();
    const stopNativePlaybackSpy = vi.fn();
    window.Xg2gHost = {
      startNativePlayback: startNativePlaybackSpy,
      stopNativePlayback: stopNativePlaybackSpy,
      getPlaybackCapabilitiesJson: () => JSON.stringify({
        capabilitiesVersion: 3,
        container: ['hls', 'ts'],
        videoCodecs: ['h264'],
        audioCodecs: ['aac', 'ac3'],
        supportsHls: true,
        supportsRange: true,
        deviceType: 'android_tv',
        hlsEngines: ['native'],
        preferredHlsEngine: 'native',
        runtimeProbeUsed: false,
        runtimeProbeVersion: 1,
        allowTranscode: true,
      }),
      getNativePlaybackStateJson: () => JSON.stringify({
        activeRequest: { kind: 'live', serviceRef: '1:0:1:AA' },
        session: { sessionId: 'sess-1', state: 'READY' },
        playerState: 3,
        playWhenReady: true,
        isInPip: false,
        lastError: null,
      }),
    };

    const started = startNativePlayback({
      kind: 'recording',
      recordingId: 'rec-1',
      startPositionMs: 42,
      title: 'Recording',
    });
    stopNativePlayback();
    const nativeCapabilities = getNativePlaybackCapabilities();
    const state = getNativePlaybackState();

    expect(started).toBe(true);
    expect(startNativePlaybackSpy).toHaveBeenCalledWith(JSON.stringify({
      kind: 'recording',
      recordingId: 'rec-1',
      startPositionMs: 42,
      title: 'Recording',
    }));
    expect(stopNativePlaybackSpy).toHaveBeenCalledTimes(1);
    expect(nativeCapabilities).toEqual(expect.objectContaining({
      deviceType: 'android_tv',
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'ac3'],
    }));
    expect(state?.activeRequest?.kind).toBe('live');
    expect(state?.session?.sessionId).toBe('sess-1');
    expect(state?.playerState).toBe(3);
  });

  it('subscribes to native playback state events', () => {
    const handler = vi.fn();
    const unsubscribe = onNativePlaybackState(handler);

    window.dispatchEvent(new CustomEvent(HOST_NATIVE_PLAYBACK_STATE_EVENT, {
      detail: {
        activeRequest: { kind: 'live', serviceRef: '1:0:1:AA' },
        session: { sessionId: 'sess-1', state: 'READY' },
        playerState: 2,
        playWhenReady: true,
        isInPip: false,
        lastError: null,
      },
    }));

    expect(handler).toHaveBeenCalledWith(expect.objectContaining({
      playerState: 2,
      playWhenReady: true,
    }));
    unsubscribe();
  });
});
