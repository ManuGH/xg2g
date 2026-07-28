import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useRef } from 'react';
import type { MutableRefObject } from 'react';
import type {
  NativePlaybackRequest,
  NativePlaybackState as HostNativePlaybackState,
} from '../../../lib/hostBridge';
import { useNativePlaybackBridge, type NativePlaybackPipeline } from './useNativePlaybackBridge';

const startNativePlaybackMock = vi.fn<(request: NativePlaybackRequest) => boolean>();
const onNativePlaybackStateMock =
  vi.fn<(handler: (state: HostNativePlaybackState) => void) => () => void>();
const getNativePlaybackStateMock = vi.fn<() => HostNativePlaybackState | null>();

vi.mock('../../../lib/hostBridge', async () => {
  const actual = await vi.importActual<typeof import('../../../lib/hostBridge')>(
    '../../../lib/hostBridge',
  );
  return {
    ...actual,
    startNativePlayback: (request: NativePlaybackRequest) => startNativePlaybackMock(request),
    onNativePlaybackState: (handler: (state: HostNativePlaybackState) => void) =>
      onNativePlaybackStateMock(handler),
    getNativePlaybackState: () => getNativePlaybackStateMock(),
  };
});

function buildPipeline(): NativePlaybackPipeline {
  return {
    setActiveHlsEngine: vi.fn(),
    setActiveRecordingId: vi.fn(),
    setPlaybackMode: vi.fn(),
    setStatus: vi.fn(),
    setTraceId: vi.fn(),
    setSessionProfileReason: vi.fn(),
    setPlaybackObservability: vi.fn(),
    mergeSessionPlaybackTrace: vi.fn(),
    clearPlayerError: vi.fn(),
    reportPlaybackFailure: vi.fn(),
  };
}

function renderBridge(opts: { isNativePlaybackHost: boolean }) {
  const pipeline = buildPipeline();
  const resolvePreferredHlsEngine = () => 'hlsjs' as const;
  return renderHook(({ isNativePlaybackHost }) => {
    const activeRecordingRef = useRef<string | null>(null) as MutableRefObject<string | null>;
    return useNativePlaybackBridge({
      isNativePlaybackHost,
      resolvePreferredHlsEngine,
      pipeline,
      activeRecordingRef,
    });
  }, { initialProps: opts });
}

describe('useNativePlaybackBridge', () => {
  beforeEach(() => {
    startNativePlaybackMock.mockReset();
    onNativePlaybackStateMock.mockReset();
    getNativePlaybackStateMock.mockReset();
    getNativePlaybackStateMock.mockReturnValue(null);
    onNativePlaybackStateMock.mockReturnValue(() => undefined);
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('subscribes to host state when native playback host is enabled', () => {
    renderBridge({ isNativePlaybackHost: true });
    expect(onNativePlaybackStateMock).toHaveBeenCalledTimes(1);
    expect(getNativePlaybackStateMock).toHaveBeenCalledTimes(1);
  });

  it('does not subscribe when native playback host is disabled', () => {
    renderBridge({ isNativePlaybackHost: false });
    expect(onNativePlaybackStateMock).not.toHaveBeenCalled();
  });

  it('invokes the unsubscribe callback on unmount', () => {
    const unsubscribe = vi.fn();
    onNativePlaybackStateMock.mockReturnValue(unsubscribe);
    const view = renderBridge({ isNativePlaybackHost: true });
    expect(unsubscribe).not.toHaveBeenCalled();
    view.unmount();
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });

  it('beginNativePlayback delegates to startNativePlayback', () => {
    startNativePlaybackMock.mockReturnValue(true);
    const view = renderBridge({ isNativePlaybackHost: true });
    const request: NativePlaybackRequest = {
      kind: 'live',
      serviceRef: 'svc-1',
      title: 'Channel',
    };
    act(() => view.result.current.beginNativePlayback(request));
    expect(startNativePlaybackMock).toHaveBeenCalledWith(request);
  });

  it('beginNativePlayback throws when bridge is unavailable', () => {
    startNativePlaybackMock.mockReturnValue(false);
    const view = renderBridge({ isNativePlaybackHost: true });
    expect(() =>
      act(() =>
        view.result.current.beginNativePlayback({
          kind: 'live',
          serviceRef: 'svc-1',
        }),
      ),
    ).toThrow(/bridge unavailable/);
  });

  it('resetBridgeState clears nativePlaybackState and nativeSessionId', () => {
    startNativePlaybackMock.mockReturnValue(true);
    const view = renderBridge({ isNativePlaybackHost: true });

    act(() =>
      view.result.current.beginNativePlayback({
        kind: 'live',
        serviceRef: 'svc-1',
      }),
    );
    expect(view.result.current.nativePlaybackState).not.toBeNull();

    act(() => view.result.current.resetBridgeState());
    expect(view.result.current.nativePlaybackState).toBeNull();
    expect(view.result.current.nativeSessionId).toBeNull();
  });
});
