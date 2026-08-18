// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { CurrentServiceInfo } from '../client-ts';
import { computeOnAirProgress, formatClockLabel, toWholeMinutes } from '../utils/onAir';
import type { ChipState } from './ui/StatusChip';
import { Button, Card, StatusChip } from './ui';
import styles from './OnAirHero.module.css';

// The receiver reports a programme, not a video: minute resolution is the
// truth available, so re-deriving the position twice a minute keeps the
// counters honest without a per-second render of a bar nobody watches move.
const TICK_MS = 30_000;

export interface OnAirHeroProps {
  // Both queries settle to null rather than undefined when the receiver has
  // nothing to report, so the props accept what the hooks actually hand over.
  receiver?: CurrentServiceInfo | null;
  recording?: { isRecording?: boolean; serviceName?: string } | null;
  healthChip: { state: ChipState; label: string };
  primaryAction: { label: string; onAction: () => void };
}

export default function OnAirHero({
  receiver,
  recording,
  healthChip,
  primaryAction,
}: OnAirHeroProps) {
  const { t } = useTranslation();
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), TICK_MS);
    return () => clearInterval(id);
  }, []);

  const receiverUnavailable = receiver?.status === 'unavailable';
  const channelName = receiver?.channel?.name;
  const programmeTitle = receiver?.now?.title;
  const progress = receiverUnavailable
    ? null
    : computeOnAirProgress(receiver?.now?.beginTimestamp, receiver?.now?.durationSec, nowMs);

  const eyebrow = receiverUnavailable
    ? t('dashboard.onAir.standbyEyebrow')
    : channelName ?? t('dashboard.onAir.eyebrow');

  const title = receiverUnavailable
    ? t('dashboard.onAir.standbyTitle')
    : programmeTitle ?? channelName ?? t('dashboard.onAir.noProgramme');

  return (
    <Card variant={progress ? 'live' : 'standard'} className={styles.hero} data-testid="on-air-hero">
      <div className={styles.header}>
        <span className={styles.eyebrow}>{eyebrow}</span>
        <div className={styles.indicators}>
          {recording?.isRecording ? (
            <StatusChip
              state="recording"
              label={
                recording.serviceName
                  ? t('dashboard.onAir.recordingService', { service: recording.serviceName })
                  : t('dashboard.onAir.recording')
              }
            />
          ) : null}
          <StatusChip state={healthChip.state} label={healthChip.label} />
        </div>
      </div>

      <h1 className={styles.title}>{title}</h1>

      {progress ? (
        // The signature of this surface: the programme on its own clock. Start
        // and end are what the guide promised, the marker is where the
        // household actually is inside that promise.
        <div
          className={styles.timeline}
          role="group"
          aria-label={t('dashboard.onAir.timelineLabel')}
        >
          <div className={styles.timelineTrack}>
            <span className={styles.timelineEdge}>{formatClockLabel(progress.startMs)}</span>
            <div className={styles.timelineRail}>
              <div
                className={styles.timelineFill}
                style={{ inlineSize: `${(progress.fraction * 100).toFixed(2)}%` }}
              />
              <div
                className={styles.timelineMarker}
                style={{ insetInlineStart: `${(progress.fraction * 100).toFixed(2)}%` }}
              />
            </div>
            <span className={styles.timelineEdge}>{formatClockLabel(progress.endMs)}</span>
          </div>
          <div className={styles.timelineCounters}>
            <span className={styles.counterLabel}>
              {t('dashboard.onAir.elapsed', { minutes: toWholeMinutes(progress.elapsedSec) })}
            </span>
            <span className={styles.counterLabel}>
              {t('dashboard.onAir.remaining', { minutes: toWholeMinutes(progress.remainingSec) })}
            </span>
          </div>
        </div>
      ) : null}

      <div className={[styles.footer, receiver?.next?.title ? '' : styles.footerActionOnly].join(' ').trim()}>
        {receiver?.next?.title ? (
          <p className={styles.nextUp}>
            <span className={styles.nextLabel}>{t('dashboard.onAir.nextUp')}</span>
            <span className={styles.nextTitle}>{receiver.next.title}</span>
          </p>
        ) : null}
        <Button variant="primary" onClick={primaryAction.onAction}>
          {primaryAction.label}
        </Button>
      </div>
    </Card>
  );
}
