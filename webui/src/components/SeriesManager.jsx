import React, { useState, useEffect } from 'react';
import { SeriesService } from '../client/services/SeriesService';

export default function SeriesManager() {
  const [rules, setRules] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  // New Rule State
  const [newKeyword, setNewKeyword] = useState('');
  // const [newDays, setNewDays] = useState('7'); // TODO: Support Day Selection in v2.1
  const [showAdd, setShowAdd] = useState(false);

  useEffect(() => {
    loadRules();
  }, []);

  const loadRules = async () => {
    setLoading(true);
    try {
      const data = await SeriesService.getSeriesRules();
      setRules(data || []);
      setError(null);
    } catch (err) {
      console.error('Failed to load rules:', err);
      setError('Failed to load series rules');
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = async (e) => {
    e.preventDefault();
    if (!newKeyword.trim()) return;

    try {
      await SeriesService.createSeriesRule({
        keyword: newKeyword,
        enabled: true,
        days: [], // Empty = All days
        priority: 0,
      });
      setNewKeyword('');
      setShowAdd(false);
      loadRules();
    } catch (err) {
      console.error('Failed to create rule:', err);
      setError('Failed to create rule');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Delete this series rule?')) return;
    try {
      await SeriesService.deleteSeriesRule(id);
      loadRules();
    } catch (err) {
      console.error('Failed to delete rule:', err);
      setError('Failed to delete rule');
    }
  };

  const handleRun = async (id) => {
    try {
      // Trigger manual run
      await SeriesService.runSeriesRule(id, { trigger: 'manual' });
      // Reload rules to update status
      loadRules();
      // Optional: Show success toast or alert?
      // alert(`Run Complete: ${result.status} (Created ${result.summary?.timersCreated || 0})`);
    } catch (err) {
      console.error('Failed to run rule:', err);
      setError('Failed to run rule: ' + (err.body?.message || err.message));
    }
  };

  return (
    <div className="p-4">
      <div className="flex justify-between items-center mb-4">
        <h2 className="text-xl font-bold">Series Recording Rules</h2>
        <button
          className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1 rounded"
          onClick={() => setShowAdd(!showAdd)}
        >
          {showAdd ? 'Cancel' : '+ New Series'}
        </button>
      </div>

      {error && <div className="bg-red-900/50 text-red-200 p-2 rounded mb-4">{error}</div>}

      {showAdd && (
        <div className="bg-gray-800 p-4 rounded mb-4">
          <h3 className="font-bold mb-2">Add New Series Rule</h3>
          <form onSubmit={handleCreate}>
            <div className="mb-3">
              <label className="block text-sm text-gray-400 mb-1">Keyword (Title Match)</label>
              <input
                type="text"
                className="w-full bg-gray-900 border border-gray-700 rounded p-2 text-white placeholder-gray-600 focus:border-blue-500 outline-none"
                value={newKeyword}
                onChange={(e) => setNewKeyword(e.target.value)}
                placeholder="e.g. Tatort"
                autoFocus
              />
            </div>
            <div className="text-right">
              <button
                type="submit"
                disabled={!newKeyword}
                className="bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white px-4 py-2 rounded"
              >
                Create Rule
              </button>
            </div>
          </form>
        </div>
      )}

      <div className="grid gap-2">
        {loading && <div className="text-center text-gray-400">Loading rules...</div>}

        {!loading && rules.length === 0 && (
          <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded">
            No series rules defined. Add one to auto-record shows.
          </div>
        )}

        {rules.map(rule => (
          <div key={rule.id} className="bg-gray-800 p-3 rounded flex justify-between items-center">
            <div>
              <div className="font-bold text-lg">{rule.keyword}</div>
              <div className="flex gap-2 mt-1">
                <span className={`text-xs px-2 py-0.5 rounded ${rule.enabled ? 'bg-green-900 text-green-200' : 'bg-gray-700 text-gray-400'}`}>
                  {rule.enabled ? 'Active' : 'Disabled'}
                </span>
                <span className="text-xs text-gray-500 px-2 py-0.5">All Days</span>
              </div>
              {rule.lastRunAt && (
                <div className="mt-2 text-xs text-gray-400 flex gap-2 items-center">
                  <span>Last Run: {new Date(rule.lastRunAt).toLocaleString()}</span>
                  <span className={`uppercase font-bold ${rule.lastRunStatus === 'success' ? 'text-green-500' : 'text-yellow-500'}`}>
                    {rule.lastRunStatus}
                  </span>
                  {rule.lastRunSummary && (
                    <span className="text-gray-500 flex gap-2">
                      <span>(Created: {rule.lastRunSummary.timersCreated})</span>
                      {rule.lastRunSummary.receiverUnreachable && (
                        <span className="text-red-400 font-bold" title="Receiver Unreachable">ERR: Receiver</span>
                      )}
                      {rule.lastRunSummary.timersConflicted > 0 && (
                        <span className="text-orange-400 font-bold" title="Conflicts Detected">⚠ {rule.lastRunSummary.timersConflicted} Conflict(s)</span>
                      )}
                      {rule.lastRunSummary.timersErrored > 0 && (
                        <span className="text-red-400 font-bold" title="Processing Errors">⚠ {rule.lastRunSummary.timersErrored} Error(s)</span>
                      )}
                    </span>
                  )}
                </div>
              )}
            </div>
            <div className="flex gap-2">
              <button
                className="bg-blue-900/50 hover:bg-blue-900 text-blue-200 px-3 py-1 rounded text-sm disabled:opacity-50"
                onClick={() => handleRun(rule.id)}
                disabled={loading}
              >
                Run Now
              </button>
              <button
                className="bg-red-900/50 hover:bg-red-900 text-red-200 px-3 py-1 rounded text-sm"
                onClick={() => handleDelete(rule.id)}
              >
                Delete
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
