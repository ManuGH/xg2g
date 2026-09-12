import React, { Suspense, startTransition, useRef, useState } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { useForegroundRecovery } from './useForegroundRecovery';
import { usePlaybackController } from './usePlaybackController';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { buildPlaybackFailure } from './playbackMachine';
import { createRecoveryLadderState } from './recoveryLadder';
import { createDefaultLiveSessionTransport, type LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand, PlaybackRetryTarget } from './playbackTypes';
import type { PlaybackCommandExecutor } from './playbackMachineRuntime';
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

  function createTestController(
    initialStatus: PlayerStatus = 'playing',
    executeCommand: PlaybackCommandExecutor = () => {},
  ) {
    return createPlaybackController({
      executeCommand,
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

  function createDummyTransport(): LiveSessionTransport {
    return createDefaultLiveSessionTransport({
      apiBase: 'http://localhost/api/v3',
      authHeaders: () => ({}),
    });
  }

  function StrictRealControllerPlayer({
    isDocumentVisible,
    onPlayCall,
  }: {
    isDocumentVisible: boolean;
    onPlayCall: () => void;
  }) {
    const transport = createDummyTransport();
    const executeCommand = vi.fn();
    const { controller } = usePlaybackController(
      transport,
      () => ({
        epoch: { playback: 1, session: 0 },
        traceId: 'trace-strict',
        status: 'playing',
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
      executeCommand,
    );

    const videoRef = useRef<HTMLVideoElement | null>(null);
    const hlsRef = useRef(null);
    const paused = useRef(false);
    const [, setStatus] = useState<PlayerStatus>('playing');

    useForegroundRecovery({
      controller,
      videoRef,
      hlsRef,
      isEligible: true,
      isDocumentVisible,
      target: { kind: 'live', serviceRef: 'strict-service' },
      userPauseIntentRef: paused,
      setStatus,
    });

    return (
      <video
        ref={(el) => {
          if (el) {
            el.play = vi.fn().mockImplementation(() => {
              onPlayCall();
              return Promise.resolve();
            });
          }
          videoRef.current = el;
        }}
        data-testid="strict-video"
      />
    );
  }

  function StrictParentPlayer({
    isDocumentVisible,
    onPlayCall,
  }: {
    isDocumentVisible: boolean;
    onPlayCall: () => void;
  }) {
    const transport = createDummyTransport();
    const { controller } = usePlaybackController(
      transport,
      () => ({
        epoch: { playback: 1, session: 0 },
        traceId: 'trace-parent',
        status: 'playing',
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
      vi.fn(),
    );

    return (
      <StrictChildPlayer
        controller={controller}
        isDocumentVisible={isDocumentVisible}
        onPlayCall={onPlayCall}
      />
    );
  }

  function StrictChildPlayer({
    controller,
    isDocumentVisible,
    onPlayCall,
  }: {
    controller: PlaybackController;
    isDocumentVisible: boolean;
    onPlayCall: () => void;
  }) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    const hlsRef = useRef(null);
    const paused = useRef(false);
    const [, setStatus] = useState<PlayerStatus>('playing');

    useForegroundRecovery({
      controller,
      videoRef,
      hlsRef,
      isEligible: true,
      isDocumentVisible,
      target: { kind: 'live', serviceRef: 'parent-child-service' },
      userPauseIntentRef: paused,
      setStatus,
    });

    return (
      <video
        ref={(el) => {
          if (el) {
            el.play = vi.fn().mockImplementation(() => {
              onPlayCall();
              return Promise.resolve();
            });
          }
          videoRef.current = el;
        }}
        data-testid="child-video"
      />
    );
  }

  it('runs safely under Root React.StrictMode using real usePlaybackController lifecycle with exactly one initial play', () => {
    let playCallCount = 0;

    const { rerender, unmount } = render(
      <React.StrictMode>
        <StrictRealControllerPlayer
          isDocumentVisible={false}
          onPlayCall={() => {
            playCallCount++;
          }}
        />
      </React.StrictMode>,
    );

    expect(playCallCount).toBe(0);

    // Reveal inside Root StrictMode
    rerender(
      <React.StrictMode>
        <StrictRealControllerPlayer
          isDocumentVisible={true}
          onPlayCall={() => {
            playCallCount++;
          }}
        />
      </React.StrictMode>,
    );

    // StrictMode double-invocation must NOT duplicate the play call
    expect(playCallCount).toBe(1);

    unmount();
  });

  it('runs safely under Subtree React.StrictMode with parent usePlaybackController and child useForegroundRecovery', () => {
    let playCallCount = 0;

    const { rerender, unmount } = render(
      <div>
        <React.StrictMode>
          <StrictParentPlayer
            isDocumentVisible={false}
            onPlayCall={() => {
              playCallCount++;
            }}
          />
        </React.StrictMode>
      </div>,
    );

    expect(playCallCount).toBe(0);

    // Reveal inside Subtree StrictMode
    rerender(
      <div>
        <React.StrictMode>
          <StrictParentPlayer
            isDocumentVisible={true}
            onPlayCall={() => {
              playCallCount++;
            }}
          />
        </React.StrictMode>
      </div>,
    );

    expect(playCallCount).toBe(1);

    unmount();
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

  it('isolates speculative hook renders during Suspense and correctly routes target and callbacks after commit', async () => {
    const executedCommands: PlaybackCommand[] = [];
    const controller = createTestController('playing', (command) => {
      executedCommands.push(command);
    });

    const committedSetter = vi.fn();
    const speculativeSetter = vi.fn();

    let rejectPlay1!: (err: unknown) => void;
    const pendingPlay1 = new Promise<void>((_resolve, reject) => {
      rejectPlay1 = reject;
    });

    let rejectPlay2!: (err: unknown) => void;
    const pendingPlay2 = new Promise<void>((_resolve, reject) => {
      rejectPlay2 = reject;
    });

    let resolveGate!: () => void;
    let gatePromise: Promise<void> | null = null;
    let shouldSuspend = false;

    function resetGate() {
      gatePromise = new Promise<void>((resolve) => {
        resolveGate = resolve;
      });
    }

    const targetA: PlaybackRetryTarget = { kind: 'live', serviceRef: 'committed-a' };
    const targetB: PlaybackRetryTarget = { kind: 'live', serviceRef: 'speculative-b' };

    let renderedVersion = -1;
    let committedVersion = -1;

    interface BoundaryChildProps {
      version: number;
    }

    function SuspendingBoundaryChild({ version }: BoundaryChildProps) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef(null);
      const paused = useRef(false);

      const target = version === 0 ? targetA : targetB;
      const setStatus = version === 0 ? committedSetter : speculativeSetter;

      // useForegroundRecovery executes BEFORE the suspension gate so its render-phase writes are tested
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

      renderedVersion = version;

      // Explicitly resolvable gate after hook execution
      if (shouldSuspend) {
        throw gatePromise;
      }

      committedVersion = version;

      return (
        <video
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockImplementation(() => {
                return version === 0 ? pendingPlay1 : pendingPlay2;
              });
            }
            videoRef.current = el;
          }}
          data-testid={`video-version-${version}`}
        />
      );
    }

    let triggerTransition: () => void = () => {};

    function SuspenseApp() {
      const [version, setVersion] = useState(0);

      triggerTransition = () => {
        startTransition(() => {
          setVersion(1);
        });
      };

      return (
        <Suspense fallback={<div data-testid="suspense-fallback">Loading...</div>}>
          <SuspendingBoundaryChild version={version} />
        </Suspense>
      );
    }

    const view = render(<SuspenseApp />);

    // 1. Initial committed render (version 0)
    expect(renderedVersion).toBe(0);
    expect(committedVersion).toBe(0);
    expect(view.getByTestId('video-version-0')).toBeDefined();

    // Trigger reveal to start foreground operation on version 0
    act(() => {
      controller.reportForegroundVisibility(false, false);
      controller.reportForegroundVisibility(true, false);
    });

    const activeOpBefore = controller.getActiveForegroundOperationId();
    expect(activeOpBefore).not.toBeNull();

    // 2. Start suspended transition to version 1
    shouldSuspend = true;
    resetGate();
    act(() => {
      triggerTransition();
    });

    // Verify speculative render ran the hook, but did NOT commit
    expect(renderedVersion).toBe(1);
    expect(committedVersion).toBe(0);
    expect(view.getByTestId('video-version-0')).toBeDefined();
    expect(view.queryByTestId('video-version-1')).toBeNull();
    // In transition, fallback is not shown, and active operation on version 0 is still alive
    expect(view.queryByTestId('suspense-fallback')).toBeNull();
    expect(controller.getActiveForegroundOperationId()).toBe(activeOpBefore);

    // 3. Reject the pending play on version 0 while version 1 is suspended
    committedSetter.mockClear();
    speculativeSetter.mockClear();

    await act(async () => {
      rejectPlay1({ name: 'NotAllowedError' });
    });

    // SPECULATIVE ISOLATION: uncommitted render must NOT have hijacked the callback
    expect(speculativeSetter).not.toHaveBeenCalled();
    expect(committedSetter).toHaveBeenCalledWith('paused');

    // 4. Resolve the gate and allow version 1 to commit
    await act(async () => {
      shouldSuspend = false;
      resolveGate();
    });

    // Version 1 is now committed!
    expect(committedVersion).toBe(1);
    expect(view.getByTestId('video-version-1')).toBeDefined();

    // 5. Verify post-commit callback & target routing
    committedSetter.mockClear();
    speculativeSetter.mockClear();

    act(() => {
      controller.reportForegroundVisibility(false, false);
      controller.reportForegroundVisibility(true, false);
    });

    const activeOpAfter = controller.getActiveForegroundOperationId();
    expect(activeOpAfter).not.toBeNull();
    expect(activeOpAfter).not.toBe(activeOpBefore);

    // Reject the second play promise
    await act(async () => {
      rejectPlay2({ name: 'NotAllowedError' });
    });

    // COMMITTED CALLBACK REFRESH: version 1's setter receives the callback
    expect(committedSetter).not.toHaveBeenCalled();
    expect(speculativeSetter).toHaveBeenCalledWith('paused');

    // Verify target routing: transition controller to error and reveal
    controller.dispatch({
      type: 'normative.playback.failure.raised',
      epoch: 1,
      failure: buildPlaybackFailure(
        { title: 'Error', code: 'TEST_ERROR', retryable: true },
        'orchestrator',
        { recoverable: true },
      ),
    });

    executedCommands.length = 0;
    await act(async () => {
      controller.reportForegroundVisibility(false, false);
      controller.reportForegroundVisibility(true, false);
    });

    // Observable target routing: retry receives committed targetB
    const startCmd = executedCommands.find((cmd) => cmd.type === 'command.playback.start');
    expect(startCmd).toBeDefined();
    if (startCmd && startCmd.type === 'command.playback.start') {
      expect(startCmd.serviceRef).toBe('speculative-b');
    }
  });
});
