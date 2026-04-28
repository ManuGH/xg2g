import type { TFunction } from 'i18next';
import { describe, expect, it } from 'vitest';

import type { BuildPlayerStartupUiModelInput } from './playerStartupUiModel';
import { buildPlayerStartupUiModel } from './playerStartupUiModel';

const t = ((key: string, options?: Record<string, unknown>) => {
  const messages: Record<string, string> = {
    'player.preparingDirectPlay': 'Preparing DirectPlay',
    'player.recordingFallbackTitle': 'Recording',
    'player.recordingStartupStatus': 'Opening',
    'player.recordingStartupSupport': 'Preparing the source. Playback will start shortly.',
    'player.runtimePolicyPhases.probing': 'Probing',
    'player.runtimePolicySupport.startup.probing': 'Testing {{profile}} briefly.',
    'player.runtimePolicySupport.error.probing': 'The player was validating {{profile}}.',
    'player.statusStates.buffering': 'Buffering',
    'player.startupSupport.default': 'Playback starts automatically as soon as the first stable segments are ready.',
  };
  const message = String(options?.defaultValue ?? messages[key] ?? key);
  return message.replace('{{profile}}', String(options?.profile ?? ''));
}) as unknown as TFunction;

const baseInput: BuildPlayerStartupUiModelInput = {
  t,
  status: 'starting',
  overlayStatus: 'starting',
  isImmediateStartupStatus: true,
  showBufferingOverlay: false,
  shouldHoldNativeVideo: false,
  showNativeVideoVeil: false,
  isNativeEngine: false,
  hostIsTv: false,
  isFullscreen: false,
  useOverlayShell: false,
  isRecordingPageLayout: false,
  recordingId: null,
  activeRecordingId: null,
  activeRecordingRefCurrent: null,
  channelName: 'Das Erste HD',
  normalizedRecordingTitle: '',
  playbackMode: 'LIVE',
  vodStreamMode: null,
  sessionProfileReason: null,
  runtimePolicyPhase: null,
  runtimeProbeCandidate: null,
  operatorMaxQualityRung: null,
};

function build(overrides: Partial<BuildPlayerStartupUiModelInput> = {}) {
  return buildPlayerStartupUiModel({
    ...baseInput,
    ...overrides,
  });
}

describe('buildPlayerStartupUiModel', () => {
  it('keeps live startup chrome visible for non-TV inline playback', () => {
    const model = build();

    expect(model.startupTitle).toBe('Das Erste HD');
    expect(model.showStartupOverlay).toBe(true);
    expect(model.showPlaybackChrome).toBe(true);
    expect(model.useMinimalStartupChrome).toBe(false);
  });

  it('uses minimal startup chrome on TV or overlay surfaces', () => {
    expect(build({ hostIsTv: true }).useMinimalStartupChrome).toBe(true);
    expect(build({ useOverlayShell: true }).useMinimalStartupChrome).toBe(true);
  });

  it('uses recording-specific startup copy and watch layout outside fullscreen', () => {
    const model = build({
      channelName: null,
      recordingId: 'rec-1',
      normalizedRecordingTitle: '',
      playbackMode: 'VOD',
      overlayStatus: 'buffering',
      useOverlayShell: true,
      vodStreamMode: 'direct_mp4',
    });

    expect(model.isRecordingStartupSurface).toBe(true);
    expect(model.startupTitle).toBe('Recording');
    expect(model.spinnerLabel).toBe('Preparing DirectPlay');
    expect(model.showRecordingWatchLayout).toBe(true);
    expect(build({ recordingId: 'rec-1', useOverlayShell: true, isFullscreen: true }).showRecordingWatchLayout).toBe(false);
  });

  it('derives runtime policy labels and native buffering masks', () => {
    const model = build({
      status: 'buffering',
      overlayStatus: 'buffering',
      shouldHoldNativeVideo: true,
      isNativeEngine: false,
      runtimePolicyPhase: 'probing',
      runtimeProbeCandidate: 'quality_audio_aac_320_stereo',
    });

    expect(model.showRuntimePolicyMeta).toBe(true);
    expect(model.runtimePolicyPhaseLabel).toBe('Probing');
    expect(model.runtimePolicyPhaseState).toBe('pending');
    expect(model.runtimePolicyMetaHintLabel).toBe('quality audio aac 320 stereo');
    expect(model.showNativeBufferingMask).toBe(true);
    expect(model.hideVideoElement).toBe(true);
  });
});
