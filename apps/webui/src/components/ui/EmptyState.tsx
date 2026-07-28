// EmptyState Primitive - Broadcast Console 2026
// Single source of truth for "no data / nothing here" surfaces.

import React from 'react';
import styles from './EmptyState.module.css';

export type EmptyStateVariant = 'panel' | 'inline';

export interface EmptyStateProps {
  title?: React.ReactNode;
  description?: React.ReactNode;
  icon?: React.ReactNode;
  variant?: EmptyStateVariant;
  action?: React.ReactNode;
  className?: string;
  role?: string;
}

export function EmptyState({
  title,
  description,
  icon,
  variant = 'panel',
  action,
  className = '',
  role = 'status',
}: EmptyStateProps) {
  const variantClass = variant === 'inline' ? styles.inline : styles.panel;
  return (
    <div
      data-ui="empty-state"
      data-variant={variant}
      role={role}
      className={[styles.root, variantClass, className].filter(Boolean).join(' ')}
    >
      {icon ? (
        <span className={styles.icon} aria-hidden="true">
          {icon}
        </span>
      ) : null}
      {title ? <h3 className={styles.title}>{title}</h3> : null}
      {description ? <p className={styles.description}>{description}</p> : null}
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}
