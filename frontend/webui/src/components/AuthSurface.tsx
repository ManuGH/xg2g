import { useId, type FormEventHandler, type ReactNode, type RefObject } from 'react';
import { Button } from './ui';
import styles from './AuthSurface.module.css';

interface AuthSurfaceFormProps {
  label: string;
  name: string;
  value: string;
  onValueChange: (nextValue: string) => void;
  onSubmit: FormEventHandler<HTMLFormElement>;
  submitLabel: string;
  submitDisabled?: boolean;
  placeholder?: string;
  inputRef?: RefObject<HTMLInputElement | null>;
  hint?: string;
  inputType?: 'password' | 'text';
  inputActions?: ReactNode;
}

interface AuthSurfaceProps {
  eyebrow?: string;
  title: string;
  copy?: string;
  children?: ReactNode;
  actions?: ReactNode;
  form?: AuthSurfaceFormProps;
}

export default function AuthSurface({
  eyebrow,
  title,
  copy,
  children,
  actions,
  form,
}: AuthSurfaceProps) {
  const titleId = useId();
  const inputId = useId();
  const hintId = useId();

  return (
    <div className={styles.overlay}>
      <div className={styles.modal} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        {eyebrow ? <span className={styles.eyebrow}>{eyebrow}</span> : null}
        <h2 id={titleId}>{title}</h2>
        {copy ? <p className={styles.copy}>{copy}</p> : null}
        {children}
        {form ? (
          <form onSubmit={form.onSubmit}>
            <div className={styles.fieldGroup}>
              <label htmlFor={inputId} className={styles.label}>
                {form.label}
              </label>
              <div className={styles.inputRow}>
                <input
                  id={inputId}
                  ref={form.inputRef}
                  type={form.inputType ?? 'password'}
                  name={form.name}
                  value={form.value}
                  placeholder={form.placeholder}
                  aria-describedby={form.hint ? hintId : undefined}
                  onChange={(event) => form.onValueChange(event.target.value)}
                  autoComplete="current-password"
                  autoCapitalize="off"
                  autoCorrect="off"
                  spellCheck={false}
                />
                {form.inputActions ? <div className={styles.inputActions}>{form.inputActions}</div> : null}
              </div>
              {form.hint ? (
                <p id={hintId} className={styles.hint}>
                  {form.hint}
                </p>
              ) : null}
            </div>
            <div className={styles.formActions}>
              <Button type="submit" disabled={form.submitDisabled}>
                {form.submitLabel}
              </Button>
            </div>
          </form>
        ) : null}
        {actions ? <div className={styles.actions}>{actions}</div> : null}
      </div>
    </div>
  );
}
