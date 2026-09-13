// Independent review (Step 3c acceptance): combined commit-boundary timelines.
// Every case drives the real useForegroundRecovery adapter against the real controller and
// asserts on DOM element identity for play() so detached media can never satisfy a recovery.
import { StrictMode, Suspense, useLayoutEffect, useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { buildPlaybackFailure, createInitialPlaybackDomainState } from './playbackMachine';
import type { PlaybackRetryTarget } from './playbackTypes';
import type { PlayerStatus } from '../../../types/v3-player';
import { useForegroundRecovery } from './useForegroundRecovery';

type Order =
  | 'parent_hook'
  | 'child_hook'
  | 'sibling_reporter_first'
  | 'sibling_hook_first'
  | 'same_component_before'
  | 'same_component_after';

const ORDERS: Order[] = [
  'parent_hook',
  'child_hook',
  'sibling_reporter_first',
  'sibling_hook_first',
  'same_component_before',
  'same_component_after',
];

const controllers: PlaybackController[] = [];

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.clearAllTimers();
  cleanup();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function createController(status: PlayerStatus = 'playing'): PlaybackController {
  const controller = createPlaybackController({
    createInitialState: () => ({
      ...createInitialPlaybackDomainState(),
      status,
      playbackMode: 'LIVE',
      connectionLost: true,
    }),
  });
  controllers.push(controller);
  return controller;
}

function spyPlay(): HTMLMediaElement[] {
  const played: HTMLMediaElement[] = [];
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
    played.push(this);
    return Promise.resolve();
  });
  return played;
}

function clearLoss(controller: PlaybackController): void {
  controller.dispatch({
    type: 'normative.session.lease.updated',
    epoch: controller.getEpoch(),
    sessionEpoch: 0,
    leaseExpiresAt: null,
    connectionLost: false,
  });
}

function raiseLoss(controller: PlaybackController): void {
  controller.dispatch({
    type: 'normative.session.lease.updated',
    epoch: controller.getEpoch(),
    sessionEpoch: 0,
    leaseExpiresAt: null,
    connectionLost: true,
  });
}

interface HarnessOptions {
  controller: PlaybackController;
  order: Order;
  media: 'replace' | 'reattach' | 'stable';
  onlineWhen?: (changed: boolean) => boolean;
  targetWhen?: (changed: boolean) => PlaybackRetryTarget | null;
  /** Runs in a layout effect sequenced after the reporter's edge, while the commit is still open. */
  afterEdge?: (controller: PlaybackController) => void;
}

interface HarnessProps {
  changed: boolean;
  detached?: boolean;
}

/**
 * Builds a tree for the requested effect hierarchy. The "reporter" clears domain loss in a
 * layout effect exactly once (when `changed` flips); the "hook owner" renders the video and
 * runs useForegroundRecovery. `media` controls whether the video is keyed (replace), removed
 * in an intermediate commit (reattach), or stable.
 */
