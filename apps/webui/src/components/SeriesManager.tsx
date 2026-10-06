// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getSeriesRules,
  deleteSeriesRule,
  createSeriesRule,
  updateSeriesRule,
  runSeriesRule,
  getServices,
  type SeriesRule,
  type Service,
  type SeriesRuleWritable,
  type SeriesRuleUpdate
} from '../client-ts';
import { debugError, formatError } from '../utils/logging';
import { formatLocalDateOnly } from '../utils/date';
import { throwOnClientResultError } from '../services/clientWrapper';
import { useUiOverlay } from '../context/UiOverlayContext';
import { matchServiceRef } from '../utils/serviceRef';
import { ROUTE_MAP } from '../routes';
import { Button, Card, StatusChip } from './ui';
import LegacyRouteNotice from './LegacyRouteNotice';
import styles from './SeriesManager.module.css';

interface SeriesManagerProps {
  showLegacyNotice?: boolean;
}

interface DaySelectorProps {
  value: number[];
  onChange: (value: number[]) => void;
}

// Helper component for Day Selection with quick presets and day buttons
const DaySelector = ({ value, onChange }: DaySelectorProps) => {
  const { t } = useTranslation();

  const dayOptions = [
    { day: 1, label: t('common.days.mo') },
    { day: 2, label: t('common.days.di') },
    { day: 3, label: t('common.days.mi') },
    { day: 4, label: t('common.days.do') },
    { day: 5, label: t('common.days.fr') },
    { day: 6, label: t('common.days.sa') },
    { day: 0, label: t('common.days.so') },
  ];

  const dayPresets = [
    { label: t('series.daysDaily'), days: [0, 1, 2, 3, 4, 5, 6] },
    { label: t('series.daysWeekdaysFilter'), days: [1, 2, 3, 4, 5] },
    { label: t('series.daysWeekendFilter'), days: [0, 6] },
    { label: t('series.daysAll'), days: [] },
  ];

  const toggleDay = (dayIndex: number) => {
    const newValue = value.includes(dayIndex)
      ? value.filter(d => d !== dayIndex)
      : [...value, dayIndex].sort((a, b) => a - b);
    onChange(newValue);
  };

  const isPresetActive = (presetDays: number[]) => {
    if (presetDays.length === 0) return value.length === 0;
    if (value.length !== presetDays.length) return false;
    return presetDays.every(d => value.includes(d));
  };

  return (
    <div>
      <div className={styles.chipRow}>
        {dayPresets.map((preset) => (
          <button
            key={preset.label}
            type="button"
            className={[styles.chip, isPresetActive(preset.days) ? styles.chipActive : ''].filter(Boolean).join(' ')}
            onClick={() => onChange(preset.days)}
          >
            {preset.label}
          </button>
        ))}
      </div>
      <div className={styles.daySelector}>
        {dayOptions.map((opt) => (
          <button
            key={opt.day}
            className={[
              styles.dayButton,
              value.includes(opt.day) ? styles.dayButtonActive : '',
            ].filter(Boolean).join(' ')}
            onClick={() => toggleDay(opt.day)}
            type="button"
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  );
};

const formatRuleDays = (days: number[] | undefined, t: (key: string, options?: Record<string, unknown>) => string) => {
  if (!days || days.length === 0 || days.length === 7) return t('series.daysDailyAll');
  if (days.length === 5 && [1, 2, 3, 4, 5].every(d => days.includes(d))) return t('series.daysWeekdays');
  if (days.length === 2 && [0, 6].every(d => days.includes(d))) return t('series.daysWeekend');
  const dayNames: Record<number, string> = {
    1: t('common.days.mo'),
    2: t('common.days.di'),
    3: t('common.days.mi'),
    4: t('common.days.do'),
    5: t('common.days.fr'),
    6: t('common.days.sa'),
    0: t('common.days.so'),
  };
  return [...days]
    .sort((a, b) => ((a === 0 ? 7 : a) - (b === 0 ? 7 : b)))
    .map(d => dayNames[d] || String(d))
    .join(', ');
};

const formatRuleExpiry = (expiresAt?: string) => {
  if (!expiresAt) return null;
  const expDate = new Date(expiresAt);
  const isExpired = expDate.getTime() < Date.now();
  return {
    dateStr: expDate.toLocaleDateString(),
    isExpired,
  };
};

interface RuleFormState {
  id?: string;
  keyword: string;
  channelRef: string;
  days: number[];
  startWindow: string;
  priority: number | string;
  retentionDays: number | string;
  expiresAt: string; // YYYY-MM-DD
  enabled: boolean;
}

function SeriesManager({ showLegacyNotice = true }: SeriesManagerProps) {
  const { t } = useTranslation();
  const { confirm, toast } = useUiOverlay();
  const [rules, setRules] = useState<SeriesRule[]>([]);
  const [channels, setChannels] = useState<Service[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [isEditing, setIsEditing] = useState<boolean>(false);
  const [currentRule, setCurrentRule] = useState<RuleFormState | null>(null);
  const [reportLoading, setReportLoading] = useState<string | false>(false);

  // Load Initial Data
  useEffect(() => {
    loadRules();
    loadChannels();
  }, []);

  const loadRules = async () => {
    setLoading(true);
    try {
      const response = await getSeriesRules();
      setRules(response.data || []);
    } catch (err) {
      debugError('Failed to load rules:', formatError(err));
    } finally {
      setLoading(false);
    }
  };

  const loadChannels = async () => {
    try {
      const response = await getServices({ query: { bouquet: '' } });
      setChannels(response.data || []);
    } catch (err) {
      debugError('Failed to load channels:', formatError(err));
    }
  };

  const handleEdit = (rule: SeriesRule | null) => {
    if (rule) {
      setCurrentRule({
        id: rule.id,
        keyword: rule.keyword || '',
        channelRef: rule.channelRef || '',
        days: rule.days || [],
        startWindow: rule.startWindow || '',
        priority: rule.priority || 0,
        retentionDays: rule.retentionDays || 0,
        expiresAt: rule.expiresAt ? formatLocalDateOnly(new Date(rule.expiresAt)) : '',
        enabled: rule.enabled !== false
      });
    } else {
      setCurrentRule({
        keyword: '',
        channelRef: '',
        days: [],
        startWindow: '',
        priority: 0,
        retentionDays: 0,
        expiresAt: '',
        enabled: true
      });
    }
    setIsEditing(true);
  };

  const handleDelete = async (id: string) => {
    const ok = await confirm({
      title: t('series.deleteConfirmTitle'),
      message: t('series.deleteConfirmMessage'),
      confirmLabel: t('common.delete'),
      cancelLabel: t('common.cancel'),
      tone: 'danger',
    });
    if (!ok) return;
    try {
      // The SDK resolves with { error } on HTTP failure instead of throwing, so the
      // result MUST be checked — otherwise a rejected delete silently reloads the
      // (unchanged) rule list and the rule reappears with no error feedback.
      const result = await deleteSeriesRule({ path: { id } });
      throwOnClientResultError(result, { source: 'SeriesManager.deleteSeriesRule' });
      await loadRules();
    } catch (err: any) {
      toast({
        kind: 'error',
        message: t('series.deleteFailed'),
        details: err.message || t('series.unknownError'),
      });
    }
  };

  const handleSave = async () => {
    if (!currentRule) return;

    try {
      if (!currentRule.keyword?.trim()) {
        toast({
          kind: 'warning',
          message: t('series.keywordRequired'),
        });
        return;
      }

      const expiresAtPayload = currentRule.expiresAt?.trim()
        ? new Date(`${currentRule.expiresAt.trim()}T23:59:59`).toISOString()
        : undefined;

      if (currentRule.id) {
        const retentionVal = Number(currentRule.retentionDays) || 0;
        const updatePayload: SeriesRuleUpdate = {
          enabled: currentRule.enabled,
          keyword: currentRule.keyword,
          priority: Number(currentRule.priority) || 0,
          ...(retentionVal > 0 ? { retentionDays: retentionVal } : {}),
          ...(currentRule.channelRef?.trim() ? { channelRef: currentRule.channelRef.trim() } : {}),
          ...(currentRule.startWindow?.trim() ? { startWindow: currentRule.startWindow.trim() } : {}),
          ...(currentRule.days?.length ? { days: currentRule.days } : {}),
          ...(expiresAtPayload ? { expiresAt: expiresAtPayload } : {})
        };

        const result = await updateSeriesRule({
          path: { id: currentRule.id },
          body: updatePayload
        });
        throwOnClientResultError(result, { source: 'SeriesManager.updateSeriesRule' });
      } else {
        const retentionVal = Number(currentRule.retentionDays) || 0;
        // UI-INV-SERIES-001: Omit empty filters to avoid unnecessary state synthesis.
        const createPayload: SeriesRuleWritable = {
          keyword: currentRule.keyword,
          priority: Number(currentRule.priority) || 0,
          enabled: currentRule.enabled,
          ...(retentionVal > 0 ? { retentionDays: retentionVal } : {}),
          ...(currentRule.channelRef?.trim() ? { channelRef: currentRule.channelRef.trim() } : {}),
          ...(currentRule.days?.length ? { days: currentRule.days } : {}),
          ...(currentRule.startWindow?.trim() ? { startWindow: currentRule.startWindow.trim() } : {}),
          ...(expiresAtPayload ? { expiresAt: expiresAtPayload } : {})
        };

        const result = await createSeriesRule({ body: createPayload });
        throwOnClientResultError(result, { source: 'SeriesManager.createSeriesRule' });
      }
      setIsEditing(false);
      await loadRules();
    } catch (err: any) {
      toast({
        kind: 'error',
        message: t('series.saveFailed'),
        details: err.message || t('series.unknownError'),
      });
    }
  };

  const handleToggleRule = async (rule: SeriesRule) => {
    if (!rule.id) return;
    try {
      const nextEnabled = !rule.enabled;
      const updatePayload: SeriesRuleUpdate = {
        enabled: nextEnabled,
        keyword: rule.keyword || '',
        priority: rule.priority || 0,
        ...(rule.retentionDays ? { retentionDays: rule.retentionDays } : {}),
        ...(rule.channelRef ? { channelRef: rule.channelRef } : {}),
        ...(rule.days?.length ? { days: rule.days } : {}),
        ...(rule.startWindow ? { startWindow: rule.startWindow } : {}),
        ...(rule.expiresAt ? { expiresAt: rule.expiresAt } : {}),
      };
      const result = await updateSeriesRule({
        path: { id: rule.id },
        body: updatePayload,
      });
      throwOnClientResultError(result, { source: 'SeriesManager.toggleRule' });
      toast({
        kind: 'success',
        message: nextEnabled
          ? t('series.ruleActivated')
          : t('series.rulePaused'),
      });
      await loadRules();
    } catch (err: any) {
      toast({
        kind: 'error',
        message: t('series.toggleFailed'),
        details: err.message || t('series.unknownError'),
      });
    }
  };

  const handleRunNow = async (id: string) => {
    setReportLoading(id);
    try {
      const response = await runSeriesRule({
        path: { id },
        query: { trigger: 'manual' }
      });
      throwOnClientResultError(response, { source: 'SeriesManager.runSeriesRule' });
      const report = response.data;
      if (report) {
        toast({
          kind: 'success',
          message: t('series.runComplete'),
          details: t('series.runCompleteDetails', {
            matched: report.summary?.epgItemsMatched ?? 0,
            created: report.summary?.timersCreated ?? 0,
            errors: report.summary?.timersErrored ?? 0,
          }),
        });
      }
      await loadRules();
    } catch (err: any) {
      toast({
        kind: 'error',
        message: t('series.runFailed'),
        details: err.message || t('series.unknownError'),
      });
    } finally {
      setReportLoading(false);
    }
  };

  const matchingChannel = currentRule
    ? channels.find(c => matchServiceRef(c.serviceRef || c.id, currentRule.channelRef))
    : undefined;
  const selectedChannelValue = currentRule
    ? (matchingChannel ? (matchingChannel.serviceRef || matchingChannel.id || currentRule.channelRef) : currentRule.channelRef)
    : '';

  if (loading && !rules.length) {
    return <div className={styles.loadingState}>{t('series.loadingRules')}</div>;
  }

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      {showLegacyNotice ? (
        <LegacyRouteNotice
          parentLabel={t('nav.recordings')}
          description={t('legacyRoute.seriesDescription')}
          route={ROUTE_MAP.recordings}
        />
      ) : null}
      <div className={styles.header}>
        <h1>{t('series.title')}</h1>
        <Button onClick={() => handleEdit(null)} data-testid="series-add-btn">
          {t('series.newRule')}
        </Button>
      </div>

      <div className={styles.grid}>
        {rules.map(rule => (
          <Card key={rule.id} className={styles.ruleCard}>
            <div className={styles.ruleHeader}>
              <h2>{rule.keyword}</h2>
              <button
                type="button"
                className={styles.statusToggle}
                onClick={() => handleToggleRule(rule)}
                title={rule.enabled ? t('series.clickToPause') : t('series.clickToActivate')}
                aria-label={rule.enabled ? t('series.pauseAction') : t('series.activateAction')}
              >
                <StatusChip
                  state={rule.enabled ? 'success' : 'idle'}
                  label={rule.enabled ? t('series.active') : t('series.disabled')}
                />
              </button>
            </div>

            <div className={`${styles.ruleMeta} ${styles.textSecondary}`.trim()}>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>{t('series.channelLabel')}</span>
                <span className={styles.metaValue}>
                  {(() => {
                    if (!rule.channelRef) {
                      return <span>{t('series.allChannels')}</span>;
                    }
                    const matchedChannel = channels.find(c => matchServiceRef(c.serviceRef || c.id, rule.channelRef));
                    return (
                      <span className={styles.channelMetaValue}>
                        {matchedChannel?.logoUrl && (
                          <img src={matchedChannel.logoUrl} alt="" className={styles.channelMetaLogo} loading="lazy" />
                        )}
                        <span>{matchedChannel?.name || t('series.unknownChannel')}</span>
                      </span>
                    );
                  })()}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>{t('series.daysLabel')}</span>
                <span className={styles.metaValue}>
                  {formatRuleDays(rule.days, t)}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>{t('series.timeLabel')}</span>
                <span className={styles.metaValue}>{rule.startWindow || t('series.anytime')}</span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>{t('series.retentionLabel')}</span>
                <span className={styles.metaValue}>
                  {rule.retentionDays
                    ? t('series.retentionDaysAutoDelete', { count: rule.retentionDays })
                    : t('series.keepForever')}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>{t('series.validUntilLabel')}</span>
                <span className={styles.metaValue}>
                  {(() => {
                    const exp = formatRuleExpiry(rule.expiresAt);
                    if (!exp) return t('series.noExpiration');
                    if (exp.isExpired) {
                      return <span className={styles.expiredBadge}>{t('series.expiredBadge', { date: exp.dateStr })}</span>;
                    }
                    return t('series.untilBadge', { date: exp.dateStr });
                  })()}
                </span>
              </div>
            </div>

            <div className={styles.ruleStats}>
              {rule.lastRunAt ? (
                <div className={styles.lastRunInfo}>
                  <span>{t('series.lastRunLabel')} {new Date(rule.lastRunAt).toLocaleDateString()} {new Date(rule.lastRunAt).toLocaleTimeString()}</span>
                  <span
                    className={[
                      styles.runStatus,
                      rule.lastRunStatus === 'success' ? styles.runStatusSuccess : '',
                      rule.lastRunStatus === 'failed' ? styles.runStatusFailed : '',
                    ].filter(Boolean).join(' ')}
                  >
                    {rule.lastRunStatus === 'success'
                      ? t('series.runStatusSuccess')
                      : rule.lastRunStatus === 'failed'
                        ? t('series.runStatusFailed')
                        : (rule.lastRunStatus || t('series.unknownStatus'))}
                    {' '}({(() => {
                      const created = rule.lastRunSummary?.timersCreated || 0;
                      const pruned = rule.lastRunSummary?.recordingsPruned || 0;
                      if (pruned > 0) {
                        return t('series.runSummaryWithPruned', {
                          created,
                          pruned,
                        });
                      }
                      return t('series.runSummaryCreated', {
                        count: created,
                      });
                    })()})
                  </span>
                </div>
              ) : (
                <div className={styles.lastRunInfo}>{t('series.neverRun')}</div>
              )}
            </div>

            <div className={styles.ruleActions}>
              <Button
                variant={rule.enabled ? 'secondary' : 'primary'}
                onClick={() => handleToggleRule(rule)}
                className={styles.ruleAction}
                data-testid={`series-toggle-${rule.id}`}
              >
                {rule.enabled ? t('series.pause') : t('series.activate')}
              </Button>
              <Button
                variant="secondary"
                onClick={() => rule.id && handleRunNow(rule.id)}
                disabled={reportLoading === rule.id}
                className={styles.ruleAction}
              >
                {reportLoading === rule.id ? t('series.running') : t('series.runNow')}
              </Button>
              <Button
                variant="secondary"
                onClick={() => handleEdit(rule)}
                className={styles.ruleAction}
              >
                {t('common.edit')}
              </Button>
              <Button
                variant="danger"
                onClick={() => rule.id && handleDelete(rule.id)}
                className={styles.ruleAction}
              >
                {t('common.delete')}
              </Button>
            </div>
          </Card>
        ))}
      </div>

      {isEditing && currentRule && (
        <div className={styles.modalOverlay}>
          <div className={styles.modal}>
            <div className={styles.modalHeader}>
              <h1>{currentRule.id ? t('series.modal.editRule') : t('series.modal.newRule')}</h1>
              <button
                type="button"
                className={styles.closeButton}
                aria-label={t('common.close')}
                onClick={() => setIsEditing(false)}
              >
                ×
              </button>
            </div>

            <div className={styles.modalBody}>
              <div className={styles.formGroup}>
                <label className={styles.checkboxRow}>
                  <input
                    type="checkbox"
                    checked={currentRule.enabled}
                    onChange={e => setCurrentRule({ ...currentRule, enabled: e.target.checked })}
                    data-testid="series-edit-enabled"
                  />
                  <span>{t('series.modal.ruleActive')}</span>
                </label>
                <small className={styles.helpText}>{t('series.modal.ruleActiveHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.keywordLabel')}</label>
                <input
                  type="text"
                  value={currentRule.keyword}
                  onChange={e => setCurrentRule({ ...currentRule, keyword: e.target.value })}
                  placeholder={t('series.placeholder.keyword')}
                  className={styles.inputField}
                  data-testid="series-edit-keyword"
                />
                <small className={styles.helpText}>{t('series.modal.keywordHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.channelLabel')}</label>
                <select
                  value={selectedChannelValue}
                  onChange={e => setCurrentRule({ ...currentRule, channelRef: e.target.value })}
                  className={styles.inputField}
                  data-testid="series-edit-channel"
                >
                  <option value="">{t('series.modal.selectChannel')}</option>
                  {channels.map(c => (
                    <option key={c.id || c.serviceRef} value={c.serviceRef || c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
                <small className={styles.helpText}>{t('series.modal.channelHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.dayFilterLabel')}</label>
                <DaySelector
                  value={currentRule.days || []}
                  onChange={v => setCurrentRule({ ...currentRule, days: v })}
                />
                <small className={styles.helpText}>{t('series.modal.dayFilterHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.timeWindowLabel')}</label>
                <input
                  type="text"
                  value={currentRule.startWindow}
                  onChange={e => setCurrentRule({ ...currentRule, startWindow: e.target.value })}
                  placeholder={t('series.placeholder.timeWindow')}
                  className={styles.inputField}
                />
                <small className={styles.helpText}>{t('series.modal.timeWindowHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.retentionLabel')}</label>
                <div className={styles.chipRow}>
                  {[
                    { label: t('series.modal.retentionUnlimited'), val: 0 },
                    { label: t('series.modal.retention7Days'), val: 7 },
                    { label: t('series.modal.retention14Days'), val: 14 },
                    { label: t('series.modal.retention30Days'), val: 30 },
                  ].map(preset => (
                    <button
                      key={preset.val}
                      type="button"
                      className={[
                        styles.chip,
                        Number(currentRule.retentionDays) === preset.val ? styles.chipActive : '',
                      ].filter(Boolean).join(' ')}
                      onClick={() => setCurrentRule({ ...currentRule, retentionDays: preset.val })}
                    >
                      {preset.label}
                    </button>
                  ))}
                </div>
                <input
                  type="number"
                  min="0"
                  value={currentRule.retentionDays}
                  onChange={e => setCurrentRule({ ...currentRule, retentionDays: e.target.value })}
                  placeholder={t('series.placeholder.retention')}
                  className={styles.inputField}
                  data-testid="series-edit-retention"
                />
                <small className={styles.helpText}>{t('series.modal.retentionHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.expiresLabel')}</label>
                <div className={styles.chipRow}>
                  <button
                    type="button"
                    className={[styles.chip, !currentRule.expiresAt ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setCurrentRule({ ...currentRule, expiresAt: '' })}
                  >
                    {t('series.modal.expiresNever')}
                  </button>
                  <button
                    type="button"
                    className={[styles.chip, currentRule.expiresAt === `${new Date().getFullYear()}-12-31` ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => {
                      const year = new Date().getFullYear();
                      setCurrentRule({ ...currentRule, expiresAt: `${year}-12-31` });
                    }}
                  >
                    {t('series.modal.expiresYearEnd', { year: new Date().getFullYear() })}
                  </button>
                  <button
                    type="button"
                    className={styles.chip}
                    onClick={() => {
                      const d = new Date();
                      d.setMonth(d.getMonth() + 3);
                      setCurrentRule({ ...currentRule, expiresAt: formatLocalDateOnly(d) });
                    }}
                  >
                    {t('series.modal.expires3Months')}
                  </button>
                </div>
                <input
                  type="date"
                  value={currentRule.expiresAt || ''}
                  onChange={e => setCurrentRule({ ...currentRule, expiresAt: e.target.value })}
                  className={styles.inputField}
                  data-testid="series-edit-expires-at"
                />
                <small className={styles.helpText}>{t('series.modal.expiresHint')}</small>
              </div>

              <div className={styles.formGroup}>
                <label>{t('series.modal.priorityLabel')}</label>
                <input
                  type="number"
                  value={currentRule.priority}
                  onChange={e => setCurrentRule({ ...currentRule, priority: e.target.value })}
                  className={styles.inputField}
                />
              </div>
            </div>

            <div className={styles.modalFooter}>
              <Button variant="secondary" onClick={() => setIsEditing(false)}>
                {t('common.cancel')}
              </Button>
              <Button onClick={handleSave} data-testid="series-edit-save">
                {t('series.modal.saveButton')}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default SeriesManager;
