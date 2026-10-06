// StatusChip Primitive - Broadcast Console 2026
// CTO Contract: Single source of truth for all status indicators

import { useTranslation } from 'react-i18next';
import styles from './StatusChip.module.css';

export type ChipState = 'idle' | 'success' | 'warning' | 'error' | 'live' | 'recording' | 'pending' | 'deleting';

export interface StatusChipProps {
  state: ChipState;
  label: string;
  showIcon?: boolean;
  className?: string;
  ariaLabel?: string;
}

// Icon mapping - CTO Contract: Unicode only, no emojis
const StateIcons: Partial<Record<ChipState, string>> = {
  idle: '○',         // U+25CB - Empty circle
  success: '✓',      // U+2713 - Check mark
  warning: '⚠',      // U+26A0 - Warning sign
  error: '✗',        // U+2717 - X mark
  live: '●',         // U+25CF - Filled circle
  recording: '●'     // U+25CF - Filled circle
};

export function computeAccessibleChipName(label: string, _state: ChipState, localizedState: string): string {
  return `${label} – ${localizedState}`;
}

export function StatusChip({
  state,
  label,
  showIcon = true,
  className = '',
  ariaLabel,
}: StatusChipProps) {
  const { t } = useTranslation();
  const localizedState = t(`statusChip.state.${state}`);

  const accessibleName = ariaLabel ?? computeAccessibleChipName(label, state, localizedState);

  return (
    <span
      className={[styles.chip, className].filter(Boolean).join(' ')}
      data-state={state}
      role="status"
      aria-label={accessibleName}
    >
      {showIcon && (
        <span className={styles.icon} aria-hidden="true">
          {StateIcons[state]}
        </span>
      )}
      <span className={styles.label}>{label}</span>
    </span>
  );
}
