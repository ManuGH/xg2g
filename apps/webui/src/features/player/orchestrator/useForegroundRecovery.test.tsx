import React, { Suspense, useRef, useState, useTransition } from 'react';
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
    expect(playCallCount).toBe(1); // Exactly 1 immediate play issued
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
  });

  it('attaches a video mounted after render with stable production-style callbacks', () => {
    const controller = createTestController('playing');
    let playCount = 0;

    function StablePlayer({ visible }: { visible: boolean }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef(null);
      const paused = useRef(false);
      const [, stableSetter] = useState<PlayerStatus>('playing');

      useForegroundRecovery({
        controller,
        videoRef,
        hlsRef,
        isEligible: true,
        isDocumentVisible: visible,
        target: { kind: 'live', serviceRef: 'channel-a' },
        userPauseIntentRef: paused,
        setStatus: stableSetter, // completely stable setter across renders
      });

      return (
        <video
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockImplementation(() => {
                playCount++;
                return Promise.resolve();
              });
            }
            videoRef.current = el;
          }}
        />
      );
    }

    const view = render(<StablePlayer visible={false} />);
    view.rerender(<StablePlayer visible={true} />);

    expect(playCount).toBe(1);
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
  });

  it('runs safely under React.StrictMode with setup/cleanup replay with exactly one initial play', () => {
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

    // StrictMode double-invocation must NOT duplicate the play call or operation
    expect(playCallCount).toBe(1);
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();
  });

  it('cancels the old nudge when React replaces the actual video DOM element', () => {
    const controller = createTestController('playing');
    let playACount = 0;
    let playBCount = 0;

    function SwappablePlayer({ visible, elementKey }: { visible: boolean; elementKey: string }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef(null);
      const paused = useRef(false);
      const [, setStatus] = useState<PlayerStatus>('playing');

      useForegroundRecovery({
        controller,
        videoRef,
        hlsRef,
        isEligible: true,
        isDocumentVisible: visible,
        target: { kind: 'live', serviceRef: 'channel-a' },
        userPauseIntentRef: paused,
        setStatus,
      });

      return (
        <video
          key={elementKey}
          data-testid={`video-${elementKey}`}
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockImplementation(() => {
                if (elementKey === 'a') playACount++;
                if (elementKey === 'b') playBCount++;
                return Promise.resolve();
              });
            }
            videoRef.current = el;
          }}
        />
      );
    }

    const view = render(<SwappablePlayer visible={false} elementKey="a" />);
    view.rerender(<SwappablePlayer visible={true} elementKey="a" />);

    expect(playACount).toBe(1);
    expect(controller.getActiveForegroundOperationId()).not.toBeNull();

    // Now swap element key to 'b'
    view.rerender(<SwappablePlayer visible={true} elementKey="b" />);

    // Old video's active operation should be cancelled
    expect(controller.getActiveForegroundOperationId()).toBeNull();

    // Advance timers by 400ms observation window
    act(() => {
      vi.advanceTimersByTime(400);
    });

    // Old video A must NOT have received any further plays!
    expect(playACount).toBe(1);
    expect(playBCount).toBe(0);
  });

  describe('Parent and child attachment ordering', () => {
    it('Parent renders video, Child runs useForegroundRecovery hook', () => {
      const controller = createTestController('playing');
      let playCount = 0;

      function ChildHook({
        videoRef,
        visible,
      }: {
        videoRef: React.RefObject<HTMLVideoElement | null>;
        visible: boolean;
      }) {
        const hlsRef = useRef(null);
        const paused = useRef(false);
        const [, setStatus] = useState<PlayerStatus>('playing');
        useForegroundRecovery({
          controller,
          videoRef,
          hlsRef,
          isEligible: true,
          isDocumentVisible: visible,
          target: { kind: 'live', serviceRef: 'order-test-1' },
          userPauseIntentRef: paused,
          setStatus,
        });
        return null;
      }

      function Parent({ visible }: { visible: boolean }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        return (
          <div>
            <video
              ref={(el) => {
                if (el) {
                  el.play = vi.fn().mockImplementation(() => {
                    playCount++;
                    return Promise.resolve();
                  });
                }
                videoRef.current = el;
              }}
            />
            <ChildHook videoRef={videoRef} visible={visible} />
          </div>
        );
      }

      const view = render(<Parent visible={false} />);
      view.rerender(<Parent visible={true} />);

      expect(playCount).toBe(1);
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
    });

    it('Parent runs useForegroundRecovery hook, Child renders video', () => {
      const controller = createTestController('playing');
      let playCount = 0;

      function ChildVideo({ videoRef }: { videoRef: React.RefObject<HTMLVideoElement | null> }) {
        return (
          <video
            ref={(el) => {
              if (el) {
                el.play = vi.fn().mockImplementation(() => {
                  playCount++;
                  return Promise.resolve();
                });
              }
              (videoRef as React.MutableRefObject<HTMLVideoElement | null>).current = el;
            }}
          />
        );
      }

      function Parent({ visible }: { visible: boolean }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef(null);
        const paused = useRef(false);
        const [, setStatus] = useState<PlayerStatus>('playing');
        useForegroundRecovery({
          controller,
          videoRef,
          hlsRef,
          isEligible: true,
          isDocumentVisible: visible,
          target: { kind: 'live', serviceRef: 'order-test-2' },
          userPauseIntentRef: paused,
          setStatus,
        });
        return <ChildVideo videoRef={videoRef} />;
      }

      const view = render(<Parent visible={false} />);
      view.rerender(<Parent visible={true} />);

      expect(playCount).toBe(1);
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
    });
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

  it('real state-driven suspended transition does not mutate controller target or binding', async () => {
    const controller = createTestController('playing');
    let resolveSuspense: (() => void) | null = null;
    let suspensePromise: Promise<void> | null = null;

    interface SuspendingPlayerProps {
      target: PlaybackRetryTarget;
      shouldSuspend: boolean;
    }

    function SuspendingPlayer({ target, shouldSuspend }: SuspendingPlayerProps) {
      if (shouldSuspend) {
        if (!suspensePromise) {
          suspensePromise = new Promise((resolve) => {
            resolveSuspense = () => {
              suspensePromise = null;
              resolve();
            };
          });
        }
        throw suspensePromise;
      }

      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef(null);
      const paused = useRef(false);
      const [, setStatus] = useState<PlayerStatus>('playing');

      useForegroundRecovery({
        controller,
        videoRef,
        hlsRef,
        isEligible: true,
        isDocumentVisible: true,
        target,
        userPauseIntentRef: paused,
        setStatus,
      });

      return (
        <video
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockResolvedValue(undefined);
            }
            videoRef.current = el;
          }}
        />
      );
    }

    let triggerSuspendedTransition: () => void = () => {};

    function SuspenseApp() {
      const [target, setTarget] = useState<PlaybackRetryTarget>(committedTarget);
      const [shouldSuspend, setShouldSuspend] = useState(false);
      const [isPending, startTransition] = useTransition();

      triggerSuspendedTransition = () => {
        startTransition(() => {
          setTarget(speculativeTarget);
          setShouldSuspend(true);
        });
      };

      return (
        <Suspense fallback={<div data-testid="fallback">Suspended Loading...</div>}>
          <SuspendingPlayer target={target} shouldSuspend={shouldSuspend} />
          {isPending && <div data-testid="pending">Transition Pending...</div>}
        </Suspense>
      );
    }

    const committedTarget: PlaybackRetryTarget = { kind: 'live', serviceRef: 'committed-1' };
    const speculativeTarget: PlaybackRetryTarget = { kind: 'live', serviceRef: 'speculative-2' };

    const view = render(<SuspenseApp />);

    // Initial committed render:
    // Report hidden then reveal to start an active operation on committedTarget
    controller.reportForegroundVisibility(false, false);
    controller.reportForegroundVisibility(true, false);
    const activeOpBefore = controller.getActiveForegroundOperationId();
    expect(activeOpBefore).not.toBeNull();

    // Now start a concurrent transition to the speculative target that suspends
    act(() => {
      triggerSuspendedTransition();
    });

    // Transition is pending, suspended offscreen
    expect(view.getByTestId('pending')).toBeDefined();

    // CRITICAL ASSERTION: The uncommitted / suspended render must NOT mutate controller target!
    // The active operation must still be alive for committedTarget
    expect(controller.getActiveForegroundOperationId()).toBe(activeOpBefore);

    // Now resolve the suspense promise to allow commit
    await act(async () => {
      resolveSuspense?.();
      await vi.runAllTimersAsync();
    });

    // Once resolved, the speculativeTarget commits, replacing the target and cancelling older op
    expect(controller.getActiveForegroundOperationId()).toBeNull();
  });
});
