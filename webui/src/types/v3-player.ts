// Type definitions for V3 Player component

import type Hls from 'hls.js';

export type PlayerStatus = 'idle' | 'starting' | 'buffering' | 'playing' | 'error' | 'stopped';

export interface Channel {
  id?: string;
  service_ref?: string;
  name?: string;
  [key: string]: unknown;
}

export interface V3PlayerProps {
  token: string;
  channel: Channel | null;
  autoStart?: boolean;
  onClose?: () => void;
}

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
  state: 'PENDING' | 'READY' | 'PLAYING' | 'ERROR' | 'STOPPED';
  error?: string;
}

// HLS-specific types
export type HlsInstanceRef = Hls | null;
export type VideoElementRef = HTMLVideoElement | null;
