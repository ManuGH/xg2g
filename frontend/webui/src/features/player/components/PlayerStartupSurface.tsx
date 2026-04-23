import { Button, StatusChip } from '../../../components/ui';
import type { ChipState } from '../../../components/ui/StatusChip';

import { PlayerRuntimeMeta } from './PlayerRuntimeMeta';
import styles from './V3Player.module.css';

export interface PlayerStartupSurfaceProps {
  show: boolean;
  isRecordingStartupSurface: boolean;
  useNativeBufferingSafeOverlay: boolean;
  startupTitle: string;
  startupEyebrow: string;
  startupStatusState: ChipState;
  startupStatusLabel: string;
  spinnerLabel: string;
  spinnerSupport: string;
  startupElapsedLabel: string;
  showRuntimePolicyMeta: boolean;
  runtimePolicyPhase: string | null | undefined;
  runtimePolicyPhaseState: ChipState;
  runtimePolicyPhaseLabel: string;
  runtimePolicyMetaHint: string | null;
  useMinimalStartupChrome: boolean;
  showStopAction: boolean;
  stopLabel: string;
  onStop: () => void;
}

export function PlayerStartupSurface({
  show,
  isRecordingStartupSurface,
  useNativeBufferingSafeOverlay,
  startupTitle,
  startupEyebrow,
  startupStatusState,
  startupStatusLabel,
  spinnerLabel,
  spinnerSupport,
  startupElapsedLabel,
  showRuntimePolicyMeta,
  runtimePolicyPhase,
  runtimePolicyPhaseState,
  runtimePolicyPhaseLabel,
  runtimePolicyMetaHint,
  useMinimalStartupChrome,
  showStopAction,
  stopLabel,
  onStop,
}: PlayerStartupSurfaceProps) {
  if (!show) {
    return null;
  }

  return (
    <div
      className={[
        styles.spinnerOverlay,
        isRecordingStartupSurface ? styles.spinnerOverlayRecording : null,
        useNativeBufferingSafeOverlay ? styles.spinnerOverlaySafe : null,
      ].filter(Boolean).join(' ')}
      aria-live="polite"
    >
      <div
        className={[
          styles.spinnerBadge,
          isRecordingStartupSurface ? styles.spinnerBadgeRecording : null,
        ].filter(Boolean).join(' ')}
      >
        {isRecordingStartupSurface ? (
          <svg
            className={styles.spinnerMediaIcon}
            viewBox="0 0 48 48"
            aria-hidden="true"
            focusable="false"
          >
            <path d="M15 10.8c0-1.6 1.7-2.6 3.1-1.8l19.2 11.2c1.4.8 1.4 2.8 0 3.6L18.1 35c-1.4.8-3.1-.2-3.1-1.8V10.8Z" />
          </svg>
        ) : (
          <div className={`${styles.spinner} spinner-base`}></div>
        )}
      </div>
      <div className={styles.spinnerContent}>
        {!isRecordingStartupSurface && (
          <div className={styles.spinnerEyebrow}>{startupEyebrow}</div>
        )}
        {startupTitle && <h2 className={styles.spinnerTitle}>{startupTitle}</h2>}
        {!isRecordingStartupSurface && (
          <div className={styles.spinnerStatusRow}>
            <StatusChip
              state={startupStatusState}
              label={startupStatusLabel}
            />
            <PlayerRuntimeMeta
              show={showRuntimePolicyMeta}
              phase={runtimePolicyPhase}
              phaseState={runtimePolicyPhaseState}
              phaseLabel={runtimePolicyPhaseLabel}
              hint={runtimePolicyMetaHint}
              variant="startup"
            />
          </div>
        )}
        <div className={styles.spinnerLabel}>{spinnerLabel}</div>
        {!isRecordingStartupSurface && (
          <>
            <div className={styles.spinnerSupport}>{spinnerSupport}</div>
            <div className={styles.spinnerMeta}>
              <div className={styles.spinnerProgressTrack} aria-hidden="true">
                <div className={`${styles.spinnerProgressFill} animate-startup-progress`}></div>
              </div>
              <div className={styles.spinnerElapsed}>{startupElapsedLabel}</div>
            </div>
          </>
        )}
        {useMinimalStartupChrome && showStopAction && (
          <div className={styles.spinnerActions}>
            <Button variant="danger" size="sm" onClick={onStop}>
              ⏹ {stopLabel}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
