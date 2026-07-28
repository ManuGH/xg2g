import type { ReactNode } from 'react';

import { Card, StatusChip } from '../../../components/ui';
import type { ChipState } from '../../../components/ui/StatusChip';

import styles from './V3Player.module.css';

type RuntimeMetaVariant = 'default' | 'startup' | 'error';

export interface PlayerRuntimeMetaProps {
  show: boolean;
  phase: string | null | undefined;
  phaseState: ChipState;
  phaseLabel: string;
  hint?: string | null;
  theater?: boolean;
  variant?: RuntimeMetaVariant;
}

export interface PlayerRuntimeMetaPanelRow {
  key: string;
  label: string;
  value: ReactNode;
}

export interface PlayerRuntimeMetaPanelProps {
  show: boolean;
  title: string;
  rows: PlayerRuntimeMetaPanelRow[];
  actions?: ReactNode;
}

export function PlayerRuntimeMeta({
  show,
  phase,
  phaseState,
  phaseLabel,
  hint,
  theater = false,
  variant = 'default',
}: PlayerRuntimeMetaProps) {
  if (!show) {
    return null;
  }

  return (
    <div
      className={[
        styles.runtimePolicyMeta,
        theater ? styles.runtimePolicyMetaTheater : null,
        variant === 'startup' ? styles.runtimePolicyMetaStartup : null, // display-only: visual variant, not playback policy.
        variant === 'error' ? styles.runtimePolicyMetaError : null, // display-only: visual variant, not playback policy.
        phase === 'probing' ? 'animate-runtime-policy-probing' : null, // display-only: CSS animation for reported phase.
        phase === 'probe_regressed' ? 'animate-runtime-policy-alert' : null, // display-only: CSS animation for reported phase.
      ].filter(Boolean).join(' ')}
      data-phase={phase ?? 'unknown'}
    >
      <StatusChip
        state={phaseState}
        label={phaseLabel}
        className={styles.runtimePolicyMetaChip}
      />
      {hint ? (
        <span className={styles.runtimePolicyMetaHint}>{hint}</span>
      ) : null}
    </div>
  );
}

export function PlayerRuntimeMetaPanel({
  show,
  title,
  rows,
  actions,
}: PlayerRuntimeMetaPanelProps) {
  if (!show) {
    return null;
  }

  return (
    <div className={styles.statsOverlay}>
      <Card variant="standard">
        <Card.Header className={styles.statsHeader}>
          <div className={styles.statsHeaderCopy}>
            <Card.Title>{title}</Card.Title>
          </div>
          {actions ? (
            <div className={styles.statsHeaderActions}>
              {actions}
            </div>
          ) : null}
        </Card.Header>
        <Card.Content className={styles.statsGrid}>
          {rows.map((row) => (
            <div key={row.key} className={styles.statsRow}>
              <span className={styles.statsLabel}>{row.label}</span>
              <div className={styles.statsValue}>{row.value}</div>
            </div>
          ))}
        </Card.Content>
      </Card>
    </div>
  );
}
