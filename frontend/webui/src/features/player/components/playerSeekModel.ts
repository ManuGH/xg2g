export type SeekKeyAction =
  | { type: 'seekBy'; seconds: number }
  | { type: 'seekToStart' }
  | { type: 'seekToEnd' };

export interface ResolveSeekTargetFromPointerInput {
  clientX: number;
  trackLeft: number;
  trackWidth: number;
  seekableStart: number;
  windowDuration: number;
}

export function resolveSeekProgressPercent(relativePosition: number, windowDuration: number): string {
  if (windowDuration <= 0) {
    return '0%';
  }

  return `${Math.min(100, Math.max(0, (relativePosition / windowDuration) * 100))}%`;
}

export function resolveSeekTargetFromPointer({
  clientX,
  trackLeft,
  trackWidth,
  seekableStart,
  windowDuration,
}: ResolveSeekTargetFromPointerInput): number | null {
  if (windowDuration <= 0 || trackWidth <= 0) {
    return null;
  }

  const ratio = Math.min(1, Math.max(0, (clientX - trackLeft) / trackWidth));
  return seekableStart + ratio * windowDuration;
}

export function resolveSeekKeyAction(key: string, windowDuration: number): SeekKeyAction | null {
  if (windowDuration <= 0) {
    return null;
  }

  switch (key) {
    case 'ArrowLeft':
      return { type: 'seekBy', seconds: -15 };
    case 'ArrowRight':
      return { type: 'seekBy', seconds: 15 };
    case 'Home':
      return { type: 'seekToStart' };
    case 'End':
      return { type: 'seekToEnd' };
    default:
      return null;
  }
}
