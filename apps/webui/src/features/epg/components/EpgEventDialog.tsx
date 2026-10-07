import { useEffect, useState, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import type { EpgEvent, EpgChannel } from '../types';
import { normalizeEpgText } from '../../../utils/text';
import { formatLocalDateOnly } from '../../../utils/date';
import { Button } from '../../../components/ui';
import { fetchEpgEvents } from '../epgApi';
import { extractMetadata, findRerunsAndBroadcasts } from '../utils/rerunMatcher';
import styles from './EpgEventDialog.module.css';

export interface ScheduleSeriesConfig {
  keyword: string;
  channelRef?: string;
  days?: number[];
  startWindow?: string;
  retentionDays?: number;
  expiresAt?: string;
}

interface EpgEventDialogProps {
  event: EpgEvent;
  onClose: () => void;
  onRecord?: (event: EpgEvent) => void;
  onScheduleSeries?: (event: EpgEvent, config: ScheduleSeriesConfig) => Promise<void> | void;
  isRecorded?: boolean | ((event: EpgEvent) => boolean);
  onPlay?: (channel: EpgChannel) => void;
  channel?: EpgChannel;
  channels?: EpgChannel[];
  currentTime?: number;
}

function formatDateTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function formatTime(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function formatRerunDate(ts: number): string {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(d);
}

function extractCleanTitle(raw: string): string {
  if (!raw) return '';
  const match = raw.split(/\s+(?:mit|–|-)\s+|:\s+/i);
  const first = match[0];
  if (match.length > 1 && first && first.trim().length >= 3) {
    return first.trim();
  }
  return raw.trim();
}

const DAY_OPTIONS = [
  { label: 'Mo', day: 1 },
  { label: 'Di', day: 2 },
  { label: 'Mi', day: 3 },
  { label: 'Do', day: 4 },
  { label: 'Fr', day: 5 },
  { label: 'Sa', day: 6 },
  { label: 'So', day: 0 },
];

export function EpgEventDialog({
  event,
  onClose,
  onRecord,
  onScheduleSeries,
  isRecorded,
  onPlay,
  channel,
  channels = [],
  currentTime,
}: EpgEventDialogProps) {
  const { t } = useTranslation();
  const [view, setView] = useState<'details' | 'series' | 'pick-broadcast'>('details');

  // Metadata & Rerun matching
  const meta = useMemo(() => extractMetadata(event), [event]);
  const [candidateEvents, setCandidateEvents] = useState<EpgEvent[]>([]);
  const [expandedSameReruns, setExpandedSameReruns] = useState<boolean>(false);
  const [expandedOtherEpisodes, setExpandedOtherEpisodes] = useState<boolean>(false);

  // Series scheduling state
  const cleanedTitle = useMemo(() => extractCleanTitle(event.title || ''), [event.title]);
  const [keyword, setKeyword] = useState<string>(cleanedTitle || event.title || '');
  const [selectedDays, setSelectedDays] = useState<number[]>([0, 1, 2, 3, 4, 5, 6]);
  const [startWindow, setStartWindow] = useState<string>('');
  const [retentionDays, setRetentionDays] = useState<number>(7);
  const [expiresAt, setExpiresAt] = useState<string>('');
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);

  useEffect(() => {
    // Lock body scroll
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

  // Fetch cross-channel candidate events for reruns
  useEffect(() => {
    let active = true;
    const controller = new AbortController();

    if (meta.mainTitle && meta.mainTitle.trim().length >= 2) {
      fetchEpgEvents({
        query: meta.mainTitle.trim(),
        signal: controller.signal,
      })
        .then((candidates) => {
          if (!active) return;
          setCandidateEvents(candidates);
        })
        .catch(() => {
          // Gracefully ignore abort / network issues
        });
    }

    return () => {
      active = false;
      controller.abort();
    };
  }, [meta.mainTitle]);

  const now = currentTime || Math.floor(Date.now() / 1000);
  const inProgress = now >= event.start && now < event.end;
  const desc = event.desc ? normalizeEpgText(event.desc) : t('epg.noDescription', { defaultValue: 'No description available.' });

  const channelPool = useMemo(() => {
    const list = [...channels];
    if (channel && !list.some((c) => (c.serviceRef && c.serviceRef === channel.serviceRef) || (c.id && c.id === channel.id))) {
      list.push(channel);
    }
    return list;
  }, [channels, channel]);

  const rerunResult = useMemo(() => {
    return findRerunsAndBroadcasts(event, candidateEvents, channelPool, now);
  }, [event, candidateEvents, channelPool, now]);

  const checkIsRecorded = (evt: EpgEvent): boolean => {
    if (typeof isRecorded === 'function') {
      return isRecorded(evt);
    }
    if (evt.start === event.start && evt.serviceRef === event.serviceRef) {
      return Boolean(isRecorded);
    }
    return false;
  };

  const isCurrentRecorded = checkIsRecorded(event);

  const toggleDay = (day: number) => {
    setSelectedDays((prev) =>
      prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day].sort((a, b) => a - b)
    );
  };

  const handleSeriesSubmit = async () => {
    if (!keyword.trim() || !onScheduleSeries) return;
    const channelRef = event.serviceRef || channel?.serviceRef;
    if (!channelRef) return;
    setIsSubmitting(true);
    try {
      const days = selectedDays.length === 7 || selectedDays.length === 0 ? undefined : selectedDays;
      const expiresAtIso = expiresAt.trim()
        ? new Date(`${expiresAt.trim()}T23:59:59`).toISOString()
        : undefined;
      await onScheduleSeries(event, {
        keyword: keyword.trim(),
        channelRef,
        days,
        startWindow: startWindow.trim() || undefined,
        retentionDays: retentionDays > 0 ? retentionDays : undefined,
        expiresAt: expiresAtIso,
      });
      onClose();
    } catch {
      // Error handled by parent onScheduleSeries
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
      <div className={styles.card} role="dialog" aria-modal="true" aria-labelledby="epg-event-title">
        <div className={styles.header}>
          <h2 id="epg-event-title" className={styles.title}>
            {view === 'series'
              ? t('epg.seriesDialogTitle', { defaultValue: 'Serienaufnahme / Scheduler einrichten' })
              : view === 'pick-broadcast'
                ? t('epg.whichBroadcast', { defaultValue: 'Welche Ausstrahlung aufnehmen?' })
                : (event.title || t('epg.unknownTitle', { defaultValue: 'Unknown show' }))}
          </h2>
          <div className={styles.time}>
            {channel?.name ? `${channel.name} · ` : ''}{formatDateTime(event.start)} – {formatTime(event.end)}
          </div>
        </div>

        {view === 'details' && (
          <>
            <div className={styles.content}>
              <div>{desc}</div>

              {/* Rerun Section: "Weitere Ausstrahlungen" (Movie) or "Diese Folge erneut" (Series) */}
              {rerunResult.sameEpisodeReruns.length > 0 && (
                <div className={styles.rerunSection} data-testid="rerun-section-same">
                  <h3 className={styles.rerunSectionTitle}>
                    {meta.classification === 'movie'
                      ? t('epg.furtherBroadcasts', { defaultValue: 'Weitere Ausstrahlungen' })
                      : t('epg.sameEpisodeReruns', { defaultValue: 'Diese Folge erneut' })}
                    <span className={styles.rerunSectionBadge}>
                      {rerunResult.sameEpisodeReruns.length}
                    </span>
                  </h3>

                  <div className={styles.rerunList}>
                    {(expandedSameReruns
                      ? rerunResult.sameEpisodeReruns
                      : rerunResult.sameEpisodeReruns.slice(0, 3)
                    ).map((rerun) => {
                      const isRec = checkIsRecorded(rerun.event);
                      return (
                        <div
                          key={`${rerun.event.serviceRef}:${rerun.event.start}`}
                          className={styles.rerunItem}
                        >
                          <div className={styles.rerunInfo}>
                            <div className={styles.rerunDateTime}>
                              <span>{formatRerunDate(rerun.event.start)}</span>
                              <span>—</span>
                              <strong className={styles.rerunChannelName}>{rerun.channelName}</strong>
                            </div>
                            {rerun.confidenceLabel && (
                              <span className={styles.rerunMetaLabel}>
                                {rerun.confidenceLabel}
                              </span>
                            )}
                          </div>
                          {onRecord && (
                            <div className={styles.rerunActions}>
                              {isRec ? (
                                <span className={styles.rerunRecordedPill}>● REC</span>
                              ) : (
                                <button
                                  type="button"
                                  className={styles.rerunRecordBtn}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    onRecord(rerun.event);
                                  }}
                                  title={t('epg.record', { defaultValue: 'Aufnehmen' })}
                                >
                                  ⏺ {t('epg.record', { defaultValue: 'Aufnehmen' })}
                                </button>
                              )}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>

                  {rerunResult.sameEpisodeReruns.length > 3 && (
                    <button
                      type="button"
                      className={styles.rerunMoreBtn}
                      onClick={() => setExpandedSameReruns((prev) => !prev)}
                    >
                      {expandedSameReruns
                        ? t('epg.showLess', { defaultValue: 'Weniger anzeigen' })
                        : t('epg.moreReruns', {
                            count: rerunResult.sameEpisodeReruns.length - 3,
                            defaultValue: `+${rerunResult.sameEpisodeReruns.length - 3} weitere`,
                          })}
                    </button>
                  )}
                </div>
              )}

              {/* Other Episodes of the Series: "Weitere Folgen der Serie" */}
              {meta.classification === 'series' && rerunResult.otherEpisodes.length > 0 && (
                <div className={styles.rerunSection} data-testid="rerun-section-other">
                  <h3 className={styles.rerunSectionTitle}>
                    {t('epg.otherEpisodes', { defaultValue: 'Weitere Folgen der Serie' })}
                    <span className={styles.rerunSectionBadge}>
                      {rerunResult.otherEpisodes.length}
                    </span>
                  </h3>

                  <div className={styles.rerunList}>
                    {(expandedOtherEpisodes
                      ? rerunResult.otherEpisodes
                      : rerunResult.otherEpisodes.slice(0, 3)
                    ).map((ep) => {
                      const isRec = checkIsRecorded(ep.event);
                      return (
                        <div
                          key={`${ep.event.serviceRef}:${ep.event.start}`}
                          className={styles.rerunItem}
                        >
                          <div className={styles.rerunInfo}>
                            <div className={styles.rerunDateTime}>
                              <span>{formatRerunDate(ep.event.start)}</span>
                              <span>—</span>
                              <strong className={styles.rerunChannelName}>{ep.channelName}</strong>
                            </div>
                            {ep.confidenceLabel && (
                              <span className={styles.rerunMetaLabel}>
                                {ep.confidenceLabel}
                              </span>
                            )}
                          </div>
                          {onRecord && (
                            <div className={styles.rerunActions}>
                              {isRec ? (
                                <span className={styles.rerunRecordedPill}>● REC</span>
                              ) : (
                                <button
                                  type="button"
                                  className={styles.rerunRecordBtn}
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    onRecord(ep.event);
                                  }}
                                  title={t('epg.record', { defaultValue: 'Aufnehmen' })}
                                >
                                  ⏺ {t('epg.record', { defaultValue: 'Aufnehmen' })}
                                </button>
                              )}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>

                  {rerunResult.otherEpisodes.length > 3 && (
                    <button
                      type="button"
                      className={styles.rerunMoreBtn}
                      onClick={() => setExpandedOtherEpisodes((prev) => !prev)}
                    >
                      {expandedOtherEpisodes
                        ? t('epg.showLess', { defaultValue: 'Weniger anzeigen' })
                        : t('epg.moreReruns', {
                            count: rerunResult.otherEpisodes.length - 3,
                            defaultValue: `+${rerunResult.otherEpisodes.length - 3} weitere`,
                          })}
                    </button>
                  )}
                </div>
              )}
            </div>

            <div className={styles.footer}>
              {inProgress && channel && onPlay && (
                <Button
                  variant="primary"
                  onClick={() => {
                    onPlay(channel);
                    onClose();
                  }}
                >
                  ▶ {t('epg.playChannel', { defaultValue: 'Sendung schauen' })}
                </Button>
              )}
              {onRecord && (
                <Button
                  variant={inProgress && channel && onPlay ? 'secondary' : (isCurrentRecorded ? 'secondary' : 'primary')}
                  onClick={() => {
                    if (rerunResult.sameEpisodeReruns.length > 0) {
                      setView('pick-broadcast');
                    } else {
                      onRecord(event);
                      onClose();
                    }
                  }}
                >
                  {isCurrentRecorded
                    ? t('epg.recordingPlanned', { defaultValue: 'Aufnahme geplant' })
                    : meta.classification === 'series'
                      ? `● ${t('epg.recordThisEpisode', { defaultValue: 'Diese Folge aufnehmen' })}`
                      : `● ${t('epg.recordSingle', { defaultValue: 'Aufnehmen' })}`}
                </Button>
              )}
              {meta.classification === 'series' && onScheduleSeries && (
                <Button
                  variant="secondary"
                  onClick={() => setView('series')}
                  data-testid="series-record-trigger"
                >
                  🔄 {t('epg.recordSeries', { defaultValue: 'Als Serie aufnehmen' })}
                </Button>
              )}
              <Button variant="secondary" onClick={onClose}>
                {t('common.close', { defaultValue: 'Schließen' })}
              </Button>
            </div>
          </>
        )}

        {view === 'pick-broadcast' && (
          <>
            <div className={styles.content}>
              <div className={styles.pickerDialogList}>
                {/* Option 1: Current / Original Broadcast */}
                <button
                  type="button"
                  className={styles.pickerDialogItem}
                  onClick={() => {
                    onRecord?.(event);
                    onClose();
                  }}
                >
                  <div className={styles.rerunInfo}>
                    <span className={styles.rerunDateTime}>
                      {formatDateTime(event.start)} – {formatTime(event.end)}
                    </span>
                    <span className={styles.rerunMetaLabel}>
                      <strong>{channel?.name || ''}</strong> · {t('epg.currentBroadcast', { defaultValue: 'Aktuelle Ausstrahlung' })}
                    </span>
                  </div>
                  {isCurrentRecorded ? (
                    <span className={styles.rerunRecordedPill}>● REC</span>
                  ) : (
                    <span className={styles.rerunRecordBtn}>⏺ {t('epg.record', { defaultValue: 'Aufnehmen' })}</span>
                  )}
                </button>

                {/* Option 2+: Future Reruns */}
                {rerunResult.sameEpisodeReruns.map((rerun) => {
                  const isRec = checkIsRecorded(rerun.event);
                  return (
                    <button
                      key={`${rerun.event.serviceRef}:${rerun.event.start}`}
                      type="button"
                      className={styles.pickerDialogItem}
                      onClick={() => {
                        onRecord?.(rerun.event);
                        onClose();
                      }}
                    >
                      <div className={styles.rerunInfo}>
                        <span className={styles.rerunDateTime}>
                          {formatRerunDate(rerun.event.start)}
                        </span>
                        <span className={styles.rerunMetaLabel}>
                          <strong className={styles.rerunChannelName}>{rerun.channelName}</strong>
                          {rerun.confidenceLabel ? ` · ${rerun.confidenceLabel}` : ''}
                        </span>
                      </div>
                      {isRec ? (
                        <span className={styles.rerunRecordedPill}>● REC</span>
                      ) : (
                        <span className={styles.rerunRecordBtn}>⏺ {t('epg.record', { defaultValue: 'Aufnehmen' })}</span>
                      )}
                    </button>
                  );
                })}
              </div>
            </div>

            <div className={styles.footer}>
              <Button variant="secondary" onClick={() => setView('details')}>
                {t('epg.seriesBack')}
              </Button>
              <Button variant="secondary" onClick={onClose}>
                {t('common.close', { defaultValue: 'Schließen' })}
              </Button>
            </div>
          </>
        )}

        {view === 'series' && (
          <>
            <div className={styles.seriesContent}>
              {/* Keyword / Title */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesKeywordLabel', { defaultValue: 'Suchbegriff (Titel)' })}</label>
                <input
                  type="text"
                  className={styles.inputField}
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  placeholder={t('epg.seriesKeywordPlaceholder')}
                  data-testid="series-modal-keyword"
                />
                {cleanedTitle && cleanedTitle !== event.title && (
                  <div className={styles.chipRow} style={{ marginTop: '4px' }}>
                    <button
                      type="button"
                      className={[styles.chip, keyword === cleanedTitle ? styles.chipActive : ''].filter(Boolean).join(' ')}
                      onClick={() => setKeyword(cleanedTitle)}
                    >
                      {cleanedTitle}
                    </button>
                    <button
                      type="button"
                      className={[styles.chip, keyword === event.title ? styles.chipActive : ''].filter(Boolean).join(' ')}
                      onClick={() => setKeyword(event.title)}
                    >
                      {event.title}
                    </button>
                  </div>
                )}
                <span className={styles.helpText}>{t('epg.seriesKeywordHelp', { defaultValue: 'Übereinstimmung beim Sendungstitel (ohne Berücksichtigung von Akzenten oder Groß-/Kleinschreibung).' })}</span>
              </div>

              {/* Channel Display */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesChannelLabel', { defaultValue: 'Sender' })}</label>
                <div className={styles.chipRow}>
                  <span className={[styles.chip, styles.chipActive].join(' ')}>
                    {channel?.name || event.serviceRef || t('epg.seriesChannelThis', { defaultValue: 'Dieser Sender' })}
                  </span>
                </div>
                <span className={styles.helpText}>{t('epg.seriesChannelHelp', { defaultValue: 'Die Serie wird auf diesem Sender überwacht und programmiert.' })}</span>
              </div>

              {/* Days of week */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesDaysLabel', { defaultValue: 'Wochentage' })}</label>
                <div className={styles.chipRow}>
                  <button
                    type="button"
                    className={[styles.chip, selectedDays.length === 7 ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setSelectedDays([0, 1, 2, 3, 4, 5, 6])}
                  >
                    {t('series.daysDaily', { defaultValue: 'Täglich' })}
                  </button>
                  <button
                    type="button"
                    className={[styles.chip, selectedDays.length === 5 && [1, 2, 3, 4, 5].every((d) => selectedDays.includes(d)) ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setSelectedDays([1, 2, 3, 4, 5])}
                  >
                    {t('series.daysWeekdays', { defaultValue: 'Werktags (Mo-Fr)' })}
                  </button>
                  <button
                    type="button"
                    className={[styles.chip, selectedDays.length === 2 && [0, 6].every((d) => selectedDays.includes(d)) ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setSelectedDays([0, 6])}
                  >
                    {t('series.daysWeekend', { defaultValue: 'Wochenende (Sa-So)' })}
                  </button>
                </div>
                <div className={styles.daySelector}>
                  {DAY_OPTIONS.map((opt) => (
                    <button
                      key={opt.day}
                      type="button"
                      className={[styles.dayButton, selectedDays.includes(opt.day) ? styles.dayButtonActive : ''].filter(Boolean).join(' ')}
                      onClick={() => toggleDay(opt.day)}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Time Window */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesTimeWindowLabel', { defaultValue: 'Startzeit-Fenster (HHMM-HHMM)' })}</label>
                <input
                  type="text"
                  className={styles.inputField}
                  value={startWindow}
                  onChange={(e) => setStartWindow(e.target.value)}
                  placeholder={t('epg.seriesTimeWindowPlaceholder')}
                />
                <span className={styles.helpText}>{t('epg.seriesTimeWindowHelp', { defaultValue: 'Optional. z.B. 0530-0930 für morgendliche Sendungen.' })}</span>
              </div>

              {/* Retention Policy */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesRetentionLabel', { defaultValue: 'Vorhaltezeit / Aufbewahrung' })}</label>
                <div className={styles.chipRow}>
                  {[
                    { days: 7, label: t('series.retentionDays', { count: 7, defaultValue: '7 Tage' }) + ' (Empfohlen)' },
                    { days: 14, label: t('series.retentionDays', { count: 14, defaultValue: '14 Tage' }) },
                    { days: 30, label: t('series.retentionDays', { count: 30, defaultValue: '30 Tage' }) },
                    { days: 0, label: t('epg.seriesRetentionForever', { defaultValue: 'Dauerhaft behalten' }) },
                  ].map((preset) => (
                    <button
                      key={preset.days}
                      type="button"
                      className={[styles.chip, retentionDays === preset.days ? styles.chipActive : ''].filter(Boolean).join(' ')}
                      onClick={() => setRetentionDays(preset.days)}
                    >
                      {preset.label}
                    </button>
                  ))}
                </div>
                <span className={styles.helpText}>
                  {retentionDays > 0
                    ? t('epg.seriesRetentionDays', { count: retentionDays, defaultValue: `${retentionDays} Tage (Auto-Löschen)` })
                    : t('epg.seriesRetentionForever', { defaultValue: 'Dauerhaft behalten' })}
                  {' — '}{t('epg.seriesRetentionHelp', { defaultValue: 'Aufnahmen, die älter als diese Tage sind, werden automatisch gelöscht.' })}
                </span>
              </div>

              {/* Expiration Policy */}
              <div className={styles.formGroup}>
                <label className={styles.formLabel}>{t('epg.seriesExpiresAtLabel', { defaultValue: 'Gültig bis (Ablaufdatum / EOL)' })}</label>
                <div className={styles.chipRow}>
                  <button
                    type="button"
                    className={[styles.chip, !expiresAt ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setExpiresAt('')}
                  >
                    {t('epg.seriesExpiresNever', { defaultValue: 'Dauerhaft behalten' })}
                  </button>
                  <button
                    type="button"
                    className={[styles.chip, expiresAt === `${new Date().getFullYear()}-12-31` ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setExpiresAt(`${new Date().getFullYear()}-12-31`)}
                  >
                    {t('epg.seriesExpiresEndOfYear', { defaultValue: 'Bis Jahresende' })} ({new Date().getFullYear()})
                  </button>
                  <button
                    type="button"
                    className={styles.chip}
                    onClick={() => {
                      const d = new Date();
                      d.setMonth(d.getMonth() + 3);
                      setExpiresAt(formatLocalDateOnly(d));
                    }}
                  >
                    {t('epg.seriesExpires3Months', { defaultValue: '+3 Monate' })}
                  </button>
                </div>
                <input
                  type="date"
                  className={styles.inputField}
                  value={expiresAt}
                  onChange={(e) => setExpiresAt(e.target.value)}
                />
                <span className={styles.helpText}>{t('epg.seriesExpiresAtHelp', { defaultValue: 'Optional. Nach diesem Datum werden keine neuen Sendungen mehr programmiert.' })}</span>
              </div>
            </div>

            <div className={styles.footer}>
              <Button
                variant="secondary"
                onClick={() => setView('details')}
                disabled={isSubmitting}
              >
                {t('epg.seriesBack', { defaultValue: 'Zurück' })}
              </Button>
              <Button
                variant="primary"
                onClick={handleSeriesSubmit}
                disabled={!keyword.trim() || isSubmitting}
                data-testid="series-modal-save"
              >
                {isSubmitting
                  ? t('epg.seriesSaving', { defaultValue: 'Wird eingerichtet...' })
                  : `✓ ${t('epg.seriesSave', { defaultValue: 'Serie einrichten' })}`}
              </Button>
            </div>
          </>
        )}
      </div>
    </div>,
    document.body
  );
}
