import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { AppError } from '../types/errors';
import { Button, Card } from './ui';
import styles from './ErrorPanel.module.css';

interface ErrorPanelProps {
  error: AppError;
  onRetry?: () => void;
  homeHref?: string;
  titleAs?: 'h2' | 'h3';
  children?: ReactNode;
  className?: string;
}

export default function ErrorPanel({
  error,
  onRetry,
  homeHref,
  titleAs = 'h2',
  children,
  className,
}: ErrorPanelProps) {
  const { t } = useTranslation();
  const TitleTag = titleAs;

  return (
    <Card className={[styles.panel, className].filter(Boolean).join(' ')}>
      <div className={styles.body} role="alert">
        {typeof error.status === 'number' ? (
          <span className={styles.status}>
            {t('common.error', { defaultValue: 'Error' })} {error.status}
          </span>
        ) : null}
        <TitleTag className={styles.title}>{error.title}</TitleTag>
        {error.detail ? <p className={styles.detail}>{error.detail}</p> : null}
        {(error.retryable && onRetry) || homeHref ? (
          <div className={styles.actions}>
            {error.retryable && onRetry ? (
              <Button variant="secondary" onClick={onRetry}>
                {t('common.retry', { defaultValue: 'Retry' })}
              </Button>
            ) : null}
            {homeHref ? (
              <Link to={homeHref} className={styles.link}>
                {t('nav.dashboard', { defaultValue: 'Dashboard' })}
              </Link>
            ) : null}
          </div>
        ) : null}
        {children ? <div className={styles.details}>{children}</div> : null}
      </div>
    </Card>
  );
}
