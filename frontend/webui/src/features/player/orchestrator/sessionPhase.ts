import type { V3SessionSnapshot } from '../../../types/v3-player';
import type { SessionPhase } from './playbackTypes';

export function resolveSessionPhaseFromState(state: V3SessionSnapshot['state'] | undefined): SessionPhase | null {
  switch (state) {
    case 'STARTING':
    case 'IDLE':
    case 'PRIMING':
      return 'starting';
    case 'READY':
    case 'DRAINING':
      return 'ready';
    case 'STOPPING':
    case 'STOPPED':
    case 'CANCELLED':
      return 'stopped';
    case 'FAILED':
      return 'error';
    default:
      return null;
  }
}
