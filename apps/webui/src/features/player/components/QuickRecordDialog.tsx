import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { addTimer } from '../../../client-ts';
import { throwOnClientResultError } from '../../../services/clientWrapper';
import { useUiOverlay } from '../../../context/UiOverlayContext';
import { normalizeEpgText } from '../../../utils/text';
import { Button } from '../../../components/ui';
import styles from './QuickRecordDialog.module.css';

export interface QuickRecordDialogProps {
  channelName?: string;
  serviceRef?: string;
  programTitle?: string;
  programDesc?: string;
  startTs?: number; // Unix seconds
  endTs?: number;   // Unix seconds
  onClose: () => void;
  onSuccess?: () => void;
}

type RecordMode = 'buffer_full' | 'from_now' | 'custom';

function formatTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export function QuickRecordDialog({
  channelName,
  serviceRef,
  programTitle,
  programDesc,
  startTs,
  endTs,
  onClose,
  onSuccess,
}: QuickRecordDialogProps) {
  const { t } = useTranslation();
  const { toast } = useUiOverlay();

  const now = Math.floor(Date.now() / 1000);
  const effectiveStart = startTs || (now - 1800);
  const effectiveEnd = endTs || (now + 3600);

  const [mode, setMode] = useState<RecordMode>('buffer_full');
  const [customMinutes, setCustomMinutes] = useState<number>(60);
  const [paddingMinutes, setPaddingMinutes] = useState<number>(5);
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);

  useEffect(() => {
    const originalOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = originalOverflow;
    };
  }, [onClose]);

  const handleStartRecord = async () => {
    if (!serviceRef) {
      toast({ kind: 'error', message: 'Kein gültiger Sender für Aufnahme gefunden.' });
      return;
    }

    setIsSubmitting(true);
    try {
      let begin = effectiveStart;
      let end = effectiveEnd + paddingMinutes * 60;

      if (mode === 'from_now') {
        begin = now;
      } else if (mode === 'custom') {
        begin = effectiveStart;
        end = now + customMinutes * 60;
      }

      const title = programTitle || 'Live Aufnahme';
      const desc = normalizeEpgText(programDesc) || '';

      const result = await addTimer({
        body: {
          serviceRef,
          begin,
          end,
          name: title,
          description: desc,
        },
      });

      throwOnClientResultError(result, { source: 'QuickRecordDialog' });

      toast({
        kind: 'success',
        message: t('epg.recordSuccess', { defaultValue: `Aufnahme gestartet: ${title}` }),
      });
      onSuccess?.();
      onClose();
    } catch (err) {
      toast({
        kind: 'error',
        message: 'Fehler beim Starten der Aufnahme.',
      });
    } finally {
      setIsSubmitting(false);
    }
  };

  return createPortal(
    <div
      className={styles.overlay}
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className={styles.card} role="dialog" aria-modal="true" aria-labelledby="quick-record-title">
        <div className={styles.header}>
          <div className={styles.channelBadge}>{channelName || 'Live TV'}</div>
          <h2 id="quick-record-title" className={styles.title}>
            🔴 Sendung aufnehmen
          </h2>
          <div className={styles.programMeta}>
            <strong>{programTitle || 'Laufendes Programm'}</strong>
            <span className={styles.timeRange}>
              ({formatTime(effectiveStart)} – {formatTime(effectiveEnd)} Uhr)
            </span>
          </div>
        </div>

        <div className={styles.content}>
          <div className={styles.optionsList}>
            {/* Option 1: Full Show including Ringbuffer */}
            <button
              type="button"
              className={`${styles.optionCard} ${mode === 'buffer_full' ? styles.optionCardActive : ''}`}
              onClick={() => setMode('buffer_full')}
            >
              <div className={styles.radioDot}>
                {mode === 'buffer_full' && <div className={styles.radioInner} />}
              </div>
              <div className={styles.optionDetails}>
                <div className={styles.optionTitle}>
                  ⏪ Komplett inkl. DVR-Puffer <span className={styles.recommendedBadge}>Empfohlen</span>
                </div>
                <div className={styles.optionDesc}>
                  Nimmt die gesamte Sendung ab Beginn ({formatTime(effectiveStart)} Uhr) aus dem Ringpuffer bis zum Ende auf.
                </div>
              </div>
            </button>

            {/* Option 2: From Now */}
            <button
              type="button"
              className={`${styles.optionCard} ${mode === 'from_now' ? styles.optionCardActive : ''}`}
              onClick={() => setMode('from_now')}
            >
              <div className={styles.radioDot}>
                {mode === 'from_now' && <div className={styles.radioInner} />}
              </div>
              <div className={styles.optionDetails}>
                <div className={styles.optionTitle}>
                  ⏺️ Ab JETZT ({formatTime(now)} Uhr)
                </div>
                <div className={styles.optionDesc}>
                  Nimmt ab der aktuellen Minute bis zum offiziellen Ende ({formatTime(effectiveEnd)} Uhr) auf.
                </div>
              </div>
            </button>

            {/* Option 3: Custom Duration */}
            <button
              type="button"
              className={`${styles.optionCard} ${mode === 'custom' ? styles.optionCardActive : ''}`}
              onClick={() => setMode('custom')}
            >
              <div className={styles.radioDot}>
                {mode === 'custom' && <div className={styles.radioInner} />}
              </div>
              <div className={styles.optionDetails}>
                <div className={styles.optionTitle}>
                  ⏱️ Manuelle Dauer wählen
                </div>
                <div className={styles.optionDesc}>
                  Lege eine feste Aufnahmedauer ab Beginn der Sendung fest.
                </div>
                {mode === 'custom' && (
                  <div className={styles.customControls} onClick={(e) => e.stopPropagation()}>
                    <label className={styles.selectLabel}>
                      Dauer:
                      <select
                        className={styles.selectInput}
                        value={customMinutes}
                        onChange={(e) => setCustomMinutes(Number(e.target.value))}
                      >
                        <option value={30}>30 Minuten</option>
                        <option value={60}>60 Minuten (1 Std)</option>
                        <option value={90}>90 Minuten (1.5 Std)</option>
                        <option value={120}>120 Minuten (2 Std)</option>
                        <option value={180}>180 Minuten (3 Std)</option>
                      </select>
                    </label>
                  </div>
                )}
              </div>
            </button>
          </div>

          {/* Padding / Nachlaufzeit Setting */}
          <div className={styles.paddingConfig}>
            <label className={styles.paddingLabel}>
              <span>Nachlaufzeit (Sicherheits-Puffer am Ende):</span>
              <select
                className={styles.selectInputSmall}
                value={paddingMinutes}
                onChange={(e) => setPaddingMinutes(Number(e.target.value))}
              >
                <option value={0}>0 Min</option>
                <option value={5}>+5 Min</option>
                <option value={10}>+10 Min</option>
                <option value={15}>+15 Min</option>
              </select>
            </label>
          </div>
        </div>

        <div className={styles.footer}>
          <Button variant="secondary" onClick={onClose} disabled={isSubmitting}>
            Abbrechen
          </Button>
          <Button variant="primary" onClick={handleStartRecord} disabled={isSubmitting}>
            {isSubmitting ? 'Startet...' : '🔴 Aufnahme starten'}
          </Button>
        </div>
      </div>
    </div>,
    document.body
  );
}
