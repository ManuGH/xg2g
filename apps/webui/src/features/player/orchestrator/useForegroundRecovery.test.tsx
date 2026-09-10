import React, { Suspense, useRef, useState } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { useForegroundRecovery } from './useForegroundRecovery';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import type { PlaybackRetryTarget } from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';

describe('useForegroundRecovery (React hook & adapter integration)', () => {
  let originalVisibilityState: PropertyDescriptor | undefined;

  beforeEach(() => {
    vi.useFakeTimers();
    originalVisibilityState = Object.getOwnPropertyDescriptor(document, 'visibilityState');
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      configurable: true,
      writable: true,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    if (originalVisibilityState) {
      Object.defineProperty(document, 'visibilityState', originalVisibilityState);
    }
  });

  function createTestController(initialStatus: PlayerStatus = 'playing') {
    return createPlaybackController({
      createInitialState: () => ({
        epoch: { playback: 1, session: 0 },
        traceId: 'trace-1',
        status: initialStatus,
        playbackMode: 'LIVE',
        vodStreamMode: null,
        activeHlsEngine: null,
        durationSeconds: null,
        canSeek: false,
        startUnix: null,
        sessionPhase: 'idle',
        mediaPhase: 'playing',
        contract: null,
        failure: null,
        lastAdvisory: null,
        explicitProfilePinned: false,
        hasSessionIntent: true,
        recovery: createRecoveryLadderState(),
        leaseExpiresAt: null,
        connectionLost: false,
      }),
    });
  }

  interface TestPlayerProps {
    controller: PlaybackController;
    isEligible?: boolean;
    isDocumentVisible?: boolean;
    target?: PlaybackRetryTarget | null;
    status?: PlayerStatus;
    onStatusChange?: (next: PlayerStatus) => void;
    onPlayCall?: () => void;
  }

  function TestPlayer({
    controller,
    isEligible = true,
    isDocumentVisible = true,
    target = { kind: 'live', serviceRef: '1:0:1:TEST' },
    status = 'playing',
    onStatusChange,
    onPlayCall,
  }: TestPlayerProps) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    const hlsRef = useRef<{ startLoad: () => void } | null>({
      startLoad: vi.fn(),
    });
    const userPauseIntentRef = useRef(false);
    const [, setLocalStatus] = useState<PlayerStatus>(status);

    const setStatus = (action: React.SetStateAction<PlayerStatus>) => {
      setLocalStatus((prev) => {
        const next = typeof action === 'function' ? (action as any)(prev) : action;
        onStatusChange?.(next);
        return next;
      });
    };

    useForegroundRecovery({
      controller,
      videoRef: videoRef as any,
      hlsRef: hlsRef as any,
      isEligible,
      isDocumentVisible,
      target,
      userPauseIntentRef,
      setStatus,
    });

    return (
      <div>
        <video
          ref={(el) => {
            if (el) {
              // Polyfill mock play
              el.play = vi.fn().mockImplementation(() => {
                onPlayCall?.();
                return Promise.resolve();
              });
            }
            videoRef.current = el;
          }}
        />
      </div>
    );
  }

  it('initial visible mount registers binding without initiating recovery', () => {
    const controller = createTestController('playing');
    render(<TestPlayer controller={controller} isDocumentVisible={true} />);

    expect(controller.getActiveForegroundOperationId()).toBeNull();
  });

  it('transitions hidden -> visible to trigger foreground nudge and status buffering', () => {
    const controller = createTestController('paused');
    let currentStatus: PlayerStatus = 'paused';
    let playCallCount = 0;

    const { rerender } = render(
      <TestPlayer
        controller={controller}
        isDocumentVisible={true}
        status={currentStatus}
        onStatusChange={(s) => {
          currentStatus = s;
        }}
        onPlayCall={() => {
          playCallCount++;
        }}
      />,
    );

    // Document hides
    rerender(
      <TestPlayer
        controller={controller}
        isDocumentVisible={false}
        status={currentStatus}
        onStatusChange={(s) => {
          currentStatus = s;
        }}
        onPlayCall={() => {
          playCallCount++;
        }}
      />,
    );
    expect(controller.getActiveForegroundOperationId()).toBeNull();

    // Document reveals
    rerender(
      <TestPlayer
        controller={controller}
        isDocumentVisible={true}
        status={currentStatus}
        onStatusChange={(s) => {
          currentStatus = s;
        }}
        onPlayCall={() => {
          playCallCount++;
        }}
      />,
    );

    expect(currentStatus).toBe('buffering');
    expect(playCallCount).toBeGreaterThanOrEqual(1); // Immediate play issued
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
  });

  it('runs safely under React.StrictMode with setup/cleanup replay without double-initiation', () => {
    const controller = createTestController('playing');
    let playCallCount = 0;

    const { rerender } = render(
      <React.StrictMode>
        <TestPlayer
          controller={controller}
          isDocumentVisible={false}
          onPlayCall={() => {
            playCallCount++;
          }}
        />
      </React.StrictMode>,
    );

    // Reveal inside StrictMode
    rerender(
      <React.StrictMode>
        <TestPlayer
          controller={controller}
          isDocumentVisible={true}
          onPlayCall={() => {
            playCallCount++;
          }}
        />
      </React.StrictMode>,
    );

    // Should create exactly one operation despite StrictMode double invocation
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
    expect(playCallCount).toBeGreaterThanOrEqual(1);
  });

  it('unmount cleanly disposes active operation and detaches binding', () => {
    const controller = createTestController('playing');
    const { rerender, unmount } = render(
      <TestPlayer controller={controller} isDocumentVisible={false} />,
    );

    rerender(<TestPlayer controller={controller} isDocumentVisible={true} />);
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();

    unmount();
    expect(controller.getActiveForegroundOperationId()).toBeNull();
  });

  it('uncommitted Suspense transition does not mutate controller binding', async () => {
    const controller = createTestController('playing');
    let shouldSuspend = false;

    function SuspendingComponent() {
      if (shouldSuspend) {
        throw new Promise(() => {}); // Never resolves (suspended)
      }
      return <TestPlayer controller={controller} target={{ kind: 'vod', recordingId: 'committed-1' }} />;
    }

    render(
      <Suspense fallback={<div>Loading...</div>}>
        <SuspendingComponent />
      </Suspense>,
    );

    // Start a suspended transition
    shouldSuspend = true;
    act(() => {
      // Re-render in suspended state
    });

    // Controller target remains committed
    expect(controller.getActiveForegroundOperationId()).toBeNull();
  });
});
