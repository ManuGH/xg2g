// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import React, { useState, useEffect, useRef } from 'react';
import { TimersService, DvrService } from '../client-ts';
import './EditTimerDialog.css';

export default function EditTimerDialog({ timer, onClose, onSave, capabilities }) {
  const [formData, setFormData] = useState({
    name: timer.name || '',
    description: timer.description || '',
    begin: timer.begin,
    end: timer.end,
    enabled: timer.state !== 'disabled',
    // Padding logic: simple absolute times as per API
  });

  const [conflict, setConflict] = useState(null); // { conflicts: [], canSchedule: boolean }
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  const abortControllerRef = useRef(null);

  useEffect(() => {
    // Initial loaded state
  }, []);

  const handleChange = (field, value) => {
    setFormData(prev => {
      const next = { ...prev, [field]: value };
      // Trigger validation if time/padding changes
      if (field === 'begin' || field === 'end') {
        debouncedValidate(next);
      }
      return next;
    });
  };

  const validationTimeoutRef = useRef(null);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (validationTimeoutRef.current) {
        clearTimeout(validationTimeoutRef.current);
      }
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, []);

  const debouncedValidate = (data) => {
    // Cancel pending execution
    if (validationTimeoutRef.current) {
      clearTimeout(validationTimeoutRef.current);
    }

    // Cancel pending request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    validationTimeoutRef.current = setTimeout(async () => {
      abortControllerRef.current = new AbortController();
      const signal = abortControllerRef.current.signal;

      setValidating(true);
      setConflict(null);
      setError(null);

      try {
        const proposed = {
          serviceRef: timer.serviceRef,
          name: data.name,
          begin: parseInt(data.begin),
          end: parseInt(data.end),
        };

        const resp = await previewConflicts({
          proposed: proposed,
          mode: 'conservative'
        });

        if (!signal.aborted) {
          if (!resp.canSchedule || (resp.conflicts && resp.conflicts.length > 0)) {
            setConflict(resp);
          }
        }
      } catch (err) {
        if (!signal.aborted) {
          console.error("Preview failed", err);
        }
      } finally {
        if (!signal.aborted) setValidating(false);
      }
    }, 500);
  };

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateTimer({ path: { timerId: timer.timerId }, body: {
        name: formData.name,
        description: formData.description,
        begin: formData.begin,
        end: formData.end,
        enabled: formData.enabled
      } });
      onSave(); // Parent refresh
      onClose();
    } catch (err) {
      // Map errors
      if (err.status === 409) {
        setError("Timer existiert bereits.");
      } else if (err.status === 422) {
        setError("Konflikt mit vorhandenem Timer.");
      } else if (err.status === 502) {
        setError("Receiver hat die Änderung nicht bestätigt. Bitte Receiver prüfen.");
      } else {
        setError("Fehler beim Speichern: " + (err.body?.detail || err.message));
      }
    } finally {
      setSaving(false);
    }
  };

  // Helper to parse local datetime input to unix
  const toUnix = (str) => Math.floor(new Date(str).getTime() / 1000);
  const toLocal = (unix) => {
    if (!unix) return '';
    const d = new Date(unix * 1000);
    const pad = (n) => n.toString().padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };

  // Capabilities Check
  const canEdit = capabilities?.timers?.edit !== false; // Default true if missing
  const readOnlyMsg = !canEdit ? "Bearbeitung nicht unterstützt." : null;

  return (
    <div className="dialog-overlay">
      <div className="dialog-card">
        <h2 className="dialog-title">Edit Timer</h2>

        {readOnlyMsg && <div className="readonly-msg">{readOnlyMsg}</div>}

        <div className="dialog-form">
          <div className="form-group">
            <label className="form-label">Name</label>
            <input
              className="form-input"
              value={formData.name}
              onChange={e => handleChange('name', e.target.value)}
              disabled={!canEdit}
            />
          </div>
          <div className="form-group">
            <label className="form-label">Description</label>
            <textarea
              className="form-textarea"
              value={formData.description}
              onChange={e => handleChange('description', e.target.value)}
              disabled={!canEdit}
            />
          </div>

          <div className="form-grid">
            <div className="form-group">
              <label className="form-label">Ref</label>
              <div className="form-static-text">{timer.serviceName || timer.serviceRef}</div>
            </div>
            <div className="form-group">
              <label className="form-label">Enabled</label>
              <input
                type="checkbox"
                checked={formData.enabled}
                onChange={e => handleChange('enabled', e.target.checked)}
                disabled={!canEdit}
                className="form-input-checkbox"
              />
            </div>
          </div>

          <div className="form-grid">
            <div className="form-group">
              <label className="form-label">Start</label>
              <input
                type="datetime-local"
                className="form-input"
                value={toLocal(formData.begin)}
                onChange={e => handleChange('begin', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
            <div className="form-group">
              <label className="form-label">End</label>
              <input
                type="datetime-local"
                className="form-input"
                value={toLocal(formData.end)}
                onChange={e => handleChange('end', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
          </div>

          {/* Conflict Warning */}
          {validating && <div className="dialog-status-text">Prüfe auf Konflikte...</div>}
          {conflict && (
            <div className="conflict-alert">
              <p className="conflict-title">Konflikt gefunden:</p>
              <ul className="conflict-list">
                {conflict.conflicts.map((c, i) => (
                  <li key={i}>
                    {c.blockingTimer.name} ({Math.round(c.overlapSeconds / 60)} min Überschneidung)
                  </li>
                ))}
              </ul>
            </div>
          )}

          {error && <div className="error-alert">{error}</div>}
        </div>

        <div className="dialog-actions">
          <button
            onClick={onClose}
            className="timer-btn btn-cancel"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={!canEdit || saving || (conflict && conflict.conflicts.length > 0)}
            className="timer-btn btn-save"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}
