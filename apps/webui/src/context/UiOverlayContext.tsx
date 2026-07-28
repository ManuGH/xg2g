import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Button } from '../components/ui';
import { useTvInitialFocus } from '../hooks/useTvInitialFocus';
import styles from './UiOverlayContext.module.css';

export type ToastKind = 'success' | 'warning' | 'error' | 'info';

export interface ToastInput {
  kind?: ToastKind;
  title?: string;
  message: string;
  details?: string;
  timeoutMs?: number;
}

export type ConfirmTone = 'default' | 'action' | 'danger';

export interface ConfirmInput {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: ConfirmTone;
}

export interface PinPromptInput {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  placeholder?: string;
}

interface Toast extends Required<Pick<ToastInput, 'message'>> {
  id: string;
  kind: ToastKind;
  title?: string;
  details?: string;
  timeoutMs: number;
}

interface ActiveConfirm extends Required<Pick<ConfirmInput, 'message'>> {
  title: string;
  confirmLabel: string;
  cancelLabel: string;
  tone: ConfirmTone;
}

interface ActivePinPrompt extends Required<Pick<PinPromptInput, 'message'>> {
  title: string;
  confirmLabel: string;
  cancelLabel: string;
  placeholder: string;
}

interface UiOverlayContextValue {
  toast: (input: ToastInput) => void;
  confirm: (input: ConfirmInput) => Promise<boolean>;
  promptPin: (input: PinPromptInput) => Promise<string | null>;
}

const UiOverlayContext = createContext<UiOverlayContextValue | undefined>(undefined);

function newId(): string {
  try {
    return crypto.randomUUID();
  } catch {
    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }
}

export function useUiOverlay(): UiOverlayContextValue {
  const ctx = useContext(UiOverlayContext);
  if (!ctx) throw new Error('useUiOverlay must be used within UiOverlayProvider');
  return ctx;
}

export function UiOverlayProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastTimeoutsRef = useRef<Map<string, number>>(new Map());

  const toast = useCallback((input: ToastInput) => {
    const id = newId();
    const kind: ToastKind = input.kind ?? 'info';
    const timeoutMs = input.timeoutMs ?? (kind === 'error' ? 6500 : 4200);

    const next: Toast = {
      id,
      kind,
      title: input.title,
      message: input.message,
      details: input.details,
      timeoutMs,
    };

    setToasts(prev => [...prev, next].slice(-4));

    const timerId = window.setTimeout(() => {
      toastTimeoutsRef.current.delete(id);
      setToasts(prev => prev.filter(t => t.id !== id));
    }, timeoutMs);
    toastTimeoutsRef.current.set(id, timerId);
  }, []);

  const [activeConfirm, setActiveConfirm] = useState<ActiveConfirm | null>(null);
  const confirmResolveRef = useRef<((v: boolean) => void) | null>(null);
  const [activePinPrompt, setActivePinPrompt] = useState<ActivePinPrompt | null>(null);
  const pinPromptResolveRef = useRef<((v: string | null) => void) | null>(null);

  const confirm = useCallback((input: ConfirmInput) => {
    // Only support one modal at a time. If a new confirm is requested, cancel the previous one.
    confirmResolveRef.current?.(false);
    confirmResolveRef.current = null;
    pinPromptResolveRef.current?.(null);
    pinPromptResolveRef.current = null;
    setActivePinPrompt(null);

    setActiveConfirm({
      title: input.title ?? 'Confirm',
      message: input.message,
      confirmLabel: input.confirmLabel ?? 'Confirm',
      cancelLabel: input.cancelLabel ?? 'Cancel',
      tone: input.tone ?? 'default',
    });

    return new Promise<boolean>((resolve) => {
      confirmResolveRef.current = resolve;
    });
  }, []);

  const promptPin = useCallback((input: PinPromptInput) => {
    confirmResolveRef.current?.(false);
    confirmResolveRef.current = null;
    setActiveConfirm(null);

    pinPromptResolveRef.current?.(null);
    pinPromptResolveRef.current = null;

    setActivePinPrompt({
      title: input.title ?? 'PIN',
      message: input.message,
      confirmLabel: input.confirmLabel ?? 'Unlock',
      cancelLabel: input.cancelLabel ?? 'Cancel',
      placeholder: input.placeholder ?? 'PIN',
    });

    return new Promise<string | null>((resolve) => {
      pinPromptResolveRef.current = resolve;
    });
  }, []);

  const dismissToast = useCallback((id: string) => {
    const timer = toastTimeoutsRef.current.get(id);
    if (typeof timer === 'number') window.clearTimeout(timer);
    toastTimeoutsRef.current.delete(id);
    setToasts(prev => prev.filter(t => t.id !== id));
  }, []);

  const resolveConfirm = useCallback((value: boolean) => {
    confirmResolveRef.current?.(value);
    confirmResolveRef.current = null;
    setActiveConfirm(null);
  }, []);

  const resolvePinPrompt = useCallback((value: string | null) => {
    pinPromptResolveRef.current?.(value);
    pinPromptResolveRef.current = null;
    setActivePinPrompt(null);
  }, []);

  const value = useMemo<UiOverlayContextValue>(() => ({ toast, confirm, promptPin }), [toast, confirm, promptPin]);

  const [portalEl, setPortalEl] = useState<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = document.createElement('div');
    const toastTimeouts = toastTimeoutsRef.current;
    el.setAttribute('data-ui-overlay-root', 'true');
    document.body.appendChild(el);
    setPortalEl(el);

    return () => {
      // Clean up pending timers and pending confirm.
      for (const timer of toastTimeouts.values()) {
        window.clearTimeout(timer);
      }
      toastTimeouts.clear();
      confirmResolveRef.current?.(false);
      confirmResolveRef.current = null;
      pinPromptResolveRef.current?.(null);
      pinPromptResolveRef.current = null;

      setPortalEl(null);
      el.remove();
    };
  }, []);

  return (
    <UiOverlayContext.Provider value={value}>
      {children}

      {portalEl && createPortal(
        <>
          {/* Toasts */}
          <div className={styles.toastViewport} aria-live="polite" aria-relevant="additions">
            {toasts.map(t => (
              <div key={t.id} className={`${styles.toast} animate-enter`.trim()} data-kind={t.kind} role="status">
                <div className={styles.toastMain}>
                  <div className={styles.toastText}>
                    {t.title && <div className={styles.toastTitle}>{t.title}</div>}
                    <div className={styles.toastMessage}>{t.message}</div>
                    {t.details && <div className={`${styles.toastDetails} tabular`.trim()}>{t.details}</div>}
                  </div>
                  <button
                    type="button"
                    className={styles.toastClose}
                    aria-label="Dismiss notification"
                    onClick={() => dismissToast(t.id)}
                  >
                    ✕
                  </button>
                </div>
              </div>
            ))}
          </div>

          {/* Confirm dialog */}
          {activeConfirm && (
            <ConfirmDialog
              confirm={activeConfirm}
              onCancel={() => resolveConfirm(false)}
              onConfirm={() => resolveConfirm(true)}
            />
          )}

          {activePinPrompt && (
            <PinPromptDialog
              prompt={activePinPrompt}
              onCancel={() => resolvePinPrompt(null)}
              onConfirm={(value) => resolvePinPrompt(value)}
            />
          )}
        </>,
        portalEl
      )}
    </UiOverlayContext.Provider>
  );
}

