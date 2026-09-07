// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import type { Dispatch } from 'react';
import {
  createPlaybackController,
  type PlaybackController,
} from './playbackController';
import type { PlaybackCommandExecutor } from './playbackMachineRuntime';
import type { LiveSessionTransport } from './liveSessionTransport';
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';

export interface UsePlaybackControllerOptions {
  startSettlementTimeoutMs?: number;
  httpRequestTimeoutMs?: number;
  stopRequestTimeoutMs?: number;
  requestedDuration?: number | null;
  onAttemptStarted?: (epoch: number) => void;
}

export interface UsePlaybackControllerResult {
  controller: PlaybackController;
  state: PlaybackDomainState;
  dispatch: Dispatch<PlaybackMachineEvent>;
}

// React integration hook for PlaybackController.
//
// Lifecycle architecture:
// - Controller instance is created once via useState and exposed via useSyncExternalStore.
// - Executor identity is decoupled from controller lifecycle: executorRef is updated
//   synchronously on every render, so changing callbacks never causes effect tear-down.
// - useEffect manages component mount/unmount lifecycle:
//   * StrictMode mount simulation (Setup 1 -> Cleanup 1 -> Setup 2) re-runs effect setup;
//     controller disposal is idempotent and subsequent setup re-establishes active state.
//   * On real unmount, effect cleanup runs, disconnecting the executor and calling controller.dispose().
//   * useLayoutEffect setup re-connects the executor on mount and when revealed from Suspense.
export function usePlaybackController(
  transport: LiveSessionTransport,
  createInitialState: () => PlaybackDomainState,
  executeCommand: PlaybackCommandExecutor,
  options?: UsePlaybackControllerOptions,
): UsePlaybackControllerResult {
  const transportRef = useRef<LiveSessionTransport>(transport);
  transportRef.current = transport;

  const optionsRef = useRef<UsePlaybackControllerOptions | undefined>(options);
  optionsRef.current = options;

  const executorRef = useRef<PlaybackCommandExecutor | null>(executeCommand);
  executorRef.current = executeCommand;

  const [controller] = useState(() =>
    createPlaybackController({
      getTransport: () => transportRef.current,
      createInitialState,
      executeCommand: (command) => {
        executorRef.current?.(command);
      },
      startSettlementTimeoutMs: options?.startSettlementTimeoutMs,
      httpRequestTimeoutMs: options?.httpRequestTimeoutMs,
      stopRequestTimeoutMs: options?.stopRequestTimeoutMs,
      get requestedDuration() {
        return optionsRef.current?.requestedDuration;
      },
      onAttemptStarted: (epoch) => {
        optionsRef.current?.onAttemptStarted?.(epoch);
      },
    }),
  );

  // Reconnect executor and reactivate controller on each render.
  // This ensures child layout effects (which run before parent effects)
  // have a valid executor during StrictMode remount, and clears isDisposed.
  controller.setCommandExecutor((command) => {
    return executorRef.current?.(command);
  });

  useEffect(() => {
    return () => {
      controller.dispose();
    };
  }, [controller]);

  const state = useSyncExternalStore(
    controller.subscribe,
    controller.getState,
    controller.getState,
  );

  return {
    controller,
    state,
    dispatch: controller.dispatch,
  };
}
