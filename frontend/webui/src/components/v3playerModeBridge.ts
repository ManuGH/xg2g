export const SERVER_PLAYBACK_MODES = [
  'native_hls',
  'hlsjs',
  'direct_mp4',
  'transcode',
  'deny'
] as const;

export type ServerPlaybackMode = (typeof SERVER_PLAYBACK_MODES)[number];
export type LivePlaybackEngine = 'native' | 'hlsjs';
export type PlaybackUiMode = ServerPlaybackMode;
export type VodStreamMode = Exclude<PlaybackUiMode, 'deny'>;
export type ResolvedLivePlaybackMode = Readonly<{
  mode: PlaybackUiMode;
  engine: LivePlaybackEngine | null;
}>;
export type LiveEngineAvailability = Readonly<{
  native: boolean;
  hlsjs: boolean;
}>;

const SERVER_PLAYBACK_MODE_SET: ReadonlySet<ServerPlaybackMode> = new Set(SERVER_PLAYBACK_MODES);

export function parseServerPlaybackMode(value: unknown): ServerPlaybackMode | null {
  if (typeof value !== 'string') return null;
  if (!SERVER_PLAYBACK_MODE_SET.has(value as ServerPlaybackMode)) return null;
  return value as ServerPlaybackMode;
}

export function isVodStreamMode(mode: PlaybackUiMode): mode is VodStreamMode {
  return mode !== 'deny';
}

export function resolveRecordingPlaybackUiMode(
  value: unknown,
  preferredHlsEngine: LivePlaybackEngine
): PlaybackUiMode | null {
  if (value === 'hls') {
    return preferredHlsEngine === 'native' ? 'native_hls' : 'hlsjs';
  }
  return parseServerPlaybackMode(value);
}

export function resolveLivePlaybackMode(
  value: unknown,
  preferredHlsEngine: LivePlaybackEngine
): ResolvedLivePlaybackMode | null {
  switch (value) {
    case 'native_hls':
      return { mode: 'native_hls', engine: 'native' };
    case 'hlsjs':
    case 'hls':
    case 'direct_stream':
      return {
        mode: preferredHlsEngine === 'native' ? 'native_hls' : 'hlsjs',
        engine: preferredHlsEngine,
      };
    case 'transcode':
      return { mode: 'transcode', engine: preferredHlsEngine };
    case 'direct_mp4':
      return { mode: 'direct_mp4', engine: null };
    case 'deny':
      return { mode: 'deny', engine: null };
    default:
      return null;
  }
}

export function mapServerModeToLiveEngine(mode: ServerPlaybackMode): LivePlaybackEngine | null {
  switch (mode) {
    case 'native_hls':
      return 'native';
    case 'hlsjs':
    case 'transcode':
      return 'hlsjs';
    case 'direct_mp4':
    case 'deny':
      return null;
    default:
      return null;
  }
}

export function resolveLiveEngineFromMode(value: unknown): LivePlaybackEngine | null {
  const mode = parseServerPlaybackMode(value);
  if (!mode) return null;
  return mapServerModeToLiveEngine(mode);
}

export function isLiveEngineAvailable(
  engine: LivePlaybackEngine,
  availability: LiveEngineAvailability
): boolean {
  if (engine === 'native') return availability.native;
  if (engine === 'hlsjs') return availability.hlsjs;
  return false;
}

export function resolveAvailableLiveEngineFromMode(
  value: unknown,
  availability: LiveEngineAvailability
): LivePlaybackEngine | null {
  const engine = resolveLiveEngineFromMode(value);
  if (!engine) return null;
  return isLiveEngineAvailable(engine, availability) ? engine : null;
}
