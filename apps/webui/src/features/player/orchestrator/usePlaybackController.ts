// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
  useEffect,
  useInsertionEffect,
  useLayoutEffect,
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
import type { V3SessionStatusResponse } from '../../../types/v3-player';
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
  onSessionSnapshot?: (snapshot: V3SessionStatusResponse) => void;
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
// - useInsertionEffect manages the true component mount/unmount lifecycle for the executor wiring:
//   * Setup connects the executor delegate synchronously.
//   * StrictMode mount simulation (Setup 1 -> Cleanup 1 -> Setup 2) only re-runs layout
//     and passive effects; insertion effects remain active so child layout effects have
//     a valid executor in Setup 2.
//   * On real unmount, insertion effect cleanup runs, disconnecting the executor so post-unmount
//     dispatches drop commands.
// - useLayoutEffect re-connects the executor on mount and when revealed from Suspense.
// - useEffect manages the controller active/disposed lifecycle:
//   * Setup calls controller.activate() (clears isDisposed).
//   * Cleanup calls controller.dispose() (cancels in-flight starts and reaps sessions).
export function usePlaybackController(
  transport: LiveSessionTransport,
  createInitialState: () => PlaybackDomainState,
  executeCommand: PlaybackCommandExecutor,
  options?: UsePlaybackControllerOptions,
): UsePlaybackControllerResult {
  const committedTransportRef = useRef<LiveSessionTransport>(transport);

  const optionsRef = useRef<UsePlaybackControllerOptions | undefined>(options);
  optionsRef.current = options;

  const committedExecutorRef = useRef<PlaybackCommandExecutor | null>(executeCommand);

  const [controller] = useState(() =>
    createPlaybackController({
      transport,
      getTransport: () => committedTransportRef.current,
      createInitialState,
      executeCommand: (command) => {
        return committedExecutorRef.current?.(command);
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
      onSessionSnapshot: (snapshot) => {
        optionsRef.current?.onSessionSnapshot?.(snapshot);
      },
    }),
  );

  useInsertionEffect(() => {
    committedExecutorRef.current = executeCommand;
  });

  useInsertionEffect(() => {
    controller.setCommandExecutor((command) => committedExecutorRef.current?.(command));
    return () => {
      controller.setCommandExecutor(null);
    };
  }, [controller]);

  useLayoutEffect(() => {
    committedTransportRef.current = transport;
    controller.updateTransport(transport);
    controller.setCommandExecutor((command) => committedExecutorRef.current?.(command));
  }, [controller, transport]);

  useEffect(() => {
    controller.activate();

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
