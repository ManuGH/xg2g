// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import {
  getSeriesRules,
  deleteSeriesRule,
  createSeriesRule,
  // updateSeriesRule, // Missing in SDK
  runSeriesRule,
  getServices,
  type SeriesRule,
  type Service,
  type SeriesRuleWritable,
  // type SeriesRuleUpdate // Missing in SDK
} from '../client-ts';
import { debugError, formatError } from '../utils/logging';
import { useUiOverlay } from '../context/UiOverlayContext';
import { Button, Card, StatusChip } from './ui';
import styles from './SeriesManager.module.css';

interface DaySelectorProps {
  value: number[];
  onChange: (value: number[]) => void;
}

// Helper component for Day Selection
const DaySelector = ({ value, onChange }: DaySelectorProps) => {
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const toggleDay = (dayIndex: number) => {
    const newValue = value.includes(dayIndex)
      ? value.filter(d => d !== dayIndex)
      : [...value, dayIndex].sort();
    onChange(newValue);
  };

  return (
    <div className={styles.daySelector}>
      {days.map((d, i) => (
        <button
          key={i}
          className={[
            styles.dayButton,
            value.includes(i) ? styles.dayButtonActive : '',
          ].filter(Boolean).join(' ')}
          onClick={() => toggleDay(i)}
          type="button"
        >
          {d}
        </button>
      ))}
    </div>
  );
};

// Use definition from SDK which is intersection (SeriesRule & SeriesRuleWritable) or separate?
// Looking at types.gen.ts, SeriesRuleWritable has the editable fields.
// For the form state, we need a shape that matches what we edit.

interface RuleFormState {
  id?: string;
  keyword: string;
  channelRef: string;
  days: number[];
  startWindow: string;
  priority: number | string; // Handle input string temporarily
  enabled: boolean;
}

function SeriesManager() {
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
        enabled: rule.enabled !== false
      });
    } else {
      setCurrentRule({
        keyword: '',
        channelRef: '',
        days: [],
        startWindow: '',
        priority: 0,
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
      // Fix: id/ruleId key name depends on SDK. 
      // User says: "DeleteSeriesRuleData: path: { id: string }"
      await deleteSeriesRule({ path: { id } });
      loadRules();
    } catch (err: any) {
      toast({ kind: 'error', message: 'Failed to delete rule', details: err.message || 'Unknown error' });
    }
  };

  const handleSave = async () => {
    if (!currentRule) return;

    try {
      if (!currentRule.keyword) {
        toast({ kind: 'warning', message: 'Keyword is required' });
        return;
      }

      // Prepare payload
      if (currentRule.id) {
        toast({
          kind: 'warning',
          message: 'Update not implemented in this version (SDK mismatch). Please delete and recreate.',
        });
        return;
        /*
        // Update existing rule
        const updatePayload: SeriesRuleUpdate = {
          enabled: currentRule.enabled,
          keyword: currentRule.keyword,
          priority: Number(currentRule.priority) || 0,
          ...(currentRule.channelRef?.trim() ? { channelRef: currentRule.channelRef.trim() } : {}),
          ...(currentRule.startWindow?.trim() ? { startWindow: currentRule.startWindow.trim() } : {}),
          ...(currentRule.days?.length ? { days: currentRule.days } : {})
        };

        await updateSeriesRule({
          path: { id: currentRule.id },
          body: updatePayload
        });
        */
      } else {
        // UI-INV-SERIES-001: Omit empty filters to avoid unnecessary state synthesis.
        const createPayload: SeriesRuleWritable = {
          keyword: currentRule.keyword,
          priority: Number(currentRule.priority) || 0,
          enabled: currentRule.enabled,
          ...(currentRule.channelRef?.trim() ? { channelRef: currentRule.channelRef.trim() } : {}),
          ...(currentRule.days?.length ? { days: currentRule.days } : {}),
          ...(currentRule.startWindow?.trim() ? { startWindow: currentRule.startWindow.trim() } : {})
        };

        await createSeriesRule({ body: createPayload });
      }
      setIsEditing(false);
      loadRules();
    } catch (err: any) {
      toast({ kind: 'error', message: 'Failed to save rule', details: err.message || 'Unknown error' });
    }
  };

  const handleRunNow = async (id: string) => {
    setReportLoading(id);
    try {
      // Fix: Params are path + query
      const response = await runSeriesRule({
        path: { id },
        query: { trigger: 'manual' }
      });
      const report = response.data;
      if (report) {
        toast({
          kind: 'success',
          message: 'Run complete',
          details: `Matched: ${report.summary?.epgItemsMatched ?? 0} | Created: ${report.summary?.timersCreated ?? 0} | Errors: ${report.summary?.timersErrored ?? 0}`,
        });
      }
      loadRules();
    } catch (err: any) {
      toast({ kind: 'error', message: 'Run failed', details: err.message || 'Unknown error' });
    } finally {
      setReportLoading(false);
    }
  };

  if (loading && !rules.length) return <div className={styles.loadingState}>Loading Rules...</div>;

  return (
    <div className={`${styles.container} animate-enter`.trim()}>
      <div className={styles.header}>
        <h2>Series Recording Rules</h2>
        <Button onClick={() => handleEdit(null)} data-testid="series-add-btn">
          + New Rule
        </Button>
      </div>

      <div className={styles.grid}>
        {rules.map(rule => (
          <Card key={rule.id} className={styles.ruleCard}>
            <div className={styles.ruleHeader}>
              <h3>{rule.keyword}</h3>
              <StatusChip
                state={rule.enabled ? 'success' : 'idle'}
                label={rule.enabled ? 'ACTIVE' : 'DISABLED'}
              />
            </div>

            <div className={`${styles.ruleMeta} ${styles.textSecondary}`.trim()}>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Channel:</span>
                <span className={styles.metaValue}>{rule.channelRef ? (channels.find(c => (c.serviceRef || c.id) === rule.channelRef)?.name || rule.channelRef) : 'All Channels'}</span>

              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Days:</span>
                <span className={styles.metaValue}>
                  {rule.days?.length
                    ? rule.days.map(d => ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d]).join(', ')
                    : 'Everyday'}
                </span>
              </div>
              <div className={styles.metaRow}>
                <span className={styles.metaLabel}>Time:</span>
                <span className={styles.metaValue}>{rule.startWindow || 'Anytime'}</span>
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
                    {rule.lastRunStatus || 'Unknown'} ({(rule.lastRunSummary?.timersCreated || 0)} Created)
                  </span>
                </div>
              ) : (
                <div className={styles.lastRunInfo}>Never Run</div>
              )}
            </div>

            <div className={styles.ruleActions}>
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
              <h2>{currentRule.id ? 'Edit Rule' : 'New Series Rule'}</h2>
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
                  <option value="">-- All Channels (Slower) --</option>
                  {channels.map(c => (
                    <option key={c.id || c.serviceRef} value={c.serviceRef || c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
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
