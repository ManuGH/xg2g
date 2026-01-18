// Card Primitive - Broadcast Console 2026
// CTO Contract: Single source of truth for all card patterns
// Token-only, no gradients, glow only for status variants

import React from 'react';
import './Card.css';

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
  const variantClass = variant !== 'standard' ? `card--accent-${variant}` : '';
  const interactiveClass = interactive ? 'card--interactive' : '';

  return (
    <div
      className={`card ${variantClass} ${interactiveClass} ${className}`.trim()}
      onClick={onClick}
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
    >
      {children}
    </div>
  );
}

export function CardHeader({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`card__header ${className}`.trim()}>{children}</div>;
}

export function CardTitle({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <h3 className={`card__title ${className}`.trim()}>{children}</h3>;
}

export function CardSubtitle({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`card__subtitle ${className}`.trim()}>{children}</div>;
}

export function CardBody({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`card__body ${className}`.trim()}>{children}</div>;
}

export function CardFooter({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`card__footer ${className}`.trim()}>{children}</div>;
}

// Attach sub-components for a nicer API
Card.Header = CardHeader;
Card.Title = CardTitle;
Card.Subtitle = CardSubtitle;
Card.Body = CardBody;
Card.Content = CardBody; // Alias for flexibility
Card.Footer = CardFooter;