function PinPromptDialog({
  prompt,
  onCancel,
  onConfirm,
}: {
  prompt: ActivePinPrompt;
  onCancel: () => void;
  onConfirm: (value: string) => void;
}) {
  const inputRef = React.useRef<HTMLInputElement>(null);
  const [value, setValue] = React.useState('');
  const normalizedValue = value.trim();

  React.useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onCancel();
        return;
      }
      if (e.key === 'Enter' && normalizedValue) {
        onConfirm(normalizedValue);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [normalizedValue, onCancel, onConfirm]);

  useTvInitialFocus({
    enabled: true,
    targetRef: inputRef,
  });

  return (
    <div
      className={`${styles.confirmOverlay} animate-enter`.trim()}
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
    >
      <div className={styles.confirmModal} role="dialog" aria-modal="true" aria-label={prompt.title}>
        <div className={styles.confirmHeader}>
          <h2 className={styles.confirmTitle}>{prompt.title}</h2>
        </div>
        <div className={styles.confirmBody}>
          <p className={styles.confirmMessage}>{prompt.message}</p>
          <div className={styles.promptField}>
            <input
              ref={inputRef}
              className={styles.promptInput}
              type="password"
              inputMode="numeric"
              pattern="[0-9]*"
              autoComplete="one-time-code"
              placeholder={prompt.placeholder}
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          </div>
        </div>
        <div className={styles.confirmActions}>
          <Button variant="secondary" onClick={onCancel}>
            {prompt.cancelLabel}
          </Button>
          <Button
            variant="primary"
            onClick={() => onConfirm(normalizedValue)}
            disabled={!normalizedValue}
          >
            {prompt.confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

function ConfirmDialog({
  confirm,
  onCancel,
  onConfirm,
}: {
  confirm: ActiveConfirm;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const confirmButtonRef = React.useRef<HTMLButtonElement>(null);

  React.useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onCancel]);

  useTvInitialFocus({
    enabled: true,
    targetRef: confirmButtonRef,
  });

  return (
    <div
      className={`${styles.confirmOverlay} animate-enter`.trim()}
      role="presentation"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
    >
      <div className={styles.confirmModal} role="dialog" aria-modal="true" aria-label={confirm.title}>
        <div className={styles.confirmHeader}>
          <h2 className={styles.confirmTitle}>{confirm.title}</h2>
        </div>
        <div className={styles.confirmBody}>
          <p className={styles.confirmMessage}>{confirm.message}</p>
        </div>
        <div className={styles.confirmActions}>
          <Button variant="secondary" onClick={onCancel}>
            {confirm.cancelLabel}
          </Button>
          <Button
            ref={confirmButtonRef}
            variant={confirm.tone === 'danger' ? 'danger' : 'primary'}
            onClick={onConfirm}
            autoFocus
          >
            {confirm.confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}
