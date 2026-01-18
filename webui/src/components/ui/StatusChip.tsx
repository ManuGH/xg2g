// StatusChip Primitive - Broadcast Console 2026
// CTO Contract: Single source of truth for all status indicators


import './StatusChip.css';

export type ChipState = 'idle' | 'success' | 'warning' | 'error' | 'live' | 'recording';

export interface StatusChipProps {
  state: ChipState;
  label: string;
  showIcon?: boolean;
  className?: string;
}

// Icon mapping - CTO Contract: Unicode only, no emojis
const StateIcons: Record<ChipState, string> = {
  idle: '○',         // U+25CB - Empty circle
  success: '✓',      // U+2713 - Check mark
  warning: '⚠',      // U+26A0 - Warning sign
  error: '✗',        // U+2717 - X mark
  live: '●',         // U+25CF - Filled circle
  recording: '●'     // U+25CF - Filled circle
};

export function StatusChip({
  state,
  label,
  showIcon = true,
  className = ''
}: StatusChipProps) {
  return (
    <span
      className={`status-chip ${className}`.trim()}
      data-state={state}
      role="status"
      aria-label={`${label} - ${state}`}
    >
      {showIcon && (
        <span className="status-chip__icon" aria-hidden="true">
          {StateIcons[state]}
        </span>
      )}
      <span className="status-chip__label">{label}</span>
    </span>
  );
}
