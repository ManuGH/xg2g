// Copy beside playbackController.ts to run. Independent review counterexamples.
import { Suspense, startTransition, useRef, useState } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { createPlaybackController, type PlaybackController } from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import { useForegroundRecovery } from './useForegroundRecovery';

const controllers: PlaybackController[] = [];
function controllerAt(epoch = 1) {
  const controller = createPlaybackController({
    createInitialState: () => ({
      epoch: { playback: epoch, session: 0 }, traceId: '-', status: 'playing',
      playbackMode: 'LIVE', vodStreamMode: null, activeHlsEngine: null,
      durationSeconds: null, canSeek: false, startUnix: null,
      sessionPhase: 'ready', mediaPhase: 'playing', contract: null, failure: null,
      lastAdvisory: null, explicitProfilePinned: false, hasSessionIntent: true,
      recovery: createRecoveryLadderState(), leaseExpiresAt: null, connectionLost: false,
    }),
  });
  controllers.push(controller);
  return controller;
}

describe('Step 3b committed callback follow-up', () => {
  afterEach(() => {
    cleanup();
    controllers.splice(0).forEach((c) => c.dispose());
    vi.restoreAllMocks();
  });

  it('delivers a pending play rejection to the committed setter after the hook renders speculatively', async () => {
    const controller = controllerAt();
    const committed = vi.fn();
    const speculative = vi.fn();
    let rejectPlay!: (error: unknown) => void;
    const pendingPlay = new Promise<void>((_resolve, reject) => { rejectPlay = reject; });
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockReturnValue(pendingPlay);
    const never = new Promise<void>(() => {});
    let attemptedSpeculativeRender = false;
    let change!: () => void;

    function BoundaryChild({ version }: { version: number }) {
      const videoRef = useRef<HTMLVideoElement | null>(null);
      const hlsRef = useRef<Hls | null>(null);
      const paused = useRef(false);
      useForegroundRecovery({
        controller, videoRef, hlsRef, isEligible: true, isDocumentVisible: true,
        target: { kind: 'live', serviceRef: version ? 'speculative-b' : 'committed-a' },
        userPauseIntentRef: paused, setStatus: version ? speculative : committed,
      });
      // The changed hook inputs must actually render before suspension.
      if (version) {
        attemptedSpeculativeRender = true;
        throw never;
      }
      return <video ref={videoRef} data-testid="committed-video" />;
    }
    function App() {
      const [version, setVersion] = useState(0);
      change = () => startTransition(() => setVersion(1));
      return <Suspense fallback={<div data-testid="fallback" />}><BoundaryChild version={version} /></Suspense>;
    }

    const view = render(<App />);
    act(() => {
      controller.reportForegroundVisibility(false, false);
      controller.reportForegroundVisibility(true, false);
    });
    const operation = controller.getActiveForegroundOperationId();
    expect(operation).not.toBeNull();
    committed.mockClear();
    act(() => { change(); });
    expect(attemptedSpeculativeRender).toBe(true);
    expect(view.queryByTestId('fallback')).toBeNull();
    expect(view.getByTestId('committed-video')).toBeDefined();
    expect(controller.getActiveForegroundOperationId()).toBe(operation);
    await act(async () => { rejectPlay({ name: 'NotAllowedError' }); });
    expect(speculative).not.toHaveBeenCalled();
    expect(committed).toHaveBeenCalledWith('paused');
  });
});
