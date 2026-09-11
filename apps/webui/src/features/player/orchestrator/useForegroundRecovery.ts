// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useEffect, useLayoutEffect, useRef } from 'react';
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
  isOnline?: boolean;
  hasActiveSession?: boolean;
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
  isOnline,
  hasActiveSession,
  target,
  userPauseIntentRef,
  setStatus,
}: UseForegroundRecoveryOptions): void {
  const committedVideoRef = useRef<HTMLVideoElement | null>(null);
  const committedBindingRef = useRef<ForegroundMediaBinding | null>(null);
  const committedMediaIdRef = useRef<string>('');
  const mediaIdCounterRef = useRef<number>(0);
  const setStatusRef = useRef(setStatus);

  // Cleanup media binding when controller changes or on unmount
  useLayoutEffect(() => {
    return () => {
      committedVideoRef.current = null;
      committedBindingRef.current = null;
      committedMediaIdRef.current = '';
      controller.setForegroundMediaBinding(null);
    };
  }, [controller]);

  // Synchronize committed target, eligibility, user pause, status callback, and video DOM attachment in layout phase.
  // Running on every commit ensures node replacements (e.g. key="a" -> key="b") or late-attached
  // refs are immediately resolved at the commit boundary without waiting for unrelated renders.
  // Crucially, setStatusRef is updated ONLY in the commit phase so uncommitted or suspended renders
  // never leak speculative callbacks to active asynchronous recovery operations.
  useLayoutEffect(() => {
    setStatusRef.current = setStatus;

    controller.setForegroundEligibility(isEligible);
    controller.setForegroundTarget(target);
    controller.setUserPaused(userPauseIntentRef.current);

    const currentVideo = videoRef.current;
    if (currentVideo !== committedVideoRef.current) {
      committedVideoRef.current = currentVideo;

      if (!currentVideo) {
        committedBindingRef.current = null;
        committedMediaIdRef.current = '';
        controller.setForegroundMediaBinding(null);
      } else {
        const mediaId = `vid-${++mediaIdCounterRef.current}`;
        committedMediaIdRef.current = mediaId;
        const binding: ForegroundMediaBinding = {
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
            setStatusRef.current((current) => (current === 'paused' ? 'buffering' : current));
          },
          onTransitionToPaused: () => {
            setStatusRef.current('paused');
          },
          isUserPaused: () => userPauseIntentRef.current,
          startNudge: (callbacks) => {
            return startResumePlaybackRecovery(currentVideo, {
              shouldContinue: callbacks.shouldContinue,
              onBlocked: callbacks.onBlocked,
              onFailed: callbacks.onFailed,
              onSettled: callbacks.onSettled,
            });
          },
        };
        committedBindingRef.current = binding;
        controller.setForegroundMediaBinding(binding);
      }
    }
  });

  // Document visibility edge listener.
  useEffect(() => {
    const video = videoRef.current;
    const isPiP = typeof document !== 'undefined' && Boolean(video && document.pictureInPictureElement === video);
    controller.reportForegroundVisibility(isDocumentVisible, isPiP);
  }, [controller, isDocumentVisible, videoRef]);

  // Browser connectivity edge listener.
  useEffect(() => {
    controller.reportBrowserConnectivity({
      online: isOnline ?? true,
      hasActiveSession: hasActiveSession ?? false,
    });
  }, [controller, isOnline, hasActiveSession]);
}
