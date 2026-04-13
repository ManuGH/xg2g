import type { AppError } from '../../../types/errors';
import type {
  NormalizedAdvisoryWarning,
  NormalizedContractFailure,
  NormalizedContractFailureKind,
} from '../contracts/normalizedPlaybackTypes';

export type PlaybackFailureClass = 'auth' | 'session' | 'contract' | 'media' | 'advisory';
export type PlaybackPolicyImpact = 'none' | 'blocked' | 'degraded';
export type PlaybackFailureSource = 'backend' | 'media-element' | 'orchestrator' | 'adapter' | 'native-host';

export interface PlaybackFailureSemantics {
  class: PlaybackFailureClass;
  code: string;
  message: string;
  terminal: boolean;
  retryable: boolean;
  recoverable: boolean;
  userVisible: boolean;
  policyImpact: PlaybackPolicyImpact;
}

export interface PlaybackFailureInput {
  appError?: AppError | null;
  code?: string | null;
  failureClass?: PlaybackFailureClass;
  source: PlaybackFailureSource;
  message?: string | null;
  policyImpact?: PlaybackPolicyImpact;
  recoverable?: boolean;
  retryable?: boolean;
  terminal?: boolean;
  userVisible?: boolean;
}

export interface PlaybackFailureReportOptions
  extends Omit<PlaybackFailureInput, 'appError' | 'message' | 'source'> {
  source?: PlaybackFailureSource;
  telemetryContext?: string | null;
  telemetryReason?: string | null;
}

export interface PlaybackAdvisorySignal {
  class: 'advisory';
  code: string;
  message: string;
  source: 'backend' | 'normalizer';
  terminal: false;
  retryable: false;
  recoverable: false;
  userVisible: boolean;
  policyImpact: 'none';
}

function fallbackCodeForStatus(status: number | undefined, source: PlaybackFailureSource): string | null {
  switch (status) {
    case 401:
      return 'AUTH_REQUIRED';
    case 403:
      return 'AUTH_FORBIDDEN';
    case 404:
      return source === 'native-host' ? 'SESSION_NOT_FOUND' : 'NOT_FOUND';
    case 409:
      return 'SESSION_CONFLICT';
    case 410:
      return 'SESSION_EXPIRED';
    case 429:
      return 'RATE_LIMITED';
    case 503:
      return 'UNAVAILABLE';
    default:
      return null;
  }
}

function inferFailureClass({
  appError,
  failureClass,
  source,
}: PlaybackFailureInput): PlaybackFailureClass {
  if (failureClass) {
    return failureClass;
  }

  const status = appError?.status;
  if (status === 401 || status === 403) {
    return 'auth';
  }
  if (status === 404 || status === 409 || status === 410) {
    return source === 'media-element' ? 'media' : 'session';
  }
  if (source === 'media-element') {
    return 'media';
  }
  if (source === 'native-host') {
    return 'session';
  }
  return 'contract';
}

function defaultRetryable(
  failureClass: PlaybackFailureClass,
  appError: AppError | null | undefined,
): boolean {
  if (typeof appError?.retryable === 'boolean') {
    return appError.retryable;
  }

  switch (failureClass) {
    case 'auth':
      return false;
    case 'session':
      return true;
    case 'contract':
      return false;
    case 'media':
      return true;
    case 'advisory':
      return false;
    default:
      return false;
  }
}

function defaultRecoverable(
  failureClass: PlaybackFailureClass,
  retryable: boolean,
): boolean {
  switch (failureClass) {
    case 'session':
      return true;
    case 'media':
      return retryable;
    default:
      return false;
  }
}

function defaultTerminal(
  failureClass: PlaybackFailureClass,
  retryable: boolean,
  recoverable: boolean,
): boolean {
  if (failureClass === 'advisory') {
    return false;
  }
  if (failureClass === 'auth') {
    return true;
  }
  return !retryable && !recoverable;
}

function defaultPolicyImpact(failureClass: PlaybackFailureClass): PlaybackPolicyImpact {
  return failureClass === 'advisory' ? 'none' : 'blocked';
}

export function classifyPlaybackFailure(input: PlaybackFailureInput): PlaybackFailureSemantics {
  const failureClass = inferFailureClass(input);
  const retryable = input.retryable ?? defaultRetryable(failureClass, input.appError);
  const recoverable = input.recoverable ?? defaultRecoverable(failureClass, retryable);
  const terminal = input.terminal ?? defaultTerminal(failureClass, retryable, recoverable);
  const code =
    input.code ??
    input.appError?.code ??
    fallbackCodeForStatus(input.appError?.status, input.source) ??
    'PLAYBACK_FAILURE';
  const message = input.message ?? input.appError?.title ?? 'Playback failed';

  if (failureClass === 'advisory') {
    return {
      class: 'advisory',
      code,
      message,
      retryable: false,
      recoverable: false,
      terminal: false,
      userVisible: input.userVisible ?? false,
      policyImpact: 'none',
    };
  }

  return {
    class: failureClass,
    code,
    message,
    retryable,
    recoverable,
    terminal,
    userVisible: input.userVisible ?? true,
    policyImpact: input.policyImpact ?? defaultPolicyImpact(failureClass),
  };
}

export function mapNormalizedContractFailureClass(
  kind: NormalizedContractFailureKind,
): PlaybackFailureClass {
  switch (kind) {
    case 'auth':
      return 'auth';
    case 'session':
      return 'session';
    case 'contract':
    case 'unavailable':
    case 'unsupported':
    default:
      return 'contract';
  }
}

export function classifyNormalizedContractFailure(
  failure: NormalizedContractFailure,
): PlaybackFailureSemantics {
  return classifyPlaybackFailure({
    appError: {
      title: failure.message,
      retryable: failure.retryable,
      code: failure.code,
    },
    failureClass: mapNormalizedContractFailureClass(failure.kind),
    source: 'backend',
    code: failure.code,
    message: failure.message,
    retryable: failure.retryable,
    terminal: failure.terminal,
    recoverable: false,
    policyImpact: 'blocked',
    userVisible: true,
  });
}

export function buildPlaybackAdvisorySignal(
  warning: NormalizedAdvisoryWarning,
): PlaybackAdvisorySignal {
  return {
    class: 'advisory',
    code: warning.code,
    message: warning.message,
    source: warning.source,
    terminal: false,
    retryable: false,
    recoverable: false,
    userVisible: false,
    policyImpact: 'none',
  };
}
