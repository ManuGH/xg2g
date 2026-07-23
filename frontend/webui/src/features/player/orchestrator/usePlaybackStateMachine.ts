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

  const isIdle = state.sessionPhase === 'idle';
  const isProbing = state.sessionPhase === 'starting' || state.mediaPhase === 'starting';
  const isPlaying = state.mediaPhase === 'playing';
  const isRetrying = state.mediaPhase === 'recovering';
  const isError = state.sessionPhase === 'error' || state.mediaPhase === 'error';

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
