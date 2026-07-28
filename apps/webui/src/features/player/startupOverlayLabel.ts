import type { TFunction } from 'i18next';

import type { PlayerStatus } from '../../types/v3-player';

type RuntimePolicyPhase =
  | 'probing'
  | 'cooldown'
  | 'probe_regressed'
  | 'recovering'
  | 'degraded'
  | 'stable'
  | 'unknown';

export function resolveStartupOverlayLabel(
  status: PlayerStatus,
  fallbackLabel: string,
  profileReason: string | null | undefined,
  t: TFunction,
): string {
  if (
    status !== 'starting' &&
    status !== 'priming' &&
    status !== 'buffering' &&
    status !== 'building' &&
    status !== 'recovering'
  ) {
    return '';
  }

  switch (profileReason) {
    case 'safari_compat_transcode':
      return t('player.startupHints.safariCompatTranscode', {
        defaultValue: 'Preparing Safari-compatible stream…',
      });
    case 'repair_transcode':
      return t('player.startupHints.repairTranscode', {
        defaultValue: 'Repairing and transcoding stream…',
      });
    case 'transcode_startup':
      return t('player.startupHints.transcodeStartup', {
        defaultValue: 'Preparing transcoded stream…',
      });
    default:
      // `recovering` (decode/stall reattach) previously fell through the gate
      // above and rendered an empty overlay — a frozen/black frame with no
      // feedback. Give it an explicit, honest label rather than the generic
      // "<status>…" fallback (which would also double the ellipsis, since
      // statusStates.recovering already ends in "…").
      if (status === 'recovering') {
        return t('player.startupHints.recovering', {
          defaultValue: 'Reconnecting the stream…',
        });
      }
      return fallbackLabel;
  }
}

export function resolveStartupOverlaySupport(
  profileReason: string | null | undefined,
  t: TFunction,
  status?: PlayerStatus,
): string {
  switch (profileReason) {
    case 'safari_compat_transcode':
      return t('player.startupSupport.safariCompatTranscode', {
        defaultValue: 'Safari needs a compatible stream variant first. This can take a little longer.',
      });
    case 'repair_transcode':
      return t('player.startupSupport.repairTranscode', {
        defaultValue: 'The incoming stream is being stabilized before playback starts.',
      });
    case 'transcode_startup':
      return t('player.startupSupport.transcodeStartup', {
        defaultValue: 'The stream is being prepared for this device and will start automatically.',
      });
    default:
      if (status === 'recovering') {
        return t('player.startupSupport.recovering', {
          defaultValue: 'The stream hit a snag and is being restored automatically.',
        });
      }
      return t('player.startupSupport.default', {
        defaultValue: 'Playback starts automatically as soon as the first stable segments are ready.',
      });
  }
}

function resolveRuntimePolicyProfileToken(
  phaseHint: string | null | undefined,
  t: TFunction,
): string {
  return phaseHint || t('player.runtimePolicySupport.genericProfile', {
    defaultValue: 'the current profile',
  });
}

export function resolveRuntimePolicyStartupSupport(
  phase: RuntimePolicyPhase | string | null | undefined,
  phaseHint: string | null | undefined,
  t: TFunction,
): string {
  const profile = resolveRuntimePolicyProfileToken(phaseHint, t);

  switch (phase) {
    case 'probing':
      return t('player.runtimePolicySupport.startup.probing', {
        defaultValue: 'Testing {{profile}} briefly. If it stays stable, playback will continue there.',
        profile,
      });
    case 'cooldown':
      return t('player.runtimePolicySupport.startup.cooldown', {
        defaultValue: 'Holding {{profile}} briefly before the next adjustment.',
        profile,
      });
    case 'probe_regressed':
      return t('player.runtimePolicySupport.startup.probeRegressed', {
        defaultValue: 'A higher profile just turned unstable. Staying on {{profile}} for now.',
        profile,
      });
    case 'recovering':
      return t('player.runtimePolicySupport.startup.recovering', {
        defaultValue: 'Recent instability is being absorbed before the next move.',
      });
    case 'degraded':
      return t('player.runtimePolicySupport.startup.degraded', {
        defaultValue: 'Using {{profile}} for now to keep playback stable.',
        profile,
      });
    default:
      return '';
  }
}

export function resolveRuntimePolicyErrorSupport(
  phase: RuntimePolicyPhase | string | null | undefined,
  phaseHint: string | null | undefined,
  t: TFunction,
): string {
  const profile = resolveRuntimePolicyProfileToken(phaseHint, t);

  switch (phase) {
    case 'probing':
      return t('player.runtimePolicySupport.error.probing', {
        defaultValue: 'The player was validating {{profile}} when playback slipped.',
        profile,
      });
    case 'cooldown':
      return t('player.runtimePolicySupport.error.cooldown', {
        defaultValue: 'A short cooldown is active before another profile change is allowed.',
      });
    case 'probe_regressed':
      return t('player.runtimePolicySupport.error.probeRegressed', {
        defaultValue: 'A higher profile just regressed. Keeping {{profile}} locked for the moment.',
        profile,
      });
    case 'recovering':
      return t('player.runtimePolicySupport.error.recovering', {
        defaultValue: 'The session is still inside a recovery window after recent instability.',
      });
    case 'degraded':
      return t('player.runtimePolicySupport.error.degraded', {
        defaultValue: 'The player already stepped down to {{profile}} to protect stability.',
        profile,
      });
    default:
      return '';
  }
}
