import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useNativeTransport } from './useNativeTransport';

describe('useNativeTransport', () => {
  it('initializes with null videoRef and empty audio tracks', () => {
    const { result } = renderHook(() => useNativeTransport());

    expect(result.current.videoRef.current).toBeNull();
    expect(result.current.nativeAudioTracks).toEqual([]);
    expect(result.current.activeNativeTrack).toBe(-1);
  });

  it('binds native audio tracks if video element has audioTracks property', () => {
    const { result } = renderHook(() => useNativeTransport());
    const dummyVideo = document.createElement('video');
    (dummyVideo as any).audioTracks = [
      { label: 'English', language: 'en', enabled: true },
      { label: 'German', language: 'de', enabled: false },
    ];

    act(() => {
      result.current.bindNativeAudioTracks(dummyVideo);
    });

    expect(result.current.nativeAudioTracks.length).toBe(2);
    expect(result.current.activeNativeTrack).toBe(0);
  });
});
