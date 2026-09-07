// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import {
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
import type {
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';

export interface UsePlaybackControllerOptions {
  startSettlementTimeoutMs?: number;
  httpRequestTimeoutMs?: number;
  stopRequestTimeoutMs?: number;
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
// - useInsertionEffect manages the true component mount/unmount lifecycle:
//   * StrictMode mount simulation (Setup 1 -> Cleanup 1 -> Setup 2) only re-runs layout
//     and passive effects; insertion effects remain active.
//   * Suspense hiding preserves insertion effects, avoiding premature disposal.
//   * On real unmount, insertion effect cleanup runs synchronously, disconnecting
//     the executor and calling controller.dispose().
//   * useLayoutEffect setup re-connects the executor on mount and when revealed from Suspense.
export function usePlaybackController(
  transport: LiveSessionTransport,
  createInitialState: () => PlaybackDomainState,
  executeCommand: PlaybackCommandExecutor,
  options?: UsePlaybackControllerOptions,
): UsePlaybackControllerResult {
  const executorRef = useRef<PlaybackCommandExecutor | null>(executeCommand);
  executorRef.current = executeCommand;

  const isConnectedRef = useRef(true);

  const [controller] = useState(() =>
    createPlaybackController({
      transport,
      createInitialState,
      executeCommand: (command) => {
        if (isConnectedRef.current && executorRef.current) {
          executorRef.current(command);
        }
      },
      startSettlementTimeoutMs: options?.startSettlementTimeoutMs,
      httpRequestTimeoutMs: options?.httpRequestTimeoutMs,
      stopRequestTimeoutMs: options?.stopRequestTimeoutMs,
    }),
  );

  useInsertionEffect(() => {
    isConnectedRef.current = true;
    controller.setCommandExecutor((command) => {
      if (isConnectedRef.current && executorRef.current) {
        executorRef.current(command);
      }
    });

    return () => {
      isConnectedRef.current = false;
      controller.setCommandExecutor(null);
      controller.dispose();
    };
  }, [controller]);

  useLayoutEffect(() => {
    isConnectedRef.current = true;
    controller.setCommandExecutor((command) => {
      if (isConnectedRef.current && executorRef.current) {
        executorRef.current(command);
      }
    });
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
