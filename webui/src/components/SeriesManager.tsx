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
import './SeriesManager.css';

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
    <div className="day-selector">
      {days.map((d, i) => (
        <button
          key={i}
          className={`day-btn ${value.includes(i) ? 'active' : ''}`}
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
  channel_ref: string;
  days: number[];
  start_window: string;
  priority: number | string; // Handle input string temporarily
  enabled: boolean;
}

function SeriesManager() {
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
      console.error('Failed to load rules:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadChannels = async () => {
    try {
      const response = await getServices({ query: { bouquet: '' } });
      setChannels(response.data || []);
    } catch (err) {
      console.error('Failed to load channels:', err);
    }
  };

  const handleEdit = (rule: SeriesRule | null) => {
    if (rule) {
      setCurrentRule({
        id: rule.id,
        keyword: rule.keyword || '',
        channel_ref: rule.channel_ref || '',
        days: rule.days || [],
        start_window: rule.start_window || '',
        priority: rule.priority || 0,
        enabled: rule.enabled !== false
      });
    } else {
      setCurrentRule({
        keyword: '',
        channel_ref: '',
        days: [],
        start_window: '',
        priority: 0,
        enabled: true
      });
    }
    setIsEditing(true);
  };

  const handleDelete = async (id: string) => {
    if (!window.confirm('Are you sure you want to delete this rule?')) return;
    try {
      // Fix: id/ruleId key name depends on SDK. 
      // User says: "DeleteSeriesRuleData: path: { id: string }"
      await deleteSeriesRule({ path: { id } });
      loadRules();
    } catch (err: any) {
      alert('Failed to delete rule: ' + (err.message || 'Unknown error'));
    }
  };

  const handleSave = async () => {
    if (!currentRule) return;

    try {
      if (!currentRule.keyword) {
        alert("Keyword is required");
        return;
      }

      // Prepare payload
      if (currentRule.id) {
        alert("Update not implemented in this version (SDK mismatch). Please delete and recreate.");
        /*
        // Update existing rule
        const updatePayload: SeriesRuleUpdate = {
          enabled: currentRule.enabled,
          keyword: currentRule.keyword,
          priority: Number(currentRule.priority) || 0,
          ...(currentRule.channel_ref?.trim() ? { channel_ref: currentRule.channel_ref.trim() } : {}),
          ...(currentRule.start_window?.trim() ? { start_window: currentRule.start_window.trim() } : {}),
          ...(currentRule.days?.length ? { days: currentRule.days } : {})
        };

        await updateSeriesRule({
          path: { id: currentRule.id },
          body: updatePayload
        });
        */
      } else {
        // Create new rule
        const createPayload: SeriesRuleWritable = {
          keyword: currentRule.keyword,
          channel_ref: currentRule.channel_ref,
          days: currentRule.days || [],
          start_window: currentRule.start_window,
          priority: Number(currentRule.priority) || 0,
          enabled: currentRule.enabled
        };

        await createSeriesRule({ body: createPayload });
      }
      setIsEditing(false);
      loadRules();
    } catch (err: any) {
      alert('Failed to save rule: ' + (err.message || 'Unknown Error'));
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
        alert(`Run Complete!\nMatched: ${report.summary?.epgItemsMatched}\nCreated: ${report.summary?.timersCreated}\nErrors: ${report.summary?.timersErrored}`);
      }
      loadRules();
    } catch (err: any) {
      alert('Run failed: ' + (err.message || 'Unknown Error'));
    } finally {
      setReportLoading(false);
    }
  };

  if (loading && !rules.length) return <div className="loading-state">Loading Rules...</div>;

  return (
    <div className="series-manager">
      <div className="sm-header">
        <h2>Series Recording Rules</h2>
        <button className="btn-primary" onClick={() => handleEdit(null)}>
          + New Rule
        </button>
      </div>

      <div className="card-grid">
        {rules.map(rule => (
          <div key={rule.id} className="card rule-card">
            <div className="rule-header">
              <h3>{rule.keyword}</h3>
              <span className={`status-badge ${rule.enabled ? 'active' : 'inactive'}`}>
                {rule.enabled ? 'Active' : 'Disabled'}
              </span>
            </div>

            <div className="rule-meta text-secondary">
              <div className="meta-row">
                <span className="label">Channel:</span>
                <span className="value">{rule.channel_ref ? (channels.find(c => (c.service_ref || c.id) === rule.channel_ref)?.name || rule.channel_ref) : 'All Channels'}</span>

              </div>
              <div className="meta-row">
                <span className="label">Days:</span>
                <span className="value">
                  {rule.days?.length
                    ? rule.days.map(d => ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d]).join(', ')
                    : 'Everyday'}
                </span>
              </div>
              <div className="meta-row">
                <span className="label">Time:</span>
                <span className="value">{rule.start_window || 'Anytime'}</span>
              </div>
            </div>

            <div className="rule-stats">
              {rule.lastRunAt ? (
                <div className="last-run-info">
                  <span>Last Run: {new Date(rule.lastRunAt).toLocaleDateString()} {new Date(rule.lastRunAt).toLocaleTimeString()}</span>
                  <span className={`run-status ${rule.lastRunStatus}`}>
                    {rule.lastRunStatus || 'Unknown'} ({(rule.lastRunSummary?.timersCreated || 0)} Created)
                  </span>
                </div>
              ) : (
                <div className="last-run-info">Never Run</div>
              )}
            </div>

            <div className="rule-actions">
              <button
                className="btn-secondary"
                onClick={() => rule.id && handleRunNow(rule.id)}
                disabled={reportLoading === rule.id}
              >
                {reportLoading === rule.id ? 'Running...' : 'Run Now'}
              </button>
              <button className="btn-secondary" onClick={() => handleEdit(rule)}>Edit</button>
              <button className="btn-danger" onClick={() => rule.id && handleDelete(rule.id)}>Delete</button>
            </div>
          </div>
        ))}
      </div>

      {isEditing && currentRule && (
        <div className="modal-overlay">
          <div className="modal glass">
            <div className="modal-header">
              <h2>{currentRule.id ? 'Edit Rule' : 'New Series Rule'}</h2>
              <button className="close-btn" onClick={() => setIsEditing(false)}>×</button>
            </div>

            <div className="modal-body">
              <div className="form-group">
                <label>Keyword (Title Match)</label>
                <input
                  type="text"
                  value={currentRule.keyword}
                  onChange={e => setCurrentRule({ ...currentRule, keyword: e.target.value })}
                  placeholder="e.g. Tatort"
                  className="input-field"
                />
                <small>Case-insensitive partial match on program title.</small>
              </div>

              <div className="form-group">
                <label>Channel</label>
                <div className="select-wrapper">
                  <select
                    value={currentRule.channel_ref}
                    onChange={e => setCurrentRule({ ...currentRule, channel_ref: e.target.value })}
                    className="input-field"
                  >
                    <option value="">-- All Channels (Slower) --</option>
                    {channels.map(c => (
                      <option key={c.id || c.service_ref} value={c.service_ref || c.id}>

                        {c.name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="form-group">
                <label>Day Filter</label>
                <DaySelector
                  value={currentRule.days || []}
                  onChange={v => setCurrentRule({ ...currentRule, days: v })}
                />
                <small>Select specific days to record. Empty = Any day.</small>
              </div>

              <div className="form-group">
                <label>Time Window (HHMM-HHMM)</label>
                <input
                  type="text"
                  value={currentRule.start_window}
                  onChange={e => setCurrentRule({ ...currentRule, start_window: e.target.value })}
                  placeholder="e.g. 2015-2200"
                  className="input-field"
                />
                <small>Only match start times within this range.</small>
              </div>

              <div className="form-group">
                <label>Priority</label>
                <input
                  type="number"
                  value={currentRule.priority}
                  onChange={e => setCurrentRule({ ...currentRule, priority: e.target.value })}
                  className="input-field"
                />
              </div>
            </div>

            <div className="modal-footer">
              <button className="btn-secondary" onClick={() => setIsEditing(false)}>Cancel</button>
              <button className="btn-primary" onClick={handleSave}>Confirm & Save</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default SeriesManager;
