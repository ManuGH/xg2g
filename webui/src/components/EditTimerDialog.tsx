// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect, useRef } from 'react';
import {
  updateTimer,
  previewConflicts,
  type Timer,
  type DvrCapabilities,
  type TimerConflictPreviewResponse,
  type TimerCreateRequest
} from '../client-ts';
import './EditTimerDialog.css';

interface EditTimerDialogProps {
  timer: Timer;
  onClose: () => void;
  onSave: () => void;
  capabilities?: DvrCapabilities;
}

interface FormData {
  name: string;
  description: string;
  begin: number; // Unix timestamp
  end: number;   // Unix timestamp
  enabled: boolean;
}

export default function EditTimerDialog({ timer, onClose, onSave, capabilities }: EditTimerDialogProps) {
  const [formData, setFormData] = useState<FormData>({
    name: timer.name || '',
    description: timer.description || '',
    begin: timer.begin || 0,
    end: timer.end || 0,
    enabled: timer.state !== 'disabled',
  });

  const [conflict, setConflict] = useState<TimerConflictPreviewResponse | null>(null);
  const [validating, setValidating] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<boolean>(false);

  const abortControllerRef = useRef<AbortController | null>(null);

  const handleChange = (field: keyof FormData, value: any) => {
    setFormData(prev => {
      const next = { ...prev, [field]: value };
      // Trigger validation if time changes
      if (field === 'begin' || field === 'end') {
        debouncedValidate(next);
      }
      return next;
    });
  };

  const validationTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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

  const debouncedValidate = (data: FormData) => {
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
        const proposed: TimerCreateRequest = {
          serviceRef: timer.serviceRef,
          name: data.name,
          begin: data.begin,
          end: data.end,
          // Correctly map optional fields if they exist
          description: data.description,
          enabled: data.enabled
        };

        const response = await previewConflicts({
          body: {
            proposed: proposed,
            mode: 'conservative'
          }
        });

        // SDK returns the response body directly, so we just use response.data (if axios wrapper)
        // BUT wait, check if the response object has data. 
        // Based on user feedback: "resp ist TimerConflictPreviewResponse" implies response.data usage.
        const resp = response.data; // Ensure we access data property

        if (!signal.aborted && resp) {
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
    if (!timer.timerId) return;

    setSaving(true);
    setError(null);
    try {
      await updateTimer({
        path: { timerId: timer.timerId },
        body: {
          name: formData.name,
          description: formData.description,
          begin: formData.begin,
          end: formData.end,
          enabled: formData.enabled
        }
      });
      onSave(); // Parent refresh
      onClose();
    } catch (err: any) {
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
  const toUnix = (str: string) => Math.floor(new Date(str).getTime() / 1000);
  const toLocal = (unix: number) => {
    if (!unix) return '';
    const d = new Date(unix * 1000);
    const pad = (n: number) => n.toString().padStart(2, '0');
    return `${d.getFullYear()} -${pad(d.getMonth() + 1)} -${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())} `;
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
                {conflict.conflicts?.map((c, i) => (
                  <li key={i}>
                    {c.blockingTimer?.name} ({Math.round((c.overlapSeconds || 0) / 60)} min Überschneidung)
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
            disabled={!canEdit || saving || (conflict !== null && (conflict.conflicts?.length || 0) > 0)}
            className="timer-btn btn-save"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}
