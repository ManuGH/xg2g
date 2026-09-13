// Independent review: domain recovery and media replacement in one tree commit.
import { useLayoutEffect, useRef } from 'react';
import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createInitialPlaybackDomainState } from './playbackMachine';
import { useForegroundRecovery } from './useForegroundRecovery';

const controllers: PlaybackController[] = [];
beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => {
  vi.clearAllTimers();
  cleanup();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('Step 3c complete commit boundary', () => {
  it.each(['replace', 'reattach'] as const)(
    'uses current media when a child clears domain loss during parent media %s', (mode) => {
      const controller = createPlaybackController({
        createInitialState: () => ({
          ...createInitialPlaybackDomainState(),
          status: 'playing', playbackMode: 'LIVE', connectionLost: true,
        }),
      });
      controllers.push(controller);
      const played: HTMLMediaElement[] = [];
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
        played.push(this);
        return Promise.resolve();
      });
      function Reporter({ changed }: { changed: boolean }) {
        useLayoutEffect(() => {
          if (changed) controller.dispatch({
            type: 'normative.session.lease.updated', epoch: controller.getEpoch(),
            sessionEpoch: 0, leaseExpiresAt: null, connectionLost: false,
          });
        }, [changed]);
        return null;
      }
      function Parent({ changed, detached = false }: { changed: boolean; detached?: boolean }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>(null);
        const paused = useRef(false);
        useForegroundRecovery({
          controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
          isOnline: true, hasActiveSession: true,
          target: { kind: 'live', serviceRef: 'channel-a' },
          userPauseIntentRef: paused, setStatus: () => {},
        });
        return <>
          {!detached && <video key={changed ? 'b' : 'a'} ref={videoRef} data-testid="video" />}
          <Reporter changed={changed} />
        </>;
      }
      const view = render(<Parent changed={false} />);
      const oldVideo = view.getByTestId('video');
      expect(played).toHaveLength(0);
      if (mode === 'reattach') view.rerender(<Parent changed={false} detached />);
      view.rerender(<Parent changed />);
      const newVideo = view.getByTestId('video');
      expect(newVideo).not.toBe(oldVideo);
      expect(oldVideo.isConnected).toBe(false);
      expect(controller.getState().connectionLost).toBe(false);
      expect(played).toHaveLength(1);
      expect(played[0]).toBe(newVideo);
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
    },
  );

  it('handles simultaneous target and media change: no old-element play and uses new target', () => {
    const controller = createPlaybackController({
      createInitialState: () => ({
        ...createInitialPlaybackDomainState(),
        status: 'playing', playbackMode: 'LIVE', connectionLost: true,
      }),
    });
    controllers.push(controller);
    const played: HTMLMediaElement[] = [];
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
      played.push(this);
      return Promise.resolve();
    });

    function Reporter({ changed }: { changed: boolean }) {
      useLayoutEffect(() => {
        if (changed) controller.dispatch({
          type: 'normative.session.lease.updated', epoch: controller.getEpoch(),
          sessionEpoch: 0, leaseExpiresAt: null, connectionLost: false,
        });
      }, [changed]);
      return null;
    }

    function Parent({ changed }: { changed: boolean }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef<Hls | null>(null);
      const paused = useRef(false);
      useForegroundRecovery({
        controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
        isOnline: true, hasActiveSession: true,
        target: { kind: 'live', serviceRef: changed ? 'channel-b' : 'channel-a' },
        userPauseIntentRef: paused, setStatus: () => {},
      });
      return <>
        <video key={changed ? 'b' : 'a'} ref={videoRef} data-testid="video" />
        <Reporter changed={changed} />
      </>;
    }

    const view = render(<Parent changed={false} />);
    const oldVideo = view.getByTestId('video');
    expect(played).toHaveLength(0);

    view.rerender(<Parent changed />);
    const newVideo = view.getByTestId('video');
    expect(newVideo).not.toBe(oldVideo);
    expect(oldVideo.isConnected).toBe(false);
    expect(controller.getState().connectionLost).toBe(false);
    expect(controller.getForegroundTarget()).toEqual({ kind: 'live', serviceRef: 'channel-b' });
    expect(played).toHaveLength(1);
    expect(played[0]).toBe(newVideo);
    expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
  });

  it('recovers stable media with domain-only recovery with exactly one operation and play', () => {
    const controller = createPlaybackController({
      createInitialState: () => ({
        ...createInitialPlaybackDomainState(),
        status: 'playing', playbackMode: 'LIVE', connectionLost: true,
      }),
    });
    controllers.push(controller);
    const played: HTMLMediaElement[] = [];
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
      played.push(this);
      return Promise.resolve();
    });

    function Reporter({ changed }: { changed: boolean }) {
      useLayoutEffect(() => {
        if (changed) controller.dispatch({
          type: 'normative.session.lease.updated', epoch: controller.getEpoch(),
          sessionEpoch: 0, leaseExpiresAt: null, connectionLost: false,
        });
      }, [changed]);
      return null;
    }

    function Parent({ changed }: { changed: boolean }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef<Hls | null>(null);
      const paused = useRef(false);
      useForegroundRecovery({
        controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
        isOnline: true, hasActiveSession: true,
        target: { kind: 'live', serviceRef: 'channel-a' },
        userPauseIntentRef: paused, setStatus: () => {},
      });
      return <>
        <video ref={videoRef} data-testid="video" />
        <Reporter changed={changed} />
      </>;
    }

    const view = render(<Parent changed={false} />);
    const video = view.getByTestId('video');
    expect(played).toHaveLength(0);

    view.rerender(<Parent changed />);
    expect(controller.getState().connectionLost).toBe(false);
    expect(played).toHaveLength(1);
    expect(played[0]).toBe(video);
    expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
  });

  it('aborts commit phase and cancels active operation on unmount', () => {
    const controller = createPlaybackController({
      createInitialState: () => ({
        ...createInitialPlaybackDomainState(),
        status: 'playing', playbackMode: 'LIVE', connectionLost: true,
      }),
    });
    controllers.push(controller);
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue().mockClear();

    function Parent() {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef<Hls | null>(null);
      const paused = useRef(false);
      useForegroundRecovery({
        controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
        isOnline: true, hasActiveSession: true,
        target: { kind: 'live', serviceRef: 'channel-a' },
        userPauseIntentRef: paused, setStatus: () => {},
      });
      return <video ref={videoRef} />;
    }

    const view = render(<Parent />);
    view.unmount();
    expect(controller.getActiveForegroundOperationId()).toBeNull();
    expect(play).not.toHaveBeenCalled();
  });
});
