// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import React, { Suspense, startTransition, useLayoutEffect, useRef, useState } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import type Hls from 'hls.js';
import { useForegroundRecovery } from './useForegroundRecovery';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { usePlaybackController } from './usePlaybackController';
import { createInitialPlaybackDomainState } from './playbackMachine';
import { createRecoveryLadderState } from './recoveryLadder';
import type { PlaybackRetryTarget } from './playbackTypes';
import type { HlsInstanceRef, PlayerStatus, V3PlayerProps, VideoElementRef } from '../../../types/v3-player';
import { usePlaybackOrchestrator } from '../usePlaybackOrchestrator';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    this.destroy = vi.fn();
    this.startLoad = vi.fn();
    this.attachMedia = vi.fn();
    this.loadSource = vi.fn();
    this.on = vi.fn();
    this.off = vi.fn();
  });
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    MANIFEST_PARSED: 'hlsManifestParsed',
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    FRAG_BUFFERED: 'hlsFragBuffered',
    ERROR: 'hlsError',
  };
  return { default: HlsMock };
});

describe('Step 3c: React Lifecycle, Suspense Isolation, and Facade Events (Rows 10-12)', () => {
  const controllers: PlaybackController[] = [];
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
    cleanup();
    controllers.splice(0).forEach((c) => c.dispose());
    vi.useRealTimers();
    if (originalVisibilityState) {
      Object.defineProperty(document, 'visibilityState', originalVisibilityState);
    }
    vi.restoreAllMocks();
  });

  function createTestController(initialStatus: PlayerStatus = 'playing', connectionLost = false) {
    const controller = createPlaybackController({
      createInitialState: () => ({
        epoch: { playback: 1, session: 0 },
        traceId: 'trace-test',
        status: initialStatus,
        playbackMode: 'LIVE',
        vodStreamMode: null,
        activeHlsEngine: null,
        durationSeconds: null,
        canSeek: false,
        startUnix: null,
        sessionPhase: 'ready',
        mediaPhase: 'playing',
        contract: null,
        failure: null,
        lastAdvisory: null,
        explicitProfilePinned: false,
        hasSessionIntent: true,
        recovery: createRecoveryLadderState(),
        leaseExpiresAt: null,
        connectionLost,
      }),
    });
    controllers.push(controller);
    return controller;
  }

  interface HarnessProps {
    controller: PlaybackController;
    isEligible?: boolean;
    isDocumentVisible?: boolean;
    isOnline?: boolean;
    hasActiveSession?: boolean;
    target?: PlaybackRetryTarget | null;
    status?: PlayerStatus;
    onStatusChange?: (next: PlayerStatus) => void;
    onPlayCall?: () => void;
    videoKey?: string;
  }

  function ResumeHarness({
    controller,
    isEligible = true,
    isDocumentVisible = true,
    isOnline = true,
    hasActiveSession = true,
    target = { kind: 'live', serviceRef: '1:0:1:TEST' },
    status = 'playing',
    onStatusChange,
    onPlayCall,
    videoKey = 'video-default',
  }: HarnessProps) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    const hlsRef = useRef<Hls | null>({
      startLoad: vi.fn(),
      destroy: vi.fn(),
    } as unknown as Hls);
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
      hlsRef,
      isEligible,
      isDocumentVisible,
      isOnline,
      hasActiveSession,
      target,
      userPauseIntentRef,
      setStatus,
    });

    return (
      <div>
        <video
          key={videoKey}
          ref={(el) => {
            if (el) {
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

  // ---------------------------------------------------------------------------
  // Row 10: Mount, ordinary rerender, video replacement, unmount, StrictMode
  // ---------------------------------------------------------------------------
  describe('Row 10: React commit phase and DOM lifecycle', () => {
    it('ordinary rerender preserves active recovery operation and exact play count', () => {
      const controller = createTestController('playing');
      let playCalls = 0;

      const { rerender } = render(
        <ResumeHarness
          controller={controller}
          isOnline={false}
          hasActiveSession={true}
          onPlayCall={() => playCalls++}
        />,
      );

      // Reconnect edge -> operation starts, 1 immediate play
      rerender(
        <ResumeHarness
          controller={controller}
          isOnline={true}
          hasActiveSession={true}
          onPlayCall={() => playCalls++}
        />,
      );

      const opId = controller.getActiveForegroundOperationId();
      expect(opId).not.toBeNull();
      expect(playCalls).toBe(1);

      // Ordinary rerender with same video element and same target
      rerender(
        <ResumeHarness
          controller={controller}
          isOnline={true}
          hasActiveSession={true}
          onPlayCall={() => playCalls++}
        />,
      );

      // Operation identity is PRESERVED
      expect(controller.getActiveForegroundOperationId()).toBe(opId);
      expect(playCalls).toBe(1);

      // 400ms passes -> nudge 1 fires as scheduled
      vi.advanceTimersByTime(400);
      expect(playCalls).toBe(2);
    });

    it('video DOM replacement cancels active recovery on the old element', () => {
      const controller = createTestController('playing');
      let playCalls = 0;

      const { rerender } = render(
        <ResumeHarness
          controller={controller}
          isOnline={false}
          hasActiveSession={true}
          videoKey="vid-key-1"
          onPlayCall={() => playCalls++}
        />,
      );

      // Start recovery
      rerender(
        <ResumeHarness
          controller={controller}
          isOnline={true}
          hasActiveSession={true}
          videoKey="vid-key-1"
          onPlayCall={() => playCalls++}
        />,
      );
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
      expect(playCalls).toBe(1);

      // Video element replaced via new key
      rerender(
        <ResumeHarness
          controller={controller}
          isOnline={true}
          hasActiveSession={true}
          videoKey="vid-key-2"
          onPlayCall={() => playCalls++}
        />,
      );

      // Operation cancelled on old element!
      expect(controller.getActiveForegroundOperationId()).toBeNull();

      // Advancing timer produces NO further plays
      vi.advanceTimersByTime(2000);
      expect(playCalls).toBe(1);
    });

    it('unmounting cancels active recovery immediately', () => {
      const controller = createTestController('playing');
      let playCalls = 0;

      const { rerender, unmount } = render(
        <ResumeHarness
          controller={controller}
          isOnline={false}
          hasActiveSession={true}
          onPlayCall={() => playCalls++}
        />,
      );

      rerender(
        <ResumeHarness
          controller={controller}
          isOnline={true}
          hasActiveSession={true}
          onPlayCall={() => playCalls++}
        />,
      );
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
      expect(playCalls).toBe(1);

      // Unmount
      unmount();
      expect(controller.getActiveForegroundOperationId()).toBeNull();

      vi.advanceTimersByTime(2000);
      expect(playCalls).toBe(1);
    });

    it('operates reliably with usePlaybackController under root StrictMode across mount, unmount, and reconnect', () => {
      let playCalls = 0;
      let controllerInstance: PlaybackController | null = null;
      const dummyTransport: any = {
        startLiveSession: vi.fn(),
        stopSession: vi.fn(),
        sendHeartbeat: vi.fn(),
        getSession: vi.fn(),
      };

      function StrictModePlayer({ isOnline }: { isOnline: boolean }) {
        const { controller } = usePlaybackController(
          dummyTransport,
          () => ({
            ...createInitialPlaybackDomainState(),
            status: 'playing',
            playbackMode: 'LIVE',
            hasSessionIntent: true,
          }),
          () => {},
        );
        controllerInstance = controller;
        controllers.push(controller);
        return (
          <ResumeHarness
            controller={controller}
            isOnline={isOnline}
            hasActiveSession={true}
            onPlayCall={() => playCalls++}
          />
        );
      }

      const { rerender, unmount } = render(
        <React.StrictMode>
          <StrictModePlayer isOnline={false} />
        </React.StrictMode>,
      );

      // In StrictMode mount, controller was activated
      expect(controllerInstance!.isDisposed()).toBe(false);
      expect(playCalls).toBe(0);

      // Transition to online
      rerender(
        <React.StrictMode>
          <StrictModePlayer isOnline={true} />
        </React.StrictMode>,
      );

      // Exactly 1 operation started and 1 immediate play
      expect(controllerInstance!.getActiveForegroundOperationId()).not.toBeNull();
      expect(playCalls).toBe(1);

      act(() => {
        vi.advanceTimersByTime(400);
      });
      expect(playCalls).toBe(2);

      // Unmount under StrictMode cancels active recovery and disposes controller cleanly
      unmount();
      expect(controllerInstance!.getActiveForegroundOperationId()).toBeNull();
      expect(controllerInstance!.isDisposed()).toBe(true);
    });

    it('operates reliably with usePlaybackController under subtree StrictMode', () => {
      let playCalls = 0;
      let controllerInstance: PlaybackController | null = null;
      const dummyTransport: any = {
        startLiveSession: vi.fn(),
        stopSession: vi.fn(),
        sendHeartbeat: vi.fn(),
        getSession: vi.fn(),
      };

      function SubtreePlayer({ isOnline }: { isOnline: boolean }) {
        const { controller } = usePlaybackController(
          dummyTransport,
          () => ({
            ...createInitialPlaybackDomainState(),
            status: 'playing',
            playbackMode: 'LIVE',
            hasSessionIntent: true,
          }),
          () => {},
        );
        controllerInstance = controller;
        controllers.push(controller);
        return (
          <ResumeHarness
            controller={controller}
            isOnline={isOnline}
            hasActiveSession={true}
            onPlayCall={() => playCalls++}
          />
        );
      }

      const { rerender, unmount } = render(
        <div>
          <React.StrictMode>
            <SubtreePlayer isOnline={false} />
          </React.StrictMode>
        </div>,
      );

      expect(controllerInstance!.isDisposed()).toBe(false);
      expect(playCalls).toBe(0);

      rerender(
        <div>
          <React.StrictMode>
            <SubtreePlayer isOnline={true} />
          </React.StrictMode>
        </div>,
      );

      expect(controllerInstance!.getActiveForegroundOperationId()).not.toBeNull();
      expect(playCalls).toBe(1);

      unmount();
      expect(controllerInstance!.isDisposed()).toBe(true);
      expect(controllerInstance!.getActiveForegroundOperationId()).toBeNull();
    });

    it('evaluates coherent committed context when child layout effect dispatches connectionLost change', () => {
      let playCalls = 0;
      const controller = createTestController('playing', true);

      function Child({ changed }: { changed: boolean }) {
        useLayoutEffect(() => {
          if (changed) {
            controller.dispatch({
              type: 'normative.session.lease.updated',
              epoch: controller.getEpoch(),
              sessionEpoch: 0,
              leaseExpiresAt: null,
              connectionLost: false,
            });
          }
        }, [changed]);
        return null;
      }

      function Parent({ isOnline, changed }: { isOnline: boolean; changed: boolean }) {
        return (
          <div>
            <ResumeHarness
              controller={controller}
              isOnline={isOnline}
              hasActiveSession={true}
              onPlayCall={() => playCalls++}
            />
            <Child changed={changed} />
          </div>
        );
      }

      const { rerender } = render(<Parent isOnline={true} changed={false} />);
      expect(playCalls).toBe(0);

      // Now rerender with isOnline=false and changed=true:
      // Child dispatches connectionLost=false in layout effect.
      // Coherent committed context ensures isOnline=false is known; zero plays issued!
      rerender(<Parent isOnline={false} changed={true} />);
      expect(controller.getState().connectionLost).toBe(false);
      expect(playCalls).toBe(0);
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // Row 11: Real post-hook suspended transition and successful reveal
  // ---------------------------------------------------------------------------
  describe('Row 11: Suspended transition isolation', () => {
    it('preserves committed target and callback during suspension; commits replacement only after reveal', async () => {
      const controller = createTestController('playing');
      const committedStatusChange = vi.fn();
      const speculativeStatusChange = vi.fn();

      let rejectPlay!: (error: unknown) => void;
      const pendingPlay = new Promise<void>((_, reject) => {
        rejectPlay = reject;
      });
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockReturnValue(pendingPlay);

      let resolveSuspension!: () => void;
      let suspensionPromise: Promise<void> | null = null;
      let triggerTransition!: () => void;

      function SuspendedChild({ version }: { version: number }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>(null);
        const pausedRef = useRef(false);

        useForegroundRecovery({
          controller,
          videoRef: videoRef as any,
          hlsRef,
          isEligible: true,
          isDocumentVisible: true,
          isOnline: true,
          hasActiveSession: true,
          target: { kind: 'live', serviceRef: version ? '1:0:1:SPECULATIVE' : '1:0:1:COMMITTED' },
          userPauseIntentRef: pausedRef,
          setStatus: version ? speculativeStatusChange : committedStatusChange,
        });

        if (version === 1 && suspensionPromise) {
          throw suspensionPromise;
        }

        return <video ref={videoRef} data-testid="video-child" />;
      }

      function App() {
        const [version, setVersion] = useState(0);
        triggerTransition = () => {
          suspensionPromise = new Promise<void>((resolve) => {
            resolveSuspension = resolve;
          });
          startTransition(() => {
            setVersion(1);
          });
        };

        return (
          <Suspense fallback={<div data-testid="suspense-fallback">Loading...</div>}>
            <SuspendedChild version={version} />
          </Suspense>
        );
      }

      const view = render(<App />);

      // Start recovery on committed version (version 0)
      act(() => {
        controller.reportForegroundVisibility(false, false);
        controller.reportForegroundVisibility(true, false);
      });
      const opId = controller.getActiveForegroundOperationId();
      expect(opId).not.toBeNull();

      // Trigger suspended transition
      act(() => {
        triggerTransition();
      });

      // Suspended child threw; active operation is STILL the committed one!
      expect(controller.getActiveForegroundOperationId()).toBe(opId);

      // Play rejection occurs while suspended
      await act(async () => {
        rejectPlay({ name: 'NotAllowedError' });
      });

      // The status update was delivered to COMMITTED setter, NOT speculative!
      expect(committedStatusChange).toHaveBeenCalledWith('paused');
      expect(speculativeStatusChange).not.toHaveBeenCalled();

      // Now resolve the suspension so the transition commits
      await act(async () => {
        suspensionPromise = null;
        resolveSuspension();
      });

      // After committed reveal:
      // 1. Target context is updated to replacement target
      expect(view.getByTestId('video-child')).toBeDefined();
      expect(controller.getForegroundTarget()).toEqual({
        kind: 'live',
        serviceRef: '1:0:1:SPECULATIVE',
      });

      // 2. Fresh recovery edge after reveal uses replacement callbacks
      let rejectRevealPlay!: (err: unknown) => void;
      const revealPlayPromise = new Promise<void>((_, reject) => {
        rejectRevealPlay = reject;
      });
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockReturnValue(revealPlayPromise);

      act(() => {
        controller.reportForegroundVisibility(false, false);
        controller.reportForegroundVisibility(true, false);
      });

      await act(async () => {
        rejectRevealPlay({ name: 'NotAllowedError' });
      });

      // Speculative (now committed) status change received the pause update!
      expect(speculativeStatusChange).toHaveBeenCalledWith('paused');
    });
  });

  // ---------------------------------------------------------------------------
  // Row 12: Facade events (real window offline/online, visibilitychange, connectionLost)
  // ---------------------------------------------------------------------------
  describe('Row 12: Real DOM facade events and connectionLost changes', () => {
    it('coordinates real window offline/online and visibilitychange events with HLS startLoad and DOM play', async () => {
      const controller = createTestController('playing');
      let playCalls = 0;
      let startLoadCalls = 0;

      function FacadeEventHarness() {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>({
          startLoad: () => {
            startLoadCalls++;
          },
          destroy: vi.fn(),
        } as unknown as Hls);
        const userPauseIntentRef = useRef(false);
        const [isDocVisible, setIsDocVisible] = useState(true);
        const [isWindowOnline, setIsWindowOnline] = useState(true);

        React.useEffect(() => {
          const onVisChange = () => setIsDocVisible(document.visibilityState === 'visible');
          const onOnline = () => setIsWindowOnline(true);
          const onOffline = () => setIsWindowOnline(false);

          document.addEventListener('visibilitychange', onVisChange);
          window.addEventListener('online', onOnline);
          window.addEventListener('offline', onOffline);

          return () => {
            document.removeEventListener('visibilitychange', onVisChange);
            window.removeEventListener('online', onOnline);
            window.removeEventListener('offline', onOffline);
          };
        }, []);

        useForegroundRecovery({
          controller,
          videoRef: videoRef as any,
          hlsRef,
          isEligible: true,
          isDocumentVisible: isDocVisible,
          isOnline: isWindowOnline,
          hasActiveSession: true,
          target: { kind: 'live', serviceRef: '1:0:1:LIVE_FACADE' },
          userPauseIntentRef,
          setStatus: () => {},
        });

        return (
          <video
            ref={(el) => {
              if (el) {
                el.play = vi.fn().mockImplementation(() => {
                  playCalls++;
                  return Promise.resolve();
                });
              }
              videoRef.current = el;
            }}
          />
        );
      }

      render(<FacadeEventHarness />);
      expect(playCalls).toBe(0);

      // 1. Simulate network disconnect event
      act(() => {
        fireEvent(window, new Event('offline'));
      });
      expect(playCalls).toBe(0);

      // 2. Simulate document hide event
      act(() => {
        Object.defineProperty(document, 'visibilityState', {
          value: 'hidden',
          configurable: true,
        });
        fireEvent(document, new Event('visibilitychange'));
      });

      // 3. Network reconnects while document is still hidden
      act(() => {
        fireEvent(window, new Event('online'));
      });

      // Online recovery triggers HLS reload and nudges video while hidden!
      expect(startLoadCalls).toBe(1);
      expect(playCalls).toBe(1);
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));

      // 4. Document returns to foreground
      act(() => {
        Object.defineProperty(document, 'visibilityState', {
          value: 'visible',
          configurable: true,
        });
        fireEvent(document, new Event('visibilitychange'));
      });

      // Foreground joins running online operation without duplicate play call!
      expect(playCalls).toBe(1);
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online', 'foreground']));

      // 5. Timer advances to 400ms -> single shared nudge tick
      act(() => {
        vi.advanceTimersByTime(400);
      });
      expect(playCalls).toBe(2);
    });

    it('reacts to domain connectionLost transitions dispatched to the controller', () => {
      const controller = createTestController('playing', false);
      let playCalls = 0;
      let startLoadCalls = 0;

      function DomainConnectionLostHarness() {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>({
          startLoad: () => {
            startLoadCalls++;
          },
        } as unknown as Hls);
        const userPauseIntentRef = useRef(false);

        useForegroundRecovery({
          controller,
          videoRef: videoRef as any,
          hlsRef,
          isEligible: true,
          isDocumentVisible: true,
          isOnline: true,
          hasActiveSession: true,
          target: { kind: 'live', serviceRef: '1:0:1:DOMAIN_LOST' },
          userPauseIntentRef,
          setStatus: () => {},
        });

        return (
          <video
            ref={(el) => {
              if (el) {
                el.play = vi.fn().mockImplementation(() => {
                  playCalls++;
                  return Promise.resolve();
                });
              }
              videoRef.current = el;
            }}
          />
        );
      }

      render(<DomainConnectionLostHarness />);
      expect(playCalls).toBe(0);

      // Domain state loses connection (e.g. heartbeat 2 consecutive reachability timeouts)
      act(() => {
        controller.dispatch({
          type: 'normative.session.lease.updated',
          epoch: 1,
          sessionEpoch: 0,
          leaseExpiresAt: null,
          connectionLost: true,
        });
      });
      expect(controller.getState().connectionLost).toBe(true);
      expect(playCalls).toBe(0);

      // Domain state recovers connection (e.g. subsequent heartbeat 200 OK)
      act(() => {
        controller.dispatch({
          type: 'normative.session.lease.updated',
          epoch: 1,
          sessionEpoch: 0,
          leaseExpiresAt: new Date(Date.now() + 60000).toISOString(),
          connectionLost: false,
        });
      });

      // Clearing connectionLost while online completed the edge!
      expect(controller.getState().connectionLost).toBe(false);
      expect(startLoadCalls).toBe(1);
      expect(playCalls).toBe(1);
      expect(controller.getActiveForegroundOperationId()).not.toBeNull();
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
    });

    it('coordinates offline/online and visibilitychange events with shared play under real usePlaybackOrchestrator facade', async () => {
      let playCalls = 0;

      const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        const u = String(url);
        if (u.includes('/live/stream-info')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              mode: 'direct_stream',
              playbackDecisionToken: 'tok-orchestrator-facade',
              decision: { mode: 'direct_stream', playbackDecisionToken: 'tok-orchestrator-facade' },
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/intents')) {
          const body = init?.body ? JSON.parse(String(init.body)) : {};
          if (body.type === 'stream.start') {
            return Promise.resolve({
              ok: true,
              status: 200,
              headers: new Headers(),
              json: async () => ({ sessionId: 'sess-orchestrator-facade' }),
              text: async () => JSON.stringify({ sessionId: 'sess-orchestrator-facade' }),
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-orchestrator-facade/heartbeat')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              acknowledged: true,
              sessionId: 'sess-orchestrator-facade',
              leaseExpiresAt: new Date(Date.now() + 60000).toISOString(),
            }),
            text: async () => JSON.stringify({}),
          });
        }
        if (u.includes('/sessions/sess-orchestrator-facade')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({
              sessionId: 'sess-orchestrator-facade',
              state: 'READY',
              mode: 'LIVE',
              playbackUrl: 'http://test/live.m3u8',
              heartbeatIntervalSeconds: 1,
              leaseExpiresAt: new Date(Date.now() + 60000).toISOString(),
            }),
            text: async () => JSON.stringify({}),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => JSON.stringify({}),
        });
      });
      vi.stubGlobal('fetch', fetchMock);

      let orchestratorController!: PlaybackController;

      function OrchestratorFacadeHarness() {
        const containerRef = useRef<HTMLDivElement>(null);
        const videoRef = useRef<VideoElementRef>(null);
        const hlsRef = useRef<HlsInstanceRef>(null);
        const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);

        const orchestrator = usePlaybackOrchestrator(
          { autoStart: false } as unknown as V3PlayerProps,
          { containerRef, videoRef, hlsRef, resumePrimaryActionRef },
        );
        orchestratorController = orchestrator.controller;

        return (
          <div>
            <video
              ref={(el) => {
                if (el) {
                  el.play = vi.fn().mockImplementation(() => {
                    playCalls++;
                    return Promise.resolve();
                  });
                }
                videoRef.current = el;
              }}
            />
            <button onClick={() => void orchestrator.actions.startStream('1:0:1:TEST_FACADE')} type="button">
              start-live
            </button>
          </div>
        );
      }

      const view = render(<OrchestratorFacadeHarness />);
      controllers.push(orchestratorController);

      // Start live session and wait for established session
      await act(async () => {
        fireEvent.click(view.getByText('start-live'));
      });
      expect(orchestratorController.getActiveSessionId()).toBe('sess-orchestrator-facade');

      const initialPlayCalls = playCalls;

      // 1. Simulate network offline event
      act(() => {
        fireEvent(window, new Event('offline'));
      });

      // 2. Simulate document hide event
      act(() => {
        Object.defineProperty(document, 'visibilityState', {
          value: 'hidden',
          configurable: true,
        });
        fireEvent(document, new Event('visibilitychange'));
      });

      // 3. Network reconnects while document is hidden
      act(() => {
        fireEvent(window, new Event('online'));
      });

      // Online recovery triggers play on attached media while hidden
      expect(playCalls).toBe(initialPlayCalls + 1);
      expect(orchestratorController.getActiveResumeParticipants()).toEqual(new Set(['online']));

      // 4. Document returns to foreground
      act(() => {
        Object.defineProperty(document, 'visibilityState', {
          value: 'visible',
          configurable: true,
        });
        fireEvent(document, new Event('visibilitychange'));
      });

      // Foreground joins running online operation without duplicate play!
      expect(playCalls).toBe(initialPlayCalls + 1);
      expect(orchestratorController.getActiveResumeParticipants()).toEqual(new Set(['online', 'foreground']));

      // 5. Shared 400ms tick advances budget
      act(() => {
        vi.advanceTimersByTime(400);
      });
      expect(playCalls).toBe(initialPlayCalls + 2);

      // 6. Domain connectionLost changes through lease update
      act(() => {
        orchestratorController.dispatch({
          type: 'normative.session.lease.updated',
          epoch: orchestratorController.getEpoch(),
          sessionEpoch: 0,
          leaseExpiresAt: null,
          connectionLost: true,
        });
      });
      expect(orchestratorController.getState().connectionLost).toBe(true);

      act(() => {
        orchestratorController.dispatch({
          type: 'normative.session.lease.updated',
          epoch: orchestratorController.getEpoch(),
          sessionEpoch: 0,
          leaseExpiresAt: new Date(Date.now() + 60000).toISOString(),
          connectionLost: false,
        });
      });
      expect(orchestratorController.getState().connectionLost).toBe(false);
    });
  });
});
