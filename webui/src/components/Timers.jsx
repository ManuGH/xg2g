// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useState, useEffect } from 'react';
import { TimersService, DvrService, OpenAPI } from '../client';
import EditTimerDialog from './EditTimerDialog';
import './Timers.css';

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
          capabilities={capabilities}
          onClose={() => setEditingTimer(null)}
          onSave={fetchTimers}
        />
      )}
    </div>
  );
}