function buildHarness(opts: HarnessOptions) {
  const { controller, order, media } = opts;
  const onlineWhen = opts.onlineWhen ?? (() => true);
  const targetWhen =
    opts.targetWhen ?? ((): PlaybackRetryTarget => ({ kind: 'live', serviceRef: 'channel-a' }));

  function useReporter(changed: boolean): void {
    useLayoutEffect(() => {
      if (changed) clearLoss(controller);
    }, [changed]);
  }

  function useAfterEdge(changed: boolean): void {
    useLayoutEffect(() => {
      if (changed) opts.afterEdge?.(controller);
    }, [changed]);
  }

  function useHook(changed: boolean, videoRef: React.RefObject<HTMLVideoElement | null>): void {
    const hlsRef = useRef<Hls | null>(null);
    const paused = useRef(false);
    useForegroundRecovery({
      controller,
      videoRef,
      hlsRef,
      isEligible: true,
      isDocumentVisible: true,
      isOnline: onlineWhen(changed),
      hasActiveSession: true,
      target: targetWhen(changed),
      userPauseIntentRef: paused,
      setStatus: () => {},
    });
  }

  function Video({ changed, detached, videoRef }: HarnessProps & { videoRef: React.RefObject<HTMLVideoElement | null> }) {
    if (detached) return null;
    if (media === 'replace') return <video key={changed ? 'b' : 'a'} ref={videoRef} data-testid="video" />;
    return <video ref={videoRef} data-testid="video" />;
  }

  function Reporter({ changed }: { changed: boolean }) {
    useReporter(changed);
    return null;
  }

  function AfterEdge({ changed }: { changed: boolean }) {
    useAfterEdge(changed);
    return null;
  }

  function HookOwner(props: HarnessProps) {
    const videoRef = useRef<HTMLVideoElement | null>(null);
    useHook(props.changed, videoRef);
    return <Video {...props} videoRef={videoRef} />;
  }

  switch (order) {
    case 'parent_hook':
      return function ParentHook(props: HarnessProps) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        useHook(props.changed, videoRef);
        return (
          <>
            <Video {...props} videoRef={videoRef} />
            <Reporter changed={props.changed} />
            <AfterEdge changed={props.changed} />
          </>
        );
      };
    case 'child_hook':
      return function ChildHook(props: HarnessProps) {
        useReporter(props.changed);
        useAfterEdge(props.changed);
        return <HookOwner {...props} />;
      };
    case 'sibling_reporter_first':
      return function SiblingReporterFirst(props: HarnessProps) {
        return (
          <>
            <Reporter changed={props.changed} />
            <AfterEdge changed={props.changed} />
            <HookOwner {...props} />
          </>
        );
      };
    case 'sibling_hook_first':
      return function SiblingHookFirst(props: HarnessProps) {
        return (
          <>
            <HookOwner {...props} />
            <Reporter changed={props.changed} />
            <AfterEdge changed={props.changed} />
          </>
        );
      };
    case 'same_component_before':
      return function SameBefore(props: HarnessProps) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        useReporter(props.changed);
        useAfterEdge(props.changed);
        useHook(props.changed, videoRef);
        return <Video {...props} videoRef={videoRef} />;
      };
    case 'same_component_after':
      return function SameAfter(props: HarnessProps) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        useHook(props.changed, videoRef);
        useReporter(props.changed);
        useAfterEdge(props.changed);
        return <Video {...props} videoRef={videoRef} />;
      };
  }
}

function driveEdgeCommit(Harness: React.ComponentType<HarnessProps>, media: 'replace' | 'reattach' | 'stable') {
  const view = render(<Harness changed={false} />);
  const oldVideo = view.getByTestId('video');
  if (media === 'reattach') view.rerender(<Harness changed={false} detached />);
  view.rerender(<Harness changed />);
  const newVideo = view.getByTestId('video');
  return { view, oldVideo, newVideo };
}

