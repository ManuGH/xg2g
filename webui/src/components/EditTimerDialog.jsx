import React, { useState, useEffect, useRef } from 'react';
import { TimersService, DvrService } from '../client';

export default function EditTimerDialog({ timer, onClose, onSave, capabilities }) {
  const [formData, setFormData] = useState({
    name: timer.name || '',
    description: timer.description || '',
    begin: timer.begin,
    end: timer.end,
    enabled: timer.state !== 'disabled',
    // Padding logic: UI could show padding or absolute times.
    // User requested: "Start time and end time (or padding...)"
    // Let's stick to absolute times for simplicity as per API.
  });

  const [conflict, setConflict] = useState(null); // { conflicts: [], canSchedule: boolean }
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  const abortControllerRef = useRef(null);

  useEffect(() => {
    // Initial validation?? Or just wait for user input.
    // Maybe validate initial state if it was already conflicting?
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

  const debouncedValidate = useRef((data) => {
    // Debounce logic
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    abortControllerRef.current = new AbortController();
    const signal = abortControllerRef.current.signal;

    setValidating(true);
    setConflict(null);
    setError(null);

    // 500ms delay
    setTimeout(async () => {
      if (signal.aborted) return;

      try {
        // Construct proposed timer
        const proposed = {
          serviceRef: timer.serviceRef,
          name: data.name,
          begin: parseInt(data.begin),
          end: parseInt(data.end),
          // ... other fields
        };

        const resp = await TimersService.previewConflicts({
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
          // Don't block UI on preview failure, just log
        }
      } finally {
        if (!signal.aborted) setValidating(false);
      }
    }, 500);
  }).current;

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await TimersService.updateTimer(timer.timerId, {
        name: formData.name,
        description: formData.description,
        begin: formData.begin,
        end: formData.end,
        enabled: formData.enabled
      });
      onSave(); // Parent refresh
      onClose();
    } catch (err) {
      // Map errors
      if (err.status === 409) {
        setError("Timer existiert bereits.");
      } else if (err.status === 422) {
        // If 422, it might return conflicts in body.
        // TimersService throws ApiError. verify body access.
        // For now generic message or try to parse body if possible.
        setError("Konflikt mit vorhandenem Timer.");
        // Ideally show conflict modal logic here too if API returns details.
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
    // adjusting for timezone offset manually or use simple ISO slice if local
    const d = new Date(unix * 1000);
    const pad = (n) => n.toString().padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  };

  // Capabilities Check
  const canEdit = capabilities?.timers?.edit !== false; // Default true if missing
  const readOnlyMsg = !canEdit ? "Bearbeitung nicht unterstützt." : null;

  return (
    <div className="fixed inset-0 bg-black/80 flex items-center justify-center z-50">
      <div className="bg-gray-800 p-6 rounded w-full max-w-lg shadow-lg border border-gray-700">
        <h2 className="text-xl font-bold mb-4">Edit Timer</h2>

        {readOnlyMsg && <div className="bg-yellow-900/50 text-yellow-200 p-2 mb-4 rounded">{readOnlyMsg}</div>}

        <div className="space-y-4">
          <div>
            <label className="block text-sm text-gray-400">Name</label>
            <input
              className="w-full bg-gray-900 border border-gray-700 rounded p-2 text-white"
              value={formData.name}
              onChange={e => handleChange('name', e.target.value)}
              disabled={!canEdit}
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400">Description</label>
            <textarea
              className="w-full bg-gray-900 border border-gray-700 rounded p-2 text-white"
              value={formData.description}
              onChange={e => handleChange('description', e.target.value)}
              disabled={!canEdit}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-gray-400">Ref</label>
              <div className="text-gray-300 text-sm py-2">{timer.serviceName || timer.serviceRef}</div>
            </div>
            <div>
              <label className="block text-sm text-gray-400">Enabled</label>
              <input
                type="checkbox"
                checked={formData.enabled}
                onChange={e => handleChange('enabled', e.target.checked)}
                disabled={!canEdit}
                className="mt-2"
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-gray-400">Start</label>
              <input
                type="datetime-local"
                className="w-full bg-gray-900 border border-gray-700 rounded p-2 text-white"
                value={toLocal(formData.begin)}
                onChange={e => handleChange('begin', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
            <div>
              <label className="block text-sm text-gray-400">End</label>
              <input
                type="datetime-local"
                className="w-full bg-gray-900 border border-gray-700 rounded p-2 text-white"
                value={toLocal(formData.end)}
                onChange={e => handleChange('end', toUnix(e.target.value))}
                disabled={!canEdit}
              />
            </div>
          </div>

          {/* Conflict Warning */}
          {validating && <div className="text-gray-500 text-sm">Prüfe auf Konflikte...</div>}
          {conflict && (
            <div className="bg-red-900/30 border border-red-800 p-3 rounded text-sm text-red-200">
              <p className="font-bold">Konflikt gefunden:</p>
              <ul className="list-disc pl-4 mt-1">
                {conflict.conflicts.map((c, i) => (
                  <li key={i}>
                    {c.blockingTimer.name} ({Math.round(c.overlapSeconds / 60)} min Überschneidung)
                  </li>
                ))}
              </ul>
            </div>
          )}

          {error && <div className="text-red-500 text-sm font-bold">{error}</div>}
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded bg-gray-700 hover:bg-gray-600"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={!canEdit || saving || (conflict && conflict.conflicts.length > 0)} // Optional: allow save despite conflict? 
            // User requirement: "If canSchedule=false... suggestions..."
            // User said: "Save returns 422 and UI shows conflicts." implying we allow click but it fails?
            // Or better: show conflict immediately.
            className={`px-4 py-2 rounded font-bold ${saving ? 'bg-blue-800' : 'bg-blue-600 hover:bg-blue-500'} disabled:opacity-50`}
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  );
}
