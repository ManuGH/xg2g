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
import { ROUTE_MAP } from '../routes';
import { Button, Card, StatusChip } from './ui';
import LegacyRouteNotice from './LegacyRouteNotice';
import styles from './SeriesManager.module.css';

interface SeriesManagerProps {
  showLegacyNotice?: boolean;
}

const DAY_OPTIONS = [
  { day: 1, label: 'Mo' },
  { day: 2, label: 'Di' },
  { day: 3, label: 'Mi' },
  { day: 4, label: 'Do' },
  { day: 5, label: 'Fr' },
  { day: 6, label: 'Sa' },
  { day: 0, label: 'So' },
];

const DAY_PRESETS = [
  { label: 'Täglich', days: [0, 1, 2, 3, 4, 5, 6] },
  { label: 'Werktags (Mo-Fr)', days: [1, 2, 3, 4, 5] },
  { label: 'Wochenende (Sa-So)', days: [0, 6] },
  { label: 'Alle Tage (Kein Filter)', days: [] },
];

interface DaySelectorProps {
  value: number[];
  onChange: (value: number[]) => void;
}

// Helper component for Day Selection with quick presets and day buttons
const DaySelector = ({ value, onChange }: DaySelectorProps) => {
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
        {DAY_PRESETS.map((preset) => (
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
        {DAY_OPTIONS.map((opt) => (
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

const formatRuleDays = (days?: number[]) => {
  if (!days || days.length === 0 || days.length === 7) return 'Täglich (Alle Tage)';
  if (days.length === 5 && [1, 2, 3, 4, 5].every(d => days.includes(d))) return 'Werktags (Mo–Fr)';
  if (days.length === 2 && [0, 6].every(d => days.includes(d))) return 'Wochenende (Sa–So)';
  const dayNames: Record<number, string> = { 1: 'Mo', 2: 'Di', 3: 'Mi', 4: 'Do', 5: 'Fr', 6: 'Sa', 0: 'So' };
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
  priority: number | string; // Handle input string temporarily
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
      // SDK returns { data: SeriesRule[] }
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
      title: 'Delete Rule',
      message: 'Are you sure you want to delete this rule?',
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
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
      toast({ kind: 'error', message: 'Failed to delete rule', details: err.message || 'Unknown error' });
    }
  };

  const handleSave = async () => {
    if (!currentRule) return;

    try {
      if (!currentRule.keyword?.trim()) {
        toast({ kind: 'warning', message: 'Keyword is required' });
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
      toast({ kind: 'error', message: 'Failed to save rule', details: err.message || 'Unknown error' });
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
          ? t('series.ruleActivated', { defaultValue: 'Regel aktiviert' })
          : t('series.rulePaused', { defaultValue: 'Regel pausiert' }),
      });
      await loadRules();
    } catch (err: any) {
      toast({
        kind: 'error',
        message: t('series.toggleFailed', { defaultValue: 'Status konnte nicht geändert werden' }),
        details: err.message || 'Unknown error',
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
          message: 'Run complete',
          details: `Matched: ${report.summary?.epgItemsMatched ?? 0} | Created: ${report.summary?.timersCreated ?? 0} | Errors: ${report.summary?.timersErrored ?? 0}`,
        });
      }
      await loadRules();
    } catch (err: any) {
      toast({ kind: 'error', message: 'Run failed', details: err.message || 'Unknown error' });
    } finally {
      setReportLoading(false);
    }
  };

  if (loading && !rules.length) return <div className={styles.loadingState}>Loading Rules...</div>;

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      {showLegacyNotice ? (
        <LegacyRouteNotice
          parentLabel={t('nav.recordings')}
          description={t('legacyRoute.seriesDescription', {
            defaultValue: 'Series rules remain available as an expert workflow. For most DVR browsing, start in Recordings.',
          })}
          route={ROUTE_MAP.recordings}
        />
      ) : null}
      <div className={styles.header}>
        <h1>Series Recording Rules</h1>
        <Button onClick={() => handleEdit(null)} data-testid="series-add-btn">
          + New Rule
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
                title={rule.enabled ? 'Klicken zum Pausieren' : 'Klicken zum Aktivieren'}
                aria-label={rule.enabled ? 'Pausieren' : 'Aktivieren'}
              >
                <StatusChip
                  state={rule.enabled ? 'success' : 'idle'}
                  label={rule.enabled ? 'ACTIVE' : 'DISABLED'}
                />
              </button>
            </div>

            <div className={`${styles.ruleMeta} ${styles.textSecondary}`.trim()}>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Channel:</span>
                <span className={styles.metaValue}>{rule.channelRef ? (channels.find(c => (c.serviceRef || c.id) === rule.channelRef)?.name || rule.channelRef) : 'All Channels'}</span>

              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Days:</span>
                <span className={styles.metaValue}>
                  {formatRuleDays(rule.days)}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Time:</span>
                <span className={styles.metaValue}>{rule.startWindow || 'Anytime'}</span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Retention:</span>
                <span className={styles.metaValue}>
                  {rule.retentionDays ? `${rule.retentionDays} days (auto-delete)` : 'Keep forever'}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Valid Until:</span>
                <span className={styles.metaValue}>
                  {(() => {
                    const exp = formatRuleExpiry(rule.expiresAt);
                    if (!exp) return 'No expiration (runs forever)';
                    if (exp.isExpired) {
                      return <span className={styles.expiredBadge}>Expired ({exp.dateStr})</span>;
                    }
                    return `Until ${exp.dateStr}`;
                  })()}
                </span>
              </div>
            </div>

            <div className={styles.ruleStats}>
              {rule.lastRunAt ? (
                <div className={styles.lastRunInfo}>
                  <span>Last Run: {new Date(rule.lastRunAt).toLocaleDateString()} {new Date(rule.lastRunAt).toLocaleTimeString()}</span>
                  <span
                    className={[
                      styles.runStatus,
                      rule.lastRunStatus === 'success' ? styles.runStatusSuccess : '',
                      rule.lastRunStatus === 'failed' ? styles.runStatusFailed : '',
                    ].filter(Boolean).join(' ')}
                  >
                    {rule.lastRunStatus || 'Unknown'} ({(rule.lastRunSummary?.timersCreated || 0)} Created{rule.lastRunSummary?.recordingsPruned ? `, ${rule.lastRunSummary.recordingsPruned} Pruned` : ''})
                  </span>
                </div>
              ) : (
                <div className={styles.lastRunInfo}>Never Run</div>
              )}
            </div>

            <div className={styles.ruleActions}>
              <Button
                variant={rule.enabled ? 'secondary' : 'primary'}
                onClick={() => handleToggleRule(rule)}
                className={styles.ruleAction}
                data-testid={`series-toggle-${rule.id}`}
              >
                {rule.enabled ? 'Pause' : 'Activate'}
              </Button>
              <Button
                variant="secondary"
                onClick={() => rule.id && handleRunNow(rule.id)}
                disabled={reportLoading === rule.id}
                className={styles.ruleAction}
              >
                {reportLoading === rule.id ? 'Running...' : 'Run Now'}
              </Button>
              <Button
                variant="secondary"
                onClick={() => handleEdit(rule)}
                className={styles.ruleAction}
              >
                Edit
              </Button>
              <Button
                variant="danger"
                onClick={() => rule.id && handleDelete(rule.id)}
                className={styles.ruleAction}
              >
                Delete
              </Button>
            </div>
          </Card>
        ))}
      </div>

      {isEditing && currentRule && (
        <div className={styles.modalOverlay}>
          <div className={styles.modal}>
            <div className={styles.modalHeader}>
              <h1>{currentRule.id ? 'Edit Rule' : 'New Series Rule'}</h1>
              <button
                type="button"
                className={styles.closeButton}
                aria-label="Close"
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
                  <span>Regel aktiv</span>
                </label>
                <small className={styles.helpText}>Wenn pausiert, ignoriert der automatische Scheduler diese Regel.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Keyword (Title Match)</label>
                <input
                  type="text"
                  value={currentRule.keyword}
                  onChange={e => setCurrentRule({ ...currentRule, keyword: e.target.value })}
                  placeholder="e.g. Tatort"
                  className={styles.inputField}
                  data-testid="series-edit-keyword"
                />
                <small className={styles.helpText}>Case-insensitive partial match on program title.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Channel</label>
                <select
                  value={currentRule.channelRef}
                  onChange={e => setCurrentRule({ ...currentRule, channelRef: e.target.value })}
                  className={styles.inputField}
                >
                  <option value="">-- Select Channel --</option>
                  {channels.map(c => (
                    <option key={c.id || c.serviceRef} value={c.serviceRef || c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
                <small className={styles.helpText}>Required. The channel monitored for this series.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Day Filter</label>
                <DaySelector
                  value={currentRule.days || []}
                  onChange={v => setCurrentRule({ ...currentRule, days: v })}
                />
                <small className={styles.helpText}>Select specific days to record. Empty = Any day.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Time Window (HHMM-HHMM)</label>
                <input
                  type="text"
                  value={currentRule.startWindow}
                  onChange={e => setCurrentRule({ ...currentRule, startWindow: e.target.value })}
                  placeholder="e.g. 2015-2200"
                  className={styles.inputField}
                />
                <small className={styles.helpText}>Only match start times within this range.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Retention / Aufbewahrung (Tage)</label>
                <div className={styles.chipRow}>
                  {[
                    { label: 'Unbegrenzt', val: 0 },
                    { label: '7 Tage (z.B. Cafe Puls)', val: 7 },
                    { label: '14 Tage', val: 14 },
                    { label: '30 Tage', val: 30 },
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
                  placeholder="0 = dauerhaft behalten"
                  className={styles.inputField}
                  data-testid="series-edit-retention"
                />
                <small className={styles.helpText}>Aufnahmen, die älter als diese Anzahl an Tagen sind, werden automatisch gelöscht.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Gültig bis (Ablaufdatum / EOL)</label>
                <div className={styles.chipRow}>
                  <button
                    type="button"
                    className={[styles.chip, !currentRule.expiresAt ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => setCurrentRule({ ...currentRule, expiresAt: '' })}
                  >
                    Dauerhaft (Kein Ablauf)
                  </button>
                  <button
                    type="button"
                    className={[styles.chip, currentRule.expiresAt === `${new Date().getFullYear()}-12-31` ? styles.chipActive : ''].filter(Boolean).join(' ')}
                    onClick={() => {
                      const year = new Date().getFullYear();
                      setCurrentRule({ ...currentRule, expiresAt: `${year}-12-31` });
                    }}
                  >
                    Bis Jahresende ({new Date().getFullYear()})
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
                    +3 Monate
                  </button>
                </div>
                <input
                  type="date"
                  value={currentRule.expiresAt || ''}
                  onChange={e => setCurrentRule({ ...currentRule, expiresAt: e.target.value })}
                  className={styles.inputField}
                  data-testid="series-edit-expires-at"
                />
                <small className={styles.helpText}>Optional. Nach diesem Datum werden keine neuen Sendungen mehr programmiert.</small>
              </div>

              <div className={styles.formGroup}>
                <label>Priority</label>
                <input
                  type="number"
                  value={currentRule.priority}
                  onChange={e => setCurrentRule({ ...currentRule, priority: e.target.value })}
                  className={styles.inputField}
                />
              </div>
            </div>

            <div className={styles.modalFooter}>
              <Button variant="secondary" onClick={() => setIsEditing(false)}>Cancel</Button>
              <Button onClick={handleSave} data-testid="series-edit-save">
                Confirm & Save
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default SeriesManager;
