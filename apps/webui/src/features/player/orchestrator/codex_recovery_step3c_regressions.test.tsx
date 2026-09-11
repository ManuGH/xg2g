// Independent Step 3c review counterexamples. Copy beside playbackController.ts.
import { useLayoutEffect, useRef } from 'react';
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createInitialPlaybackDomainState } from './playbackMachine';
import { createPlaybackForegroundRuntime } from './playbackForegroundRuntime';
import { useForegroundRecovery } from './useForegroundRecovery';

const controllers: PlaybackController[] = [];
afterEach(() => {
  cleanup();
  controllers.splice(0).forEach((controller) => controller.dispose());
  vi.restoreAllMocks();
});

describe('Step 3c independent review', () => {
  it('does not retry an error on reconnect without an attached media binding', () => {
    const retry = vi.fn(async () => ({ status: 'restarted' as const, epoch: 2 }));
    const runtime = createPlaybackForegroundRuntime({
      getDomainStatus: () => 'error', getPlaybackEpoch: () => 1,
      isStalePlaybackEpoch: () => false, isStoppedEpoch: () => false,
      isDisposed: () => false, onRetry: retry,
    });
    runtime.setTargetContext({ kind: 'live', serviceRef: 'channel-a' });
    runtime.setMediaBinding(null);
    runtime.updateConnectivity(false, true);
    runtime.updateConnectivity(true, true);
    expect(retry).not.toHaveBeenCalled();
  });

  it.each(['offline', 'session_removed'] as const)(
    'does not recover using stale committed context when %s and domain recovery share a commit',
    (change) => {
      const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue().mockClear();
      const reload = vi.fn();
      const controller = createPlaybackController({
        createInitialState: () => ({
          ...createInitialPlaybackDomainState(),
          status: 'playing', playbackMode: 'LIVE', connectionLost: true,
        }),
      });
      controllers.push(controller);

      function Harness({ changed }: { changed: boolean }) {
        const videoRef = useRef<HTMLVideoElement | null>(null);
        const hlsRef = useRef<Hls | null>({ startLoad: reload } as unknown as Hls);
        const pauseRef = useRef(false);
        useForegroundRecovery({
          controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
          isOnline: !(changed && change === 'offline'),
          hasActiveSession: !(changed && change === 'session_removed'),
          target: { kind: 'live', serviceRef: 'channel-a' },
          userPauseIntentRef: pauseRef, setStatus: () => {},
        });
        // This runs after the adapter's context layout effect, before passive effects.
        useLayoutEffect(() => {
          if (changed) {
            controller.dispatch({
              type: 'normative.session.lease.updated', epoch: controller.getEpoch(),
              sessionEpoch: 0, leaseExpiresAt: null, connectionLost: false,
            });
          }
        }, [changed]);
        return <video ref={videoRef} />;
      }

      const view = render(<Harness changed={false} />);
      expect(play).not.toHaveBeenCalled();
      view.rerender(<Harness changed />);
      expect(controller.getState().connectionLost).toBe(false);
      expect(play).not.toHaveBeenCalled();
      expect(reload).not.toHaveBeenCalled();
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    },
  );

  it('does not start online nudging while an existing retry is preparing its replacement', async () => {
    const controller = createPlaybackController({ createInitialState: createInitialPlaybackDomainState });
    controllers.push(controller);
    let completePreparation!: () => void;
    const pendingPreparation = new Promise<void>((resolve) => { completePreparation = resolve; });
    controller.setCommandExecutor((command) => {
      if (command.type === 'command.playback.start') {
        const epoch = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting', true);
        return pendingPreparation;
      }
    });
    const epoch = controller.allocatePlaybackEpoch();
    controller.beginPlaybackAttempt(epoch, 'LIVE', 'playing', true);
    const nudge = vi.fn(() => () => {});
    controller.setForegroundMediaBinding({ mediaId: 'video-a', startNudge: nudge });
    controller.setForegroundTarget({ kind: 'live', serviceRef: 'channel-a' });
    controller.reportBrowserConnectivity({ online: false, hasActiveSession: true });
    await controller.retry({ kind: 'live', serviceRef: 'channel-a' });
    expect(controller.isRetryInFlight()).toBe(true);
    controller.reportBrowserConnectivity({ online: true, hasActiveSession: true });
    try {
      expect(nudge).not.toHaveBeenCalled();
      expect(controller.getActiveForegroundOperationId()).toBeNull();
    } finally {
      completePreparation();
    }
  });
});
