import React, { useState, useEffect } from 'react';
import { TimersService, DvrService, OpenAPI } from '../client';
import EditTimerDialog from './EditTimerDialog';

function formatDateTime(ts) {
  if (!ts) return '';
  const d = new Date(ts * 1000);
  return d.toLocaleString([], {
    weekday: 'short',
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  });
}

export default function Timers() {
  const [timers, setTimers] = useState([]);
  const [capabilities, setCapabilities] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  // Edit State
  const [editingTimer, setEditingTimer] = useState(null);

  const fetchTimers = async () => {
    setLoading(true);
    setError(null);
    try {
      const token = localStorage.getItem('XG2G_API_TOKEN');
      if (token) OpenAPI.TOKEN = token;

      const data = await TimersService.getTimers();
      // data is TimerList, so data.items
      setTimers(data.items || []);
    } catch (err) {
      console.error("Failed to load timers:", err);
      setError("Failed to load timers. Ensure backend is running and authenticated.");
    } finally {
      setLoading(false);
    }
  };

  const fetchCapabilities = async () => {
    try {
      const caps = await DvrService.getDvrCapabilities();
      setCapabilities(caps);
    } catch (err) {
      console.warn("Failed to fetch capabilities", err);
    }
  };

  useEffect(() => {
    fetchTimers();
    fetchCapabilities();
  }, []);

  const handleDelete = async (timer) => {
    if (!confirm(`Delete timer for "${timer.name}"?`)) return;

    try {
      // Use timerId for v2 delete
      if (timer.timerId) {
        await TimersService.deleteTimer(timer.timerId);
      } else {
        // Fallback for legacy (shouldn't happen with v2 API)
        console.warn("No timerId found, try legacy?");
      }
      fetchTimers();
    } catch (err) {
      alert("Failed to delete timer: " + err.message);
    }
  };

  return (
    <div className="timers-view p-4">
      <div className="flex justify-between items-center mb-4">
        <h2 className="text-xl font-bold">Scheduled Recordings</h2>
        <button onClick={fetchTimers} className="bg-gray-700 px-3 py-1 rounded">Refresh</button>
      </div>

      {loading && <div className="text-center">Loading...</div>}
      {error && <div className="text-red-500">{error}</div>}

      {!loading && !error && timers.length === 0 && (
        <div className="text-gray-400 text-center py-8">No timers scheduled.</div>
      )}

      <div className="grid gap-2">
        {timers.map((t, idx) => (
          <div key={t.timerId || idx} className="bg-gray-800 p-3 rounded flex justify-between items-center">
            <div>
              <div className="font-bold">{t.name}</div>
              <div className="text-sm text-gray-400">{t.serviceName || t.serviceRef}</div>
              <div className="text-xs text-gray-500">
                {formatDateTime(t.begin)} - {formatDateTime(t.end)}
              </div>
              <div className={`text-xs mt-1 ${t.state === 'recording' ? 'text-green-500' : 'text-yellow-500'}`}>
                {t.state}
              </div>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setEditingTimer(t)}
                className="bg-blue-900/50 text-blue-300 hover:bg-blue-900 px-3 py-1 rounded text-sm"
              >
                Edit
              </button>
              <button
                onClick={() => handleDelete(t)}
                className="bg-red-900/50 text-red-300 hover:bg-red-900 px-3 py-1 rounded text-sm"
              >
                Delete
              </button>
            </div>
          </div>
        ))}
      </div>

      {editingTimer && (
        <EditTimerDialog
          timer={editingTimer}
          capabilities={capabilities}
          onClose={() => setEditingTimer(null)}
          onSave={fetchTimers}
        />
      )}
    </div>
  );
}
