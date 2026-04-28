import type { PlaybackTargetProfile } from '../../../client-ts';

export function formatQualityRungLabel(rung: string | null): string {
  if (!rung) return '-';
  return rung.split('_').join(' ');
}

export function formatBooleanLabel(value: boolean): string {
  return value ? 'yes' : 'no';
}

export function formatTargetProfileSummary(target: PlaybackTargetProfile | null): string {
  if (!target) return '-';

  // display-only: target profile summary renders backend-provided fields, not client policy.
  const videoMode = target.video?.mode || '-';
  const videoCodec = target.video?.codec ? `/${target.video.codec}` : '';
  const videoCRF = target.video?.crf ? `/crf${target.video.crf}` : '';
  const videoPreset = target.video?.preset ? `/${target.video.preset}` : '';
  // display-only: target profile summary renders backend-provided fields, not client policy.
  const audioMode = target.audio?.mode || '-';
  const audioCodec = target.audio?.codec ? `/${target.audio.codec}` : '';
  const audioChannels = target.audio?.channels ? `/${target.audio.channels}ch` : '';
  const audioBitrate = target.audio?.bitrateKbps ? `@${target.audio.bitrateKbps}k` : '';
  const packaging = target.packaging || target.container || '-';

  return [
    packaging,
    `v:${videoMode}${videoCodec}${videoCRF}${videoPreset}`,
    `a:${audioMode}${audioCodec}${audioChannels}${audioBitrate}`
  ].join(' · ');
}

export function formatExecutionLabel(target: PlaybackTargetProfile | null): string {
  if (!target?.hwAccel || target.hwAccel === 'none') {
    return 'CPU';
  }
  return target.hwAccel.toUpperCase();
}
