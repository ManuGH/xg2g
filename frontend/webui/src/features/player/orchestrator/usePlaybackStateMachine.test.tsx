import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { usePlaybackStateMachine } from './usePlaybackStateMachine';

describe('usePlaybackStateMachine', () => {
  it('initializes in idle state by default', () => {
    const executor = vi.fn();
    const { result } = renderHook(() => usePlaybackStateMachine(executor));

    expect(result.current.isIdle).toBe(true);
    expect(result.current.isPlaying).toBe(false);
    expect(result.current.isError).toBe(false);
  });

  it('dispatches events and invokes command executor', () => {
    const executor = vi.fn();
    const { result } = renderHook(() => usePlaybackStateMachine(executor));

    act(() => {
      result.current.dispatch({
        type: 'intent.start.requested',
        epoch: 1,
        kind: 'live',
        serviceRef: '1:0:1:0:0:0:0:0:0:0:http://test.ts',
      });
    });

    expect(result.current.state.epoch.session).toBeDefined();
    expect(executor).toHaveBeenCalled();
  });
});
