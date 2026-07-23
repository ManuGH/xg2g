import { describe, it, expect, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useHlsTransport } from './useHlsTransport';

describe('useHlsTransport', () => {
  it('initializes with empty tracks and null ref', () => {
    const { result } = renderHook(() => useHlsTransport());
    expect(result.current.hlsRef.current).toBeNull();
    expect(result.current.audioTracks).toEqual([]);
    expect(result.current.activeAudioTrack).toBe(-1);
  });

  it('destroys current HLS instance cleanly', () => {
    const { result } = renderHook(() => useHlsTransport());
    const mockDestroy = vi.fn();
    (result.current.hlsRef as any).current = { destroy: mockDestroy };

    act(() => {
      result.current.destroyHls();
    });

    expect(mockDestroy).toHaveBeenCalled();
    expect(result.current.hlsRef.current).toBeNull();
  });
});
