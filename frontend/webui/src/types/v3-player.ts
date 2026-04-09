// Type definitions for V3 Player component

import type Hls from 'hls.js';
import type {
  IntentAcceptedResponse,
  SessionHeartbeatResponse,
  SessionResponse,
  Service,
} from '../client-ts';

export type PlayerStatus =
  | 'idle'
  | 'starting'
  | 'priming'
  | 'building'
  | 'ready'
  | 'buffering'
  | 'playing'
  | 'recovering'
  | 'error'
  | 'stopped'
  | 'paused';

export interface PlayerStats {
  bandwidth: number;
  resolution: string;
  fps: number;
  droppedFrames: number;
  buffer: number;
  bufferHealth: number;
  latency: number | null;
  levelIndex: number;
}

export interface Channel {
  id?: string;
  serviceRef?: string;
  name?: string;
  [key: string]: unknown;
}

export interface V3PlayerBaseProps {
  token?: string;
  autoStart?: boolean;
  onClose?: () => void;
  duration?: number; // Duration in seconds (enables VOD mode)
}

export interface V3PlayerLiveProps extends V3PlayerBaseProps {
  channel: Service;
  src?: never;
}

export interface V3PlayerDirectProps extends V3PlayerBaseProps {
  src: string;
  channel?: never;
}

export interface V3PlayerRecordingProps extends V3PlayerBaseProps {
  recordingId: string;
  channel?: never;
  src?: never;
}

export type V3PlayerProps = V3PlayerLiveProps | V3PlayerDirectProps | V3PlayerRecordingProps;


export interface SessionCookieState {
  token: string | null;
  pending: Promise<void> | null;
}
export type V3SessionResponse = IntentAcceptedResponse;
export type V3SessionStatusResponse = SessionResponse;
export type V3SessionHeartbeatResponse = SessionHeartbeatResponse;
export type V3SessionSnapshot =
  Pick<SessionResponse, 'sessionId' | 'state'> &
  Partial<Pick<SessionResponse, 'requestId' | 'reason' | 'reasonDetail' | 'profileReason' | 'trace'>>;


// HLS-specific types
export type HlsInstanceRef = Hls | null;

export interface SafariVideoElement extends HTMLVideoElement {
  webkitEnterFullscreen?: () => void;
  webkitSupportsFullscreen?: boolean;
  webkitDisplayingFullscreen?: boolean;
}

export type VideoElementRef = SafariVideoElement | null;
