import type { PlayerStatus } from '../../../types/v3-player';

type HlsEngine = 'native' | 'hlsjs' | null;

export interface BuildPlayerPlaybackStateModelInput {
  status: PlayerStatus;
  activeHlsEngine: HlsEngine;
  showNativeVideo: boolean;
  isDocumentVisible: boolean;
  hostIsTv: boolean;
  hostSupportsKeepScreenAwake: boolean;
  hasTouchPlaybackInput: boolean;
}

export interface PlayerPlaybackStateModel {
  isImmediateStartupStatus: boolean;
  isNativeEngine: boolean;
  shouldManageVisibilityResume: boolean;
  hasTerminalStatus: boolean;
  shouldKeepHostAwake: boolean;
  shouldHoldNativeVideo: boolean;
  isOverlayStartupStatus: boolean;
  overlayStatus: PlayerStatus;
}

export function isTerminalPlayerStatus(status: PlayerStatus): boolean {
  return status === 'idle' || status === 'error' || status === 'stopped';
}

export function isImmediateStartupPlayerStatus(status: PlayerStatus): boolean {
  return status === 'starting' || status === 'priming' || status === 'building';
}

export function buildPlayerPlaybackStateModel({
  status,
  activeHlsEngine,
  showNativeVideo,
  isDocumentVisible,
  hostIsTv,
  hostSupportsKeepScreenAwake,
  hasTouchPlaybackInput,
}: BuildPlayerPlaybackStateModelInput): PlayerPlaybackStateModel {
  const isImmediateStartupStatus = isImmediateStartupPlayerStatus(status);
  const isNativeEngine = activeHlsEngine === 'native';
  const shouldManageVisibilityResume =
    hostIsTv || (isNativeEngine && hasTouchPlaybackInput);
  const hasTerminalStatus = isTerminalPlayerStatus(status);
  const shouldKeepHostAwake =
    hostSupportsKeepScreenAwake &&
    isDocumentVisible &&
    !hasTerminalStatus &&
    status !== 'paused';
  const shouldHoldNativeVideo =
    isNativeEngine && !showNativeVideo && !hasTerminalStatus;
  const isOverlayStartupStatus =
    isImmediateStartupStatus || status === 'buffering' || shouldHoldNativeVideo;

  return {
    isImmediateStartupStatus,
    isNativeEngine,
    shouldManageVisibilityResume,
    hasTerminalStatus,
    shouldKeepHostAwake,
    shouldHoldNativeVideo,
    isOverlayStartupStatus,
    overlayStatus: shouldHoldNativeVideo ? 'buffering' : status,
  };
}