describe('Step 3c commit boundary — combined timelines', () => {
  describe('domain edge and keyed media replacement in one commit, every effect order', () => {
    it.each(ORDERS.flatMap((order) => (['replace', 'reattach'] as const).map((media) => [order, media] as const)))(
      '%s / %s: exactly one play on the newly committed video',
      (order, media) => {
        const controller = createController();
        const played = spyPlay();
        const Harness = buildHarness({ controller, order, media });

        const { oldVideo, newVideo } = driveEdgeCommit(Harness, media);

        expect(newVideo).not.toBe(oldVideo);
        expect(oldVideo.isConnected).toBe(false);
        expect(controller.getState().connectionLost).toBe(false);
        expect(played).toHaveLength(1);
        expect(played[0]).toBe(newVideo);
        expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
      },
    );
  });

  it.each(['parent_hook', 'sibling_reporter_first'] as const)(
    'browser online edge, domain edge and media replacement in one commit (%s): one play on the new video',
    (order) => {
      const controller = createController();
      const played = spyPlay();
      const Harness = buildHarness({ controller, order, media: 'replace', onlineWhen: (changed) => changed });

      const { oldVideo, newVideo } = driveEdgeCommit(Harness, 'replace');

      expect(oldVideo.isConnected).toBe(false);
      expect(played).toHaveLength(1);
      expect(played[0]).toBe(newVideo);
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
    },
  );

  it('error status: reconnect during target and media replacement retries only the new target', async () => {
    const controller = createController('error');
    const played = spyPlay();
    const startedServiceRefs: Array<string | undefined> = [];
    controller.setCommandExecutor((command) => {
      if (command.type === 'command.playback.start') {
        startedServiceRefs.push(command.serviceRef);
      }
    });
    const Harness = buildHarness({
      controller,
      order: 'parent_hook',
      media: 'replace',
      targetWhen: (changed) => ({ kind: 'live', serviceRef: changed ? 'channel-b' : 'channel-a' }),
    });

    driveEdgeCommit(Harness, 'replace');
    await act(async () => {});

    expect(played).toHaveLength(0);
    expect(controller.getActiveForegroundOperationId()).toBeNull();
    expect(controller.getForegroundTarget()).toEqual({ kind: 'live', serviceRef: 'channel-b' });
    expect(startedServiceRefs).toEqual(['channel-b']);
  });

  describe('lifecycle events while the edge is staged inside the commit', () => {
    it('stop after the staged edge: no play on either element and no resume operation', () => {
      const controller = createController();
      const played = spyPlay();
      const Harness = buildHarness({
        controller,
        order: 'parent_hook',
        media: 'replace',
        afterEdge: (c) => {
          void c.stop('user_stop');
        },
      });

      driveEdgeCommit(Harness, 'replace');

      expect(played).toHaveLength(0);
      expect(controller.getActiveForegroundOperationId()).toBeNull();
      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('dispose after the staged edge: no play and the later media publication starts nothing', () => {
      const controller = createController();
      const played = spyPlay();
      const Harness = buildHarness({
        controller,
        order: 'parent_hook',
        media: 'replace',
        afterEdge: (c) => c.dispose(),
      });

      driveEdgeCommit(Harness, 'replace');

      expect(controller.isDisposed()).toBe(true);
      expect(played).toHaveLength(0);
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    });

    it('terminal auth after the staged edge: no DOM play and no resume operation', () => {
      const controller = createController();
      const played = spyPlay();
      const Harness = buildHarness({
        controller,
        order: 'parent_hook',
        media: 'replace',
        afterEdge: (c) =>
          c.dispatch({
            type: 'normative.playback.failure.raised',
            epoch: c.getEpoch(),
            failure: buildPlaybackFailure(
              { title: 'Forbidden', code: 'SESSION_FORBIDDEN', status: 403, retryable: false },
              'orchestrator',
              { class: 'auth', terminal: true },
            ),
          }),
      });

      driveEdgeCommit(Harness, 'replace');

      expect(played).toHaveLength(0);
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    });

    it('superseding retry after the staged edge: no nudge during its preparation', () => {
      const controller = createController();
      const played = spyPlay();
      let completePreparation!: () => void;
      const pendingPreparation = new Promise<void>((resolve) => {
        completePreparation = resolve;
      });
      controller.setCommandExecutor((command) => {
        if (command.type === 'command.playback.start') {
          const epoch = controller.allocatePlaybackEpoch();
          controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting', true);
          return pendingPreparation;
        }
      });
      const Harness = buildHarness({
        controller,
        order: 'parent_hook',
        media: 'replace',
        afterEdge: (c) => {
          void c.retry({ kind: 'live', serviceRef: 'channel-a' });
        },
      });

      try {
        driveEdgeCommit(Harness, 'replace');
        expect(played).toHaveLength(0);
        expect(controller.getActiveForegroundOperationId()).toBeNull();
      } finally {
        completePreparation();
      }
    });

    it('loss re-asserted after the staged edge: nothing runs now, the next real edge recovers the current video', () => {
      const controller = createController();
      const played = spyPlay();
      const Harness = buildHarness({
        controller,
        order: 'parent_hook',
        media: 'replace',
        afterEdge: (c) => raiseLoss(c),
      });

      const { newVideo } = driveEdgeCommit(Harness, 'replace');

      expect(controller.getState().connectionLost).toBe(true);
      expect(played).toHaveLength(0);
      expect(controller.getActiveForegroundOperationId()).toBeNull();

      act(() => clearLoss(controller));

      expect(played).toHaveLength(1);
      expect(played[0]).toBe(newVideo);
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
    });
  });

  it('root StrictMode with a staged edge at mount: one play, and the barrier is open afterwards', () => {
    const controller = createController();
    const played = spyPlay();
    const Harness = buildHarness({ controller, order: 'parent_hook', media: 'stable' });

    const view = render(
      <StrictMode>
        <Harness changed />
      </StrictMode>,
    );
    const video = view.getByTestId('video');

    expect(controller.getState().connectionLost).toBe(false);
    expect(played).toHaveLength(1);
    expect(played[0]).toBe(video);

    // A later outage/recovery outside any commit must evaluate immediately against the replayed binding.
    act(() => {
      raiseLoss(controller);
      clearLoss(controller);
    });

    expect(played).toHaveLength(2);
    expect(played[1]).toBe(video);
    expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
  });

  it('post-hook Suspense hide then reveal: the staged edge recovers the re-committed video exactly once', async () => {
    const controller = createController();
    const played = spyPlay();
    let resolveSuspension!: () => void;
    let suspensionPromise: Promise<void> | null = null;

    function Reporter({ changed }: { changed: boolean }) {
      useLayoutEffect(() => {
        if (changed) clearLoss(controller);
      }, [changed]);
      return null;
    }

    function Leaf({ changed }: { changed: boolean }) {
      if (changed && suspensionPromise) throw suspensionPromise;
      return null;
    }

    function Parent({ changed }: { changed: boolean }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef<Hls | null>(null);
      const paused = useRef(false);
      useForegroundRecovery({
        controller,
        videoRef,
        hlsRef,
        isEligible: true,
        isDocumentVisible: true,
        isOnline: true,
        hasActiveSession: true,
        target: { kind: 'live', serviceRef: 'channel-a' },
        userPauseIntentRef: paused,
        setStatus: () => {},
      });
      return (
        <>
          <video ref={videoRef} data-testid="video" />
          <Reporter changed={changed} />
          <Leaf changed={changed} />
        </>
      );
    }

    function App({ changed }: { changed: boolean }) {
      return (
        <Suspense fallback={<div data-testid="fallback" />}>
          <Parent changed={changed} />
        </Suspense>
      );
    }

    const view = render(<App changed={false} />);
    const video = view.getByTestId('video');
    expect(played).toHaveLength(0);

    suspensionPromise = new Promise<void>((resolve) => {
      resolveSuspension = resolve;
    });
    view.rerender(<App changed />);

    // Hidden primary content: no speculative recovery, the edge has not been consumed.
    expect(view.getByTestId('fallback')).toBeDefined();
    expect(controller.getState().connectionLost).toBe(true);
    expect(played).toHaveLength(0);
    expect(controller.getActiveForegroundOperationId()).toBeNull();

    await act(async () => {
      suspensionPromise = null;
      resolveSuspension();
    });

    expect(view.queryByTestId('fallback')).toBeNull();
    expect(controller.getState().connectionLost).toBe(false);
    expect(played).toHaveLength(1);
    expect(played[0]).toBe(video);
    expect(view.getByTestId('video')).toBe(video);
    expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
  });
});
