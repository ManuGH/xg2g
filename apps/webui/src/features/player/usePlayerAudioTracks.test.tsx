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

  it('correctly maps divergent identities (logical id !== engineIndex !== nativeId)', () => {
    const complexTrack: PlayerAudioTrack = {
      id: 7,
      engineIndex: 2,
      nativeId: 'eng-surround',
      key: 'audio-de-ac3',
      name: 'Surround AC3',
      language: 'de',
    };

    // 1. HLS engine test
    const hlsMock = { audioTrack: -1 };
    const hlsRef = { current: hlsMock as any };
    const videoRefEmpty = { current: null };

    const { result: hlsHook } = renderHook(() => usePlayerAudioTracks({ videoRef: videoRefEmpty, hlsRef }));
    act(() => {
      hlsHook.current.handleAudioTracksUpdated([complexTrack]);
    });

    // Selecting by logical id (7) routes to engineIndex (2) on HLS.js
    act(() => {
      hlsHook.current.changeAudioTrack(7);
    });
    expect(hlsHook.current.activeAudioTrack).toBe(7);
    expect(hlsMock.audioTrack).toBe(2);

    // 2. Native audioTracks test
    const fakeAudioTracks = [
      { id: 'stereo-default', enabled: true },
      { id: 'other-track', enabled: false },
      { id: 'eng-surround', enabled: false },
    ];
    const videoElement = { audioTracks: fakeAudioTracks };
    const videoRef = { current: videoElement as any };
    const hlsRefEmpty = { current: null };

    const { result: nativeHook } = renderHook(() => usePlayerAudioTracks({ videoRef, hlsRef: hlsRefEmpty }));
    act(() => {
      nativeHook.current.handleAudioTracksUpdated([complexTrack]);
    });

    // Selecting by logical id (7) routes to nativeId 'eng-surround'
    act(() => {
      nativeHook.current.changeAudioTrack(7);
    });
    expect(nativeHook.current.activeAudioTrack).toBe(7);
    expect(fakeAudioTracks[0]!.enabled).toBe(false);
    expect(fakeAudioTracks[1]!.enabled).toBe(false);
    expect(fakeAudioTracks[2]!.enabled).toBe(true);

    // Also supports passing the track object directly
    act(() => {
      nativeHook.current.changeAudioTrack(complexTrack);
    });
    expect(nativeHook.current.activeAudioTrack).toBe(7);
    expect(fakeAudioTracks[2]!.enabled).toBe(true);
  });
});
