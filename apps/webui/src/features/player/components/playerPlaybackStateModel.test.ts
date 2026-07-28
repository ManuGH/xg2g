import { describe, expect, it } from 'vitest';

import {
  buildPlayerPlaybackStateModel,
  isImmediateStartupPlayerStatus,
  isTerminalPlayerStatus,
} from './playerPlaybackStateModel';

describe('playerPlaybackStateModel', () => {
  it('classifies startup and terminal statuses', () => {
    expect(isImmediateStartupPlayerStatus('starting')).toBe(true);
    expect(isImmediateStartupPlayerStatus('priming')).toBe(true);
    expect(isImmediateStartupPlayerStatus('building')).toBe(true);
    expect(isImmediateStartupPlayerStatus('buffering')).toBe(false);

    expect(isTerminalPlayerStatus('idle')).toBe(true);
    expect(isTerminalPlayerStatus('error')).toBe(true);
    expect(isTerminalPlayerStatus('stopped')).toBe(true);
    expect(isTerminalPlayerStatus('paused')).toBe(false);
  });

  it('keeps host awake only for visible non-terminal non-paused playback', () => {
    expect(buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: true,
      hasTouchPlaybackInput: false,
    }).shouldKeepHostAwake).toBe(true);

    expect(buildPlayerPlaybackStateModel({
      status: 'paused',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: true,
      hasTouchPlaybackInput: false,
    }).shouldKeepHostAwake).toBe(false);

    expect(buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: false,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: true,
      hasTouchPlaybackInput: false,
    }).shouldKeepHostAwake).toBe(false);
  });

  it('manages visibility resume for TV hosts and touch native playback', () => {
    expect(buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: true,
      hostSupportsKeepScreenAwake: false,
      hasTouchPlaybackInput: false,
    }).shouldManageVisibilityResume).toBe(true);

    expect(buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'native',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: false,
      hasTouchPlaybackInput: true,
    }).shouldManageVisibilityResume).toBe(true);

    expect(buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: false,
      hasTouchPlaybackInput: true,
    }).shouldManageVisibilityResume).toBe(false);
  });

  it('holds hidden native video behind a buffering overlay until it can render', () => {
    const hiddenNative = buildPlayerPlaybackStateModel({
      status: 'playing',
      activeHlsEngine: 'native',
      showNativeVideo: false,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: true,
      hasTouchPlaybackInput: true,
    });

    expect(hiddenNative.shouldHoldNativeVideo).toBe(true);
    expect(hiddenNative.isOverlayStartupStatus).toBe(true);
    expect(hiddenNative.overlayStatus).toBe('buffering');

    const terminalNative = buildPlayerPlaybackStateModel({
      status: 'error',
      activeHlsEngine: 'native',
      showNativeVideo: false,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: true,
      hasTouchPlaybackInput: true,
    });

    expect(terminalNative.shouldHoldNativeVideo).toBe(false);
    expect(terminalNative.overlayStatus).toBe('error');
  });

  it('treats normal startup and buffering statuses as overlay startup states', () => {
    expect(buildPlayerPlaybackStateModel({
      status: 'starting',
      activeHlsEngine: null,
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: false,
      hasTouchPlaybackInput: false,
    }).isOverlayStartupStatus).toBe(true);

    expect(buildPlayerPlaybackStateModel({
      status: 'buffering',
      activeHlsEngine: 'hlsjs',
      showNativeVideo: true,
      isDocumentVisible: true,
      hostIsTv: false,
      hostSupportsKeepScreenAwake: false,
      hasTouchPlaybackInput: false,
    }).isOverlayStartupStatus).toBe(true);
  });
});
