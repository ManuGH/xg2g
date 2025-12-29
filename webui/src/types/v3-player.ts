// Type definitions for V3 Player component

import type Hls from 'hls.js';
import type { Service } from '../client-ts';

export type PlayerStatus =
  | 'idle'
  | 'starting'
  | 'priming'
  | 'building'
  | 'ready'
  | 'buffering'
  | 'playing'
  | 'error'
  | 'stopped'
  | 'paused';

export interface Channel {
  id?: string;
  service_ref?: string;
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

export interface V3Intent {
  type: 'stream.start' | 'stream.stop';
  profileID?: string;
  serviceRef?: string;
  sessionId?: string;
}

export interface V3SessionResponse {
  sessionId: string;
  status?: string;
  correlationId?: string;
}

export interface V3SessionStatusResponse {
  sessionId: string;
  state:
  | 'NEW'
  | 'STARTING'
  | 'PRIMING'
  | 'READY'
  | 'DRAINING'
  | 'STOPPING'
  | 'STOPPED'
  | 'FAILED'
  | 'CANCELLED';
  reason?: string;
  reasonDetail?: string;
  mode?: 'LIVE' | 'RECORDING';
  durationSeconds?: number;
  seekableStartSeconds?: number;
  seekableEndSeconds?: number;
  liveEdgeSeconds?: number;
  playbackUrl?: string;
}

// HLS-specific types
export type HlsInstanceRef = Hls | null;
export type VideoElementRef = HTMLVideoElement | null;
