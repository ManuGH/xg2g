import type { TFunction } from 'i18next';

import type { VodStreamMode } from '../../../components/v3playerModeBridge';
import type { ChipState } from '../../../components/ui/StatusChip';
import type { PlayerStatus } from '../../../types/v3-player';
import {
  resolveRuntimePolicyErrorSupport,
  resolveRuntimePolicyStartupSupport,
  resolveStartupOverlayLabel,
  resolveStartupOverlaySupport,
} from '../startupOverlayLabel';
import { formatQualityRungLabel } from './playerRuntimeMetaFormat';
import {
  formatRuntimePolicyPhaseLabel,
  resolveRuntimePolicyMetaHint,
  resolveRuntimePolicyPhaseState,
} from './playerRuntimeTraceFormat';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';

export interface BuildPlayerStartupUiModelInput {
  t: TFunction;
  status: PlayerStatus;
  overlayStatus: PlayerStatus;
  isImmediateStartupStatus: boolean;
  showBufferingOverlay: boolean;
  shouldHoldNativeVideo: boolean;
  showNativeVideoVeil: boolean;
  isNativeEngine: boolean;
  hostIsTv: boolean;
  isFullscreen: boolean;
  useOverlayShell: boolean;
  isRecordingPageLayout: boolean;
  recordingId: string | null | undefined;
  activeRecordingId: string | null | undefined;
  activeRecordingRefCurrent: string | null | undefined;
  channelName: string | null | undefined;
  normalizedRecordingTitle: string;
  playbackMode: PlaybackMode;
  vodStreamMode: VodStreamMode | null;
  sessionProfileReason: string | null | undefined;
  runtimePolicyPhase: string | null | undefined;
  runtimeProbeCandidate: string | null | undefined;
  operatorMaxQualityRung: string | null | undefined;
}

export interface PlayerStartupUiModel {
  effectiveOperatorMaxQualityRung: string | null;
  effectiveRuntimePolicyPhase: string | null;
  effectiveRuntimeProbeCandidate: string | null;
  runtimePolicyErrorSupport: string;
  isRecordingStartupSurface: boolean;
  startupTitle: string;
  spinnerLabel: string;
  spinnerSupport: string;
  startupStatusLabel: string;
  showStartupOverlay: boolean;
  useNativeBufferingSafeOverlay: boolean;
  showNativeBufferingMask: boolean;
  hideVideoElement: boolean;
  useMinimalStartupChrome: boolean;
  showPlaybackChrome: boolean;
  showRecordingWatchLayout: boolean;
  recordingWatchTitle: string;
  showRuntimePolicyMeta: boolean;
  runtimePolicyPhaseLabel: string;
  runtimePolicyPhaseState: ChipState;
  runtimePolicyMetaHintLabel: string | null;
}

