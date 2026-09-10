// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useEffect, useLayoutEffect, useMemo, useRef } from 'react';
import type { PlaybackController } from './playbackController';
import type { ForegroundMediaBinding } from './playbackForegroundRuntime';
import { startResumePlaybackRecovery } from './resumePlaybackRecovery';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackRetryTarget } from './playbackTypes';
import type Hls from 'hls.js';
import { debugWarn } from '../../../utils/logging';

export interface UseForegroundRecoveryOptions {
  controller: PlaybackController;
  videoRef: React.RefObject<HTMLVideoElement | null>;
  hlsRef: React.RefObject<Hls | null>;
  isEligible: boolean;
  isDocumentVisible: boolean;
  target: PlaybackRetryTarget | null;
  userPauseIntentRef: React.MutableRefObject<boolean>;
  setStatus: React.Dispatch<React.SetStateAction<PlayerStatus>>;
}

export function useForegroundRecovery({
  controller,
  videoRef,
  hlsRef,
  isEligible,
  isDocumentVisible,
  target,
  userPauseIntentRef,
  setStatus,
}: UseForegroundRecoveryOptions): void {
  const lastVideoElementRef = useRef<HTMLVideoElement | null>(null);
  const mediaIdRef = useRef<string>('');
  const mediaIdCounterRef = useRef(0);

  const currentVideo = videoRef.current;
  if (currentVideo !== lastVideoElementRef.current) {
    lastVideoElementRef.current = currentVideo;
    mediaIdRef.current = currentVideo ? `vid-${++mediaIdCounterRef.current}` : '';
  }

  const mediaBinding = useMemo<ForegroundMediaBinding | null>(() => {
    const video = videoRef.current;
    if (!video) {
      return null;
    }
    const mediaId = mediaIdRef.current;

    return {
      mediaId,
      onHlsReload: () => {
        if (hlsRef.current) {
          try {
            hlsRef.current.startLoad();
          } catch (err) {
            debugWarn('[V3Player] hls resume startLoad failed', err);
          }
        }
      },
      onTransitionToBuffering: () => {
        setStatus((current) => (current === 'paused' ? 'buffering' : current));
      },
      onTransitionToPaused: () => {
        setStatus('paused');
      },
      isUserPaused: () => userPauseIntentRef.current,
      startNudge: (callbacks) => {
        return startResumePlaybackRecovery(video, {
          shouldContinue: callbacks.shouldContinue,
          onBlocked: callbacks.onBlocked,
          onFailed: callbacks.onFailed,
        });
      },
    };
  }, [hlsRef, setStatus, videoRef, userPauseIntentRef]);

  // Publish committed target, media binding, and eligibility in the layout phase (commit phase).
  // This guarantees uncommitted / suspended renders never mutate controller state.
  useLayoutEffect(() => {
    controller.setForegroundEligibility(isEligible);
    controller.setForegroundTarget(target);
    controller.setForegroundMediaBinding(mediaBinding);
    controller.setUserPaused(userPauseIntentRef.current);
  }, [controller, isEligible, target, mediaBinding, userPauseIntentRef]);

  // Document visibility edge listener.
  useEffect(() => {
    const video = videoRef.current;
    const isPiP = typeof document !== 'undefined' && Boolean(video && document.pictureInPictureElement === video);
    controller.reportForegroundVisibility(isDocumentVisible, isPiP);
  }, [controller, isDocumentVisible, videoRef]);

  // On unmount, detach media binding cleanly
  useEffect(() => {
    return () => {
      controller.setForegroundMediaBinding(null);
    };
  }, [controller]);
}
