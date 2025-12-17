import { useState, useEffect } from 'react';
import { SeriesService } from '../client/services/SeriesService';
import { DefaultService } from '../client/services/DefaultService';
import './SeriesManager.css';

function SeriesManager() {
  const [rules, setRules] = useState([]);
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [currentRule, setCurrentRule] = useState(null);
  const [reportLoading, setReportLoading] = useState(false);

  // Load Initial Data
  useEffect(() => {
    loadRules();
    loadChannels();
  }, []);

  const loadRules = async () => {
    setLoading(true);
    try {
      const data = await SeriesService.getSeriesRules();
      setRules(data || []);
    } catch (err) {
      console.error('Failed to load rules:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadChannels = async () => {
    try {
      const data = await DefaultService.getServices({ bouquet: '' });
      setChannels(data || []);
    } catch (err) {
      console.error('Failed to load channels:', err);
    }
  };

  const handleEdit = (rule) => {
    setCurrentRule(rule ? { ...rule } : {
      keyword: '',
      channel_ref: '',
      days: [],
      start_window: '',
      priority: 0,
      enabled: true
    });
    setIsEditing(true);
  };

  const handleDelete = async (id) => {
    if (!window.confirm('Are you sure you want to delete this rule?')) return;
    try {
      await SeriesService.deleteSeriesRule(id);
      loadRules();
    } catch (err) {
      alert('Failed to delete rule: ' + err.message);
    }
  };

  const handleSave = async () => {
    try {
      if (!currentRule.keyword) {
        alert("Keyword is required");
        return;
      }

      // Prepare payload (ensure types)
      const payload = {
        ...currentRule,
        days: currentRule.days || [],
        priority: parseInt(currentRule.priority) || 0
      };

      await SeriesService.createSeriesRule(payload);
      setIsEditing(false);
      loadRules();
    } catch (err) {
      alert('Failed to save rule: ' + err.message);
    }
  };

  const handleRunNow = async (id) => {
    setReportLoading(id);
    try {
      const report = await SeriesService.runSeriesRule(id, { trigger: 'manual' });
      alert(`Run Complete!\nMatched: ${report.summary?.epgItemsMatched}\nCreated: ${report.summary?.timersCreated}\nErrors: ${report.summary?.timersErrored}`);
      loadRules();
    } catch (err) {
      alert('Run failed: ' + err.message);
    } finally {
      setReportLoading(false);
    }
  };

  // Helper component for Day Selection
  const DaySelector = ({ value, onChange }) => {
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const toggleDay = (dayIndex) => {
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
                <span className="value">{rule.channel_ref ? (channels.find(c => c.ref === rule.channel_ref || c.service_ref === rule.channel_ref)?.name || rule.channel_ref) : 'All Channels'}</span>
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
                onClick={() => handleRunNow(rule.id)}
                disabled={reportLoading === rule.id}
              >
                {reportLoading === rule.id ? 'Running...' : 'Run Now'}
              </button>
              <button className="btn-secondary" onClick={() => handleEdit(rule)}>Edit</button>
              <button className="btn-danger" onClick={() => handleDelete(rule.id)}>Delete</button>
            </div>
          </div>
        ))}
      </div>

      {isEditing && (
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
                      <option key={c.ref || c.service_ref} value={c.ref || c.service_ref}>
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
