import { Button } from '../../../components/ui';
import type { ChipState } from '../../../components/ui/StatusChip';
import type { AppError } from '../../../types/errors';

import { PlayerRuntimeMeta } from './PlayerRuntimeMeta';
import styles from './V3Player.module.css';

export interface PlayerErrorSurfaceProps {
  error: AppError | null;
  onRetry: () => void;
  showRuntimePolicyMeta: boolean;
  runtimePolicyPhase: string | null | undefined;
  runtimePolicyPhaseState: ChipState;
  runtimePolicyPhaseLabel: string;
  runtimePolicyMetaHint: string | null;
  runtimePolicyErrorSupport: string;
  showVerboseErrorTelemetry: boolean;
  stopSummary: string;
  hostPressureSummary: string;
  fallbackSummary: string;
  ffmpegPlanSummary: string;
  stopLabel: string;
  hostPressureLabel: string;
  fallbackLabel: string;
  ffmpegPlanLabel: string;
  retryLabel: string;
  showErrorDetails: boolean;
  onToggleDetails: () => void;
  hideDetailsLabel: string;
  showDetailsLabel: string;
  sessionLabel: string;
  sessionValue: string;
}

export function PlayerErrorSurface({
  error,
  onRetry,
  showRuntimePolicyMeta,
  runtimePolicyPhase,
  runtimePolicyPhaseState,
  runtimePolicyPhaseLabel,
  runtimePolicyMetaHint,
  runtimePolicyErrorSupport,
  showVerboseErrorTelemetry,
  stopSummary,
  hostPressureSummary,
  fallbackSummary,
  ffmpegPlanSummary,
  stopLabel,
  hostPressureLabel,
  fallbackLabel,
  ffmpegPlanLabel,
  retryLabel,
  showErrorDetails,
  onToggleDetails,
  hideDetailsLabel,
  showDetailsLabel,
  sessionLabel,
  sessionValue,
}: PlayerErrorSurfaceProps) {
  if (!error) {
    return null;
  }

  const showTelemetry =
    showVerboseErrorTelemetry &&
    (stopSummary !== '-' || fallbackSummary !== '-' || ffmpegPlanSummary !== '-' || hostPressureSummary !== '-');

  return (
    <div className={styles.errorToast} aria-live="polite" role="alert">
      <div className={styles.errorMain}>
        <span className={styles.errorText}>⚠ {error.title}</span>
        {error.retryable ? (
          <Button variant="secondary" size="sm" onClick={onRetry}>
            {retryLabel}
          </Button>
        ) : null}
      </div>
      <PlayerRuntimeMeta
        show={showRuntimePolicyMeta}
        phase={runtimePolicyPhase}
        phaseState={runtimePolicyPhaseState}
        phaseLabel={runtimePolicyPhaseLabel}
        hint={runtimePolicyMetaHint}
        variant="error"
      />
      {runtimePolicyErrorSupport ? (
        <div className={styles.errorSupport}>{runtimePolicyErrorSupport}</div>
      ) : null}
      {showTelemetry && (
        <div className={styles.errorTelemetry}>
          {stopSummary !== '-' && (
            <div className={styles.errorTelemetryRow}>
              <span className={styles.errorTelemetryLabel}>{stopLabel}</span>
              <span className={styles.errorTelemetryValue}>{stopSummary}</span>
            </div>
          )}
          {hostPressureSummary !== '-' && (
            <div className={styles.errorTelemetryRow}>
              <span className={styles.errorTelemetryLabel}>{hostPressureLabel}</span>
              <span className={styles.errorTelemetryValue}>{hostPressureSummary}</span>
            </div>
          )}
          {fallbackSummary !== '-' && (
            <div className={styles.errorTelemetryRow}>
              <span className={styles.errorTelemetryLabel}>{fallbackLabel}</span>
              <span className={styles.errorTelemetryValue}>{fallbackSummary}</span>
            </div>
          )}
          {ffmpegPlanSummary !== '-' && (
            <div className={styles.errorTelemetryRow}>
              <span className={styles.errorTelemetryLabel}>{ffmpegPlanLabel}</span>
              <span className={styles.errorTelemetryValue}>{ffmpegPlanSummary}</span>
            </div>
          )}
        </div>
      )}
      {error.detail && (
        <button
          onClick={onToggleDetails}
          className={styles.errorDetailsButton}
        >
          {showErrorDetails ? hideDetailsLabel : showDetailsLabel}
        </button>
      )}
      {showErrorDetails && error.detail && (
        <div className={styles.errorDetailsContent}>
          <pre className={styles.errorDetailsPre}>{error.detail}</pre>
          <br />
          {sessionLabel}: {sessionValue}
        </div>
      )}
    </div>
  );
}
