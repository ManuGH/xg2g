// Independent Step 3c follow-up: complete commit context before recovery actions.
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
  cleanup();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function createController(connectionLost = false) {
  const controller = createPlaybackController({
    createInitialState: () => ({
      ...createInitialPlaybackDomainState(),
      status: 'playing', playbackMode: 'LIVE', connectionLost,
    }),
  });
  controllers.push(controller);
  return controller;
}

describe('Step 3c follow-up commit ordering', () => {
  it.each(['replace', 'attach'] as const)(
    'recovers the committed video when reconnect and media %s share a commit', (mode) => {
      const controller = createController();
      const played: HTMLMediaElement[] = [];
      vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
        played.push(this);
        return Promise.resolve();
      });
      function Harness({ changed, detached = false }: { changed: boolean; detached?: boolean }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>(null);
        const paused = useRef(false);
        useForegroundRecovery({
          controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
          isOnline: changed, hasActiveSession: true,
          target: { kind: 'live', serviceRef: 'channel-a' },
          userPauseIntentRef: paused, setStatus: () => {},
        });
        if (detached) return null;
        return <video key={changed ? 'b' : 'a'} data-testid="video" ref={videoRef} />;
      }
      const view = render(<Harness changed={false} />);
      expect(played).toHaveLength(0);
      const oldVideo = view.getByTestId('video');
      if (mode === 'attach') view.rerender(<Harness changed={false} detached />);
      view.rerender(<Harness changed />);
      const currentVideo = view.getByTestId('video');
      expect(currentVideo).not.toBe(oldVideo);
      expect(oldVideo.isConnected).toBe(false);
      expect(played).toHaveLength(1);
      expect(played[0]).toBe(currentVideo);
      expect(controller.getActiveResumeParticipants()).toEqual(new Set(['online']));
    },
  );

  it.each(['offline', 'session_removed'] as const)(
    'does not use stale parent context when child clears connectionLost during %s commit', (change) => {
      const controller = createController(true);
      const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue().mockClear();
      function DomainChild({ changed }: { changed: boolean }) {
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
          isOnline: !(changed && change === 'offline'),
          hasActiveSession: !(changed && change === 'session_removed'),
          target: { kind: 'live', serviceRef: 'channel-a' },
          userPauseIntentRef: paused, setStatus: () => {},
        });
        return <><video ref={videoRef} /><DomainChild changed={changed} /></>;
      }
      const view = render(<Parent changed={false} />);
      expect(play).not.toHaveBeenCalled();
      view.rerender(<Parent changed />);
      expect(controller.getState().connectionLost).toBe(false);
      expect(play).not.toHaveBeenCalled();
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    },
  );
});
