// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { useState, useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import {
  updateTimer,
  previewConflicts,
  type Timer,
  type DvrCapabilities,
  type TimerConflictPreviewResponse,
  type TimerCreateRequest
} from '../client-ts';
import { debugError, formatError } from '../utils/logging';
import { Button } from './ui';
import styles from './EditTimerDialog.module.css';

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
  const { t } = useTranslation();
  const [formData, setFormData] = useState<FormData>({
    name: timer.name || '',
    // UI-INV-TIMER-001: Preserve raw truth. Normalization happens ONLY for display if needed.
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

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

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
          debugError('Preview failed', formatError(err));
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
      // UI-INV-TIMER-001: Dirty-field write strategy (Seal Model B).
      // Only include fields that have definitively changed compared to props.
      const body: any = {};

      if (formData.name !== (timer.name || '')) body.name = formData.name;
      if (formData.description !== (timer.description || '')) body.description = formData.description;
      if (formData.begin !== (timer.begin || 0)) body.begin = formData.begin;
      if (formData.end !== (timer.end || 0)) body.end = formData.end;

      const isEnabled = timer.state !== 'disabled';
      if (formData.enabled !== isEnabled) body.enabled = formData.enabled;

      if (Object.keys(body).length === 0) {
        onClose(); // No changes to save
        return;
      }

      await updateTimer({
        path: { timerId: timer.timerId },
        body
      });
      onSave(); // Trigger parent refresh
      onClose();
    } catch (err: any) {
      debugError('Save failed', formatError(err));

      // RFC7807 Discipline: Extract title/detail
      let msg = t('common.saveFailed', 'Save failed');
      if (err.data && err.data.detail) {
        msg = `${err.data.title || msg}: ${err.data.detail}`;
      } else if (err.message) {
        msg = err.message;
      }

      setError(msg);
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
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };

  // Capabilities Check
  const canEdit = capabilities?.timers?.edit !== false; // Default true if missing
  const readOnlyMsg = !canEdit ? "Bearbeitung nicht unterstützt." : null;

  return (
    <div
      className={`${styles.overlay} animate-enter`.trim()}
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className={styles.card} role="dialog" aria-modal="true" aria-labelledby="timer-edit-title">
        <h2 id="timer-edit-title" className={styles.title}>Edit Timer</h2>

        {readOnlyMsg && <div className={styles.readonlyMsg}>{readOnlyMsg}</div>}

        <div className={styles.form}>
          <div className={styles.formGroup}>
            <label className={styles.label}>Name</label>
            <input
              className={styles.input}
              value={formData.name}
              onChange={e => handleChange('name', e.target.value)}
              disabled={!canEdit}
              data-testid="timer-edit-name"
              autoFocus
            />
          </div>
          <div className={styles.formGroup}>
            <label className={styles.label}>Description</label>
            <textarea
              className={styles.textarea}
              value={formData.description}
              onChange={e => handleChange('description', e.target.value)}
              disabled={!canEdit}
              data-testid="timer-edit-description"
            />
          </div>

          <div className={styles.grid}>
            <div className={styles.formGroup}>
              <label className={styles.label}>Ref</label>
              <div className={styles.staticText}>{timer.serviceName || timer.serviceRef}</div>
            </div>
            <div className={styles.formGroup}>
              <label className={styles.label}>Enabled</label>
              <input
                type="checkbox"
                checked={formData.enabled}
                onChange={e => handleChange('enabled', e.target.checked)}
                disabled={!canEdit}
                className={styles.inputCheckbox}
              />
            </div>
          </div>

          <div className={styles.grid}>
            <div className={styles.formGroup}>
              <label className={styles.label}>Start</label>
              <input
                type="datetime-local"
                className={styles.input}
                value={toLocal(formData.begin)}
                onChange={e => handleChange('begin', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
            <div className={styles.formGroup}>
              <label className={styles.label}>End</label>
              <input
                type="datetime-local"
                className={styles.input}
                value={toLocal(formData.end)}
                onChange={e => handleChange('end', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
          </div>

          {/* Conflict Warning */}
          {validating && <div className={styles.statusText}>Prüfe auf Konflikte...</div>}
          {conflict && (
            <div className={styles.conflictAlert}>
              <p className={styles.conflictTitle}>Konflikt gefunden:</p>
              <ul className={styles.conflictList}>
                {conflict.conflicts?.map((c, i) => (
                  <li key={i}>
                    {c.blockingTimer?.name} ({Math.round((c.overlapSeconds || 0) / 60)} min Überschneidung)
                  </li>
                ))}
              </ul>
            </div>
          )}

          {error && <div className={styles.errorAlert} role="alert">{error}</div>}
        </div>

        <div className={styles.actions}>
          <Button
            onClick={onClose}
            variant="secondary"
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={!canEdit || saving || (conflict !== null && (conflict.conflicts?.length || 0) > 0)}
            data-testid="timer-edit-save"
          >
            {saving ? 'Saving...' : 'Save'}
          </Button>
        </div>
      </div>
    </div>
  );
}
