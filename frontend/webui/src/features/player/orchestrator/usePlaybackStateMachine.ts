import { useMemo } from 'react';
import type { Dispatch } from 'react';
import { usePlaybackMachineRuntime } from './usePlaybackMachineRuntime';
import type {
  PlaybackCommand,
  PlaybackDomainState,
  PlaybackMachineEvent,
} from './playbackTypes';
import { createInitialPlaybackDomainState } from './playbackMachine';

export type PlaybackCommandExecutor = (command: PlaybackCommand) => void;

export interface PlaybackStateMachineResult {
  state: PlaybackDomainState;
  dispatch: Dispatch<PlaybackMachineEvent>;
  isIdle: boolean;
  isProbing: boolean;
  isPlaying: boolean;
  isRetrying: boolean;
  isError: boolean;
}

// Pure state machine hook for player orchestration per SPEC_MODERNIZATION_2026.md §A3
export function usePlaybackStateMachine(
  executeCommand: PlaybackCommandExecutor,
  initialStateFactory: () => PlaybackDomainState = createInitialPlaybackDomainState,
): PlaybackStateMachineResult {
  const [state, dispatch] = usePlaybackMachineRuntime(initialStateFactory, executeCommand);

  const isIdle = state.phase.kind === 'idle';
  const isProbing = state.phase.kind === 'probing' || state.phase.kind === 'preflight';
  const isPlaying = state.phase.kind === 'playing' || state.phase.kind === 'active';
  const isRetrying = state.phase.kind === 'reconnecting' || state.phase.kind === 'recovering';
  const isError = state.phase.kind === 'error' || state.phase.kind === 'failed';

  return useMemo(
    () => ({
      state,
      dispatch,
      isIdle,
      isProbing,
      isPlaying,
      isRetrying,
      isError,
    }),
    [state, dispatch, isIdle, isProbing, isPlaying, isRetrying, isError],
  );
}