export function buildPlayerStartupUiModel({
  t,
  status,
  overlayStatus,
  isImmediateStartupStatus,
  showBufferingOverlay,
  shouldHoldNativeVideo,
  showNativeVideoVeil,
  isNativeEngine,
  hostIsTv,
  isFullscreen,
  useOverlayShell,
  isRecordingPageLayout,
  recordingId,
  activeRecordingId,
  activeRecordingRefCurrent,
  channelName,
  normalizedRecordingTitle,
  playbackMode,
  vodStreamMode,
  sessionProfileReason,
  runtimePolicyPhase,
  runtimeProbeCandidate,
  operatorMaxQualityRung,
}: BuildPlayerStartupUiModelInput): PlayerStartupUiModel {
  const effectiveOperatorMaxQualityRung = operatorMaxQualityRung ?? null;
  const effectiveRuntimePolicyPhase = runtimePolicyPhase ?? null;
  const effectiveRuntimeProbeCandidate = runtimeProbeCandidate ?? null;
  const runtimePolicyMetaHint = resolveRuntimePolicyMetaHint(
    effectiveRuntimePolicyPhase,
    effectiveRuntimeProbeCandidate,
    effectiveOperatorMaxQualityRung,
  );
  const runtimePolicyCopyHint = runtimePolicyMetaHint
    ? formatQualityRungLabel(runtimePolicyMetaHint)
    : null;
  const runtimePolicyStartupSupport = resolveRuntimePolicyStartupSupport(
    effectiveRuntimePolicyPhase,
    runtimePolicyCopyHint,
    t,
  );
  const runtimePolicyErrorSupport = resolveRuntimePolicyErrorSupport(
    effectiveRuntimePolicyPhase,
    runtimePolicyCopyHint,
    t,
  );
  const isRecordingStartupSurface = Boolean(recordingId || activeRecordingId || activeRecordingRefCurrent);
  const startupTitle = channelName || normalizedRecordingTitle || (isRecordingStartupSurface
    ? t('player.recordingFallbackTitle', { defaultValue: 'Recording' })
    : '');
  const isOverlayStartupStatus = (
    overlayStatus === 'starting' ||
    overlayStatus === 'priming' ||
    overlayStatus === 'buffering' ||
    overlayStatus === 'building'
  );
  const spinnerLabel =
    isOverlayStartupStatus
      ? isRecordingStartupSurface
        ? (overlayStatus === 'buffering' && playbackMode === 'VOD' && vodStreamMode === 'direct_mp4')
          ? t('player.preparingDirectPlay')
          : t('player.preparingRecordingPlayback', { defaultValue: 'Opening recording\u2026' })
        : resolveStartupOverlayLabel(
            overlayStatus,
            `${t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus })}\u2026`,
            sessionProfileReason,
            t,
          )
      : '';
  const spinnerSupport =
    isOverlayStartupStatus
      ? isRecordingStartupSurface
        ? t('player.recordingStartupSupport', { defaultValue: 'Preparing the source. Playback will start shortly.' })
        : runtimePolicyStartupSupport || resolveStartupOverlaySupport(sessionProfileReason, t)
      : '';
  const startupStatusLabel = isRecordingStartupSurface
    ? t('player.recordingStartupStatus', { defaultValue: 'Opening' })
    : t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus });
  const showStartupOverlay =
    isImmediateStartupStatus ||
    (status === 'buffering' && showBufferingOverlay) ||
    shouldHoldNativeVideo;
  const useNativeBufferingSafeOverlay = shouldHoldNativeVideo;
  const showNativeBufferingMask =
    (shouldHoldNativeVideo || showNativeVideoVeil) &&
    !(isNativeEngine && status === 'playing');
  const hideVideoElement = showNativeBufferingMask && !isNativeEngine;
  const useMinimalStartupChrome = showStartupOverlay && (hostIsTv || useOverlayShell || isRecordingPageLayout);
  const showPlaybackChrome = !useMinimalStartupChrome;
  const showRecordingWatchLayout = Boolean(recordingId && !isFullscreen && (useOverlayShell || isRecordingPageLayout));

  return {
    effectiveOperatorMaxQualityRung,
    effectiveRuntimePolicyPhase,
    effectiveRuntimeProbeCandidate,
    runtimePolicyErrorSupport,
    isRecordingStartupSurface,
    startupTitle,
    spinnerLabel,
    spinnerSupport,
    startupStatusLabel,
    showStartupOverlay,
    useNativeBufferingSafeOverlay,
    showNativeBufferingMask,
    hideVideoElement,
    useMinimalStartupChrome,
    showPlaybackChrome,
    showRecordingWatchLayout,
    recordingWatchTitle: startupTitle || t('player.recordingFallbackTitle', { defaultValue: 'Recording' }),
    showRuntimePolicyMeta: Boolean(
      effectiveRuntimePolicyPhase &&
        effectiveRuntimePolicyPhase !== 'stable'
    ),
    runtimePolicyPhaseLabel: t(`player.runtimePolicyPhases.${effectiveRuntimePolicyPhase ?? 'unknown'}`, {
      defaultValue: formatRuntimePolicyPhaseLabel(effectiveRuntimePolicyPhase),
    }),
    runtimePolicyPhaseState: resolveRuntimePolicyPhaseState(effectiveRuntimePolicyPhase),
    runtimePolicyMetaHintLabel: runtimePolicyMetaHint
      ? formatQualityRungLabel(runtimePolicyMetaHint)
      : null,
  };
}
