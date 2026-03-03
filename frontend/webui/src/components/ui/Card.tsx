// Card Primitive - Broadcast Console 2026
// CTO Contract: Single source of truth for all card patterns
// Token-only, no gradients, glow only for status variants

import React from 'react';
import styles from './Card.module.css';

export type CardVariant = 'standard' | 'live' | 'action';

export interface CardProps {
  variant?: CardVariant;
  interactive?: boolean;
  children: React.ReactNode;
  className?: string;
  onClick?: () => void;
}

export function Card({
  variant = 'standard',
  interactive = false,
  children,
  className = '',
  onClick
}: CardProps) {
  const hasClick = typeof onClick === 'function';
  const isInteractive = interactive || hasClick;
  const variantClass =
    variant === 'live'
      ? styles.accentLive
      : variant === 'action'
        ? styles.accentAction
        : '';
  const interactiveClass = isInteractive ? styles.interactive : '';

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (!hasClick) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onClick();
    }
  };

  return (
    <div
      data-ui="card"
      data-interactive={isInteractive ? 'true' : undefined}
      className={[styles.card, variantClass, interactiveClass, className].filter(Boolean).join(' ')}
      onClick={onClick}
      onKeyDown={hasClick ? handleKeyDown : undefined}
      role={hasClick ? 'button' : undefined}
      tabIndex={hasClick ? 0 : undefined}
    >
      {children}
    </div>
  );
}

export function CardHeader({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={[styles.header, className].filter(Boolean).join(' ')}>{children}</div>;
}

export function CardTitle({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <h3 className={[styles.title, className].filter(Boolean).join(' ')}>{children}</h3>;
}

export function CardSubtitle({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={[styles.subtitle, className].filter(Boolean).join(' ')}>{children}</div>;
}

export function CardBody({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={[styles.body, className].filter(Boolean).join(' ')}>{children}</div>;
}

export function CardFooter({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={[styles.footer, className].filter(Boolean).join(' ')}>{children}</div>;
}

// Attach sub-components for a nicer API
Card.Header = CardHeader;
Card.Title = CardTitle;
Card.Subtitle = CardSubtitle;
Card.Body = CardBody;
Card.Content = CardBody; // Alias for flexibility
Card.Footer = CardFooter;
