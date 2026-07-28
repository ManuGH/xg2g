import { useCallback, useRef, useState } from 'react';
import type { Dispatch } from 'react';
import { runPlaybackMachine } from './playbackMachine';
import type {
  PlaybackCommand,
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';

export type PlaybackCommandExecutor = (command: PlaybackCommand) => void;

// Replaces useReducer(playbackMachine, ...) with a render-independent event
// loop. The machine runs inside dispatch — a plain callback, never the render
// phase — so React StrictMode's double-invocation of render/reducers cannot
// re-run it: each dispatched event computes the transition once and executes
// its commands exactly once, synchronously.
//
// The executor is read through a ref so callers may pass a fresh closure every
// render without destabilizing dispatch's identity.
export function usePlaybackMachineRuntime(
  createInitialState: () => PlaybackDomainState,
  executeCommand: PlaybackCommandExecutor,
): [PlaybackDomainState, Dispatch<PlaybackMachineEvent>] {
  const [state, setState] = useState(createInitialState);
  const stateRef = useRef(state);
  const executorRef = useRef(executeCommand);
  executorRef.current = executeCommand;

  const dispatch = useCallback((event: PlaybackMachineEvent) => {
    const { state: nextState, commands } = runPlaybackMachine(stateRef.current, event);
    if (nextState !== stateRef.current) {
      stateRef.current = nextState;
      setState(nextState);
    }
    for (const command of commands) {
      executorRef.current(command);
    }
  }, []);

  return [state, dispatch];
}
