// Type definitions for EPG component

import type { Service, Bouquet } from '../client-ts';

export interface Programme {
  service_ref: string;
  start: number; // Unix timestamp
  end: number; // Unix timestamp
  title: string;
  desc?: string;
}

export interface Timer {
  begin: number;
  end: number;
  serviceRef?: string;
  serviceref?: string;
  service_ref?: string;
  name?: string;
  description?: string;
}

export interface TimersResponse {
  items?: Timer[];
}

export interface EpgResponse {
  items?: Programme[];
}

export interface EPGProps {
  channels: Service[];
  bouquets?: Bouquet[];
  selectedBouquet?: string;
  onSelectBouquet?: (bouquet: string) => void;
  onPlay?: (channel: Service) => void;
}

export interface ProgrammeRowProps {
  prog: Programme;
  now: number;
  highlight?: boolean;
  onRecord?: (prog: Programme) => void;
  isRecorded?: boolean;
}

export type ExpandedState = Record<string, boolean>;
export type ChannelMap = Record<string, Service>;
