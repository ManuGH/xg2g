import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import type { PlaybackTraceRuntimeReplay } from '../../../client-ts';
import { Button } from '../../../components/ui';

import styles from './V3Player.module.css';

type CopyState = 'idle' | 'copied' | 'failed';

export function serializeRuntimePolicyReplay(replay: PlaybackTraceRuntimeReplay): string {
  return JSON.stringify(replay, null, 2);
}

export interface PlayerRuntimeReplayExportProps {
  replay: PlaybackTraceRuntimeReplay | null | undefined;
}

export function PlayerRuntimeReplayExport({
  replay,
}: PlayerRuntimeReplayExportProps) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = useState<CopyState>('idle');

  useEffect(() => {
    if (copyState === 'idle') {
      return undefined;
    }

    const timer = window.setTimeout(() => setCopyState('idle'), 2200);
    return () => window.clearTimeout(timer);
  }, [copyState]);

  const serializedReplay = useMemo(
    () => (replay ? serializeRuntimePolicyReplay(replay) : null),
    [replay]
  );

  const handleCopy = async () => {
    if (!serializedReplay || typeof navigator === 'undefined' || !navigator.clipboard?.writeText) {
      setCopyState('failed');
      return;
    }

    try {
      await navigator.clipboard.writeText(serializedReplay);
      setCopyState('copied');
    } catch {
      setCopyState('failed');
    }
  };

  if (!replay) {
    return null;
  }

  const statusText = copyState === 'copied'
    ? t('player.runtimeReplayCopied', { defaultValue: 'Replay copied' })
    : copyState === 'failed'
      ? t('player.runtimeReplayCopyFailed', { defaultValue: 'Replay copy failed' })
      : null;

  return (
    <div className={styles.runtimeReplayActions}>
      <Button
        variant="secondary"
        size="sm"
        state={copyState === 'copied' ? 'valid' : copyState === 'failed' ? 'invalid' : undefined}
        onClick={() => void handleCopy()}
      >
        {t('player.copyRuntimeReplay', { defaultValue: 'Copy replay' })}
      </Button>
      {statusText ? (
        <span role="status" className={styles.runtimeReplayStatus}>
          {statusText}
        </span>
      ) : null}
    </div>
  );
}
