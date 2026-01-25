// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect } from 'react';
import { getTimers, deleteTimer, getDvrCapabilities, type Timer, type DvrCapabilities } from '../client-ts';
import EditTimerDialog from './EditTimerDialog';
import { debugError, debugWarn, formatError } from '../utils/logging';
import './Timers.css';

function formatDateTime(ts: number | undefined): string {
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
  const [timers, setTimers] = useState<Timer[]>([]);
  const [capabilities, setCapabilities] = useState<DvrCapabilities | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // Edit State
  const [editingTimer, setEditingTimer] = useState<Timer | null>(null);

  const fetchTimers = async (): Promise<void> => {
    setLoading(true);
    setError(null);
    try {
      const result = await getTimers();

      if (result.error) {
        setError("Failed to load timers. Ensure backend is running and authenticated.");
      } else if (result.data) {
        setTimers(result.data.items || []);
      }
    } catch (err) {
      debugError('Failed to load timers:', formatError(err));
      setError("Failed to load timers. Ensure backend is running and authenticated.");
    } finally {
      setLoading(false);
    }
  };

  const fetchCapabilities = async (): Promise<void> => {
    try {
      const result = await getDvrCapabilities();

      if (result.data) {
        setCapabilities(result.data);
      }
    } catch (err) {
      debugWarn('Failed to fetch capabilities', formatError(err));
    }
  };

  useEffect(() => {
    fetchTimers();
    fetchCapabilities();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleDelete = async (timer: Timer): Promise<void> => {
    if (!confirm(`Delete timer for "${timer.name}"?`)) return;

    try {
      // Use timerId for v2 delete
      if (timer.timerId) {
        const result = await deleteTimer({ path: { timerId: timer.timerId } });

        if (result.error) {
          throw new Error('Failed to delete timer');
        }
        fetchTimers();
      } else {
        debugWarn('No timerId found');
      }
    } catch (err) {
      const error = err as Error;
      alert("Failed to delete timer: " + error.message);
    }
  };

  return (
    <div className="timers-view">
      <div className="timers-toolbar">
        <h2>Scheduled Recordings</h2>
        <button onClick={fetchTimers} className="timer-refresh-btn">Refresh</button>
      </div>

      {loading && <div className="timers-loading">Loading...</div>}
      {error && <div className="status-indicator error w-full justify-center">{error}</div>}

      {!loading && !error && timers.length === 0 && (
        <div className="timers-empty">No timers scheduled.</div>
      )}

      <div className="timers-list">
        {timers.map((t, idx) => (
          <div key={t.timerId || idx} className="timer-card">
            <div className="timer-info">
              <div className="timer-name">{t.name}</div>
              <div className="timer-service">{t.serviceName || t.serviceRef}</div>
              <div className="timer-time">
                {formatDateTime(t.begin)} - {formatDateTime(t.end)}
              </div>
              <div className={`timer-state ${t.state === 'recording' ? 'status-recording' : 'status-scheduled'}`}>
                {t.state}
              </div>
            </div>
            <div className="timer-actions">
              <button
                onClick={() => setEditingTimer(t)}
                className="timer-btn btn-edit"
              >
                Edit
              </button>
              <button
                onClick={() => handleDelete(t)}
                className="timer-btn btn-delete"
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
          capabilities={capabilities || undefined}
          onClose={() => setEditingTimer(null)}
          onSave={fetchTimers}
        />
      )}
    </div>
  );
}
