// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { runPlaybackMachine } from './playbackMachine';
import type {
  PlaybackCommand,
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';

export type PlaybackCommandExecutor = (
  command: PlaybackCommand,
) => unknown;

export interface PlaybackMachineRuntime {
  getState(): PlaybackDomainState;
  dispatch(event: PlaybackMachineEvent): void;
  subscribe(listener: () => void): () => void;
  setCommandExecutor(executor: PlaybackCommandExecutor | null): void;
  waitForCommands(): Promise<void>;
  destroy(): void;
}

export class PlaybackMachineRuntimeInstance implements PlaybackMachineRuntime {
  private state: PlaybackDomainState;
  private executor: PlaybackCommandExecutor | null = null;
  private readonly listeners = new Set<() => void>();
  private readonly pendingCommands = new Set<Promise<unknown>>();
  private destroyed = false;

  constructor(
    createInitialState: () => PlaybackDomainState,
    executor?: PlaybackCommandExecutor | null,
  ) {
    this.state = createInitialState();
    this.executor = executor ?? null;
  }

  getState = (): PlaybackDomainState => {
    return this.state;
  };

  setCommandExecutor = (executor: PlaybackCommandExecutor | null): void => {
    if (this.destroyed) return;
    this.executor = executor;
  };

  dispatch = (event: PlaybackMachineEvent): void => {
    if (this.destroyed) return;

    const previousState = this.state;
    const { state: nextState, commands } = runPlaybackMachine(previousState, event);

    if (nextState !== previousState) {
      this.state = nextState;
      // Snapshot the listeners to prevent re-registration during notification
      // from extending or looping the iteration.
      const snapshot = Array.from(this.listeners);
      for (const listener of snapshot) {
        if (this.destroyed) break;
        if (this.listeners.has(listener)) {
          listener();
        }
      }
    }

    // Commands are executed regardless of state change (e.g. stop intents emit commands
    // while state change is deferred until teardown completes)
    if (this.executor && !this.destroyed) {
      for (const command of commands) {
        if (this.destroyed || !this.executor) break;
        const result = this.executor(command);
        if (result && typeof (result as Promise<unknown>).then === 'function') {
          const promise = result as Promise<unknown>;
          this.pendingCommands.add(promise);
          promise.finally(() => {
            this.pendingCommands.delete(promise);
          });
        }
      }
    }
  };

  waitForCommands = async (): Promise<void> => {
    while (this.pendingCommands.size > 0) {
      await Promise.allSettled(Array.from(this.pendingCommands));
    }
  };

  subscribe = (listener: () => void): (() => void) => {
    if (this.destroyed) return () => {};
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  destroy = (): void => {
    this.destroyed = true;
    this.listeners.clear();
    this.pendingCommands.clear();
    this.executor = null;
  };
}

export function createPlaybackMachineRuntime(
  createInitialState: () => PlaybackDomainState,
  executor?: PlaybackCommandExecutor | null,
): PlaybackMachineRuntime {
  return new PlaybackMachineRuntimeInstance(createInitialState, executor);
}
