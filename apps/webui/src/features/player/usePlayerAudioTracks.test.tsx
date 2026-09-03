import { describe, expect, it } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { usePlayerAudioTracks, areAudioTrackListsEqual } from './usePlayerAudioTracks';
import type { PlayerAudioTrack } from '../../types/v3-player';

describe('usePlayerAudioTracks', () => {
  it('deduplicates equal audio track lists', () => {
    const trackA: PlayerAudioTrack = {
      id: 0,
      name: 'German',
      language: 'de',
      key: 'hls-0',
      engineIndex: 0,
    };
    const trackB: PlayerAudioTrack = {
      id: 0,
      name: 'German',
      language: 'de',
      key: 'hls-0',
      engineIndex: 0,
    };
    expect(areAudioTrackListsEqual([trackA], [trackB])).toBe(true);

    const trackC: PlayerAudioTrack = {
      id: 1,
      name: 'English',
      language: 'en',
      key: 'hls-1',
      engineIndex: 1,
    };
    expect(areAudioTrackListsEqual([trackA], [trackC])).toBe(false);
  });

  it('updates tracks and changes active track on hls instance', () => {
    const hlsMock = { audioTrack: -1 };
    const videoRef = { current: null };
    const hlsRef = { current: hlsMock as any };

    const { result } = renderHook(() => usePlayerAudioTracks({ videoRef, hlsRef }));

    expect(result.current.audioTracks).toEqual([]);
    expect(result.current.activeAudioTrack).toBe(-1);

    const tracks: PlayerAudioTrack[] = [
      { id: 0, name: 'Stereo', language: 'de', key: 'hls-0', engineIndex: 0 },
      { id: 1, name: 'Surround', language: 'en', key: 'hls-1', engineIndex: 1 },
    ];

    act(() => {
      result.current.handleAudioTracksUpdated(tracks);
    });

    expect(result.current.audioTracks).toEqual(tracks);

    act(() => {
      result.current.changeAudioTrack(1);
    });

    expect(result.current.activeAudioTrack).toBe(1);
    expect(hlsMock.audioTrack).toBe(1);

    act(() => {
      result.current.resetAudioTracks();
    });

    expect(result.current.audioTracks).toEqual([]);
    expect(result.current.activeAudioTrack).toBe(-1);
  });

  it('updates native video element audioTracks when changing track', () => {
    const fakeAudioTracks = [
      { id: 'track-0', language: 'de', label: 'Stereo', enabled: true },
      { id: 'track-1', language: 'en', label: 'Surround', enabled: false },
    ];
    const videoElement = { audioTracks: fakeAudioTracks };
    const videoRef = { current: videoElement as any };
    const hlsRef = { current: null };

    const { result } = renderHook(() => usePlayerAudioTracks({ videoRef, hlsRef }));

    act(() => {
      result.current.changeAudioTrack(1);
    });

    expect(result.current.activeAudioTrack).toBe(1);
    expect(fakeAudioTracks[0]!.enabled).toBe(false);
    expect(fakeAudioTracks[1]!.enabled).toBe(true);
  });
});
