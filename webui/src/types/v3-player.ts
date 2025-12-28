// Type definitions for V3 Player component

import type Hls from 'hls.js';
import type { Service } from '../client-ts';

export type PlayerStatus =
  | 'idle'
  | 'starting'
  | 'priming'
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

export type V3PlayerProps = V3PlayerLiveProps | V3PlayerDirectProps;


export interface SessionCookieState {
  token: string | null;
  pending: Promise<void> | null;
}

export interface V3Intent {
  type: 'stream.start' | 'stream.stop';
  profile?: string;
  serviceRef?: string;
  sessionId?: string;
}

export interface V3SessionResponse {
  sessionId: string;
  state?: string;
  playlistUrl?: string;
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
}

// HLS-specific types
export type HlsInstanceRef = Hls | null;
export type VideoElementRef = HTMLVideoElement | null;
