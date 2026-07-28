import { useEffect, useRef } from 'react';
import { telemetry } from '../../../services/TelemetryService';
import {
  mapPlaybackAdvisoryToTelemetryEvents,
  mapPlaybackFailureToTelemetryEvents,
} from '../semantics/playbackTelemetryMapping';
import type { PlaybackDomainState } from './playbackTypes';

interface UseTelemetryEmitterArgs {
  failure: PlaybackDomainState['failure'];
  lastAdvisory: PlaybackDomainState['lastAdvisory'];
  playbackEpoch: number;
}

export function useTelemetryEmitter({
  failure,
  lastAdvisory,
  playbackEpoch,
}: UseTelemetryEmitterArgs): void {
  const lastFailureKeyRef = useRef<string | null>(null);
  const lastAdvisoryKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!failure) {
      lastFailureKeyRef.current = null;
      return;
    }

    const telemetryKey = [
      playbackEpoch,
      failure.class,
      failure.code,
      failure.status ?? '-',
      failure.telemetryContext ?? '-',
      failure.appError?.requestId ?? '-',
    ].join(':');

    if (lastFailureKeyRef.current === telemetryKey) {
      return;
    }

    lastFailureKeyRef.current = telemetryKey;
    mapPlaybackFailureToTelemetryEvents(failure).forEach((event) => {
      telemetry.emit(event.type, event.payload);
    });
  }, [failure, playbackEpoch]);

  useEffect(() => {
    if (!lastAdvisory) {
      lastAdvisoryKeyRef.current = null;
      return;
    }

    const telemetryKey = [
      playbackEpoch,
      lastAdvisory.code,
      lastAdvisory.source,
    ].join(':');

    if (lastAdvisoryKeyRef.current === telemetryKey) {
      return;
    }

    lastAdvisoryKeyRef.current = telemetryKey;
    mapPlaybackAdvisoryToTelemetryEvents(lastAdvisory).forEach((event) => {
      telemetry.emit(event.type, event.payload);
    });
  }, [lastAdvisory, playbackEpoch]);
}
