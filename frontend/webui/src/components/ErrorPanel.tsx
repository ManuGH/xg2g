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

function getSeverityLabel(severity: NonNullable<AppError['severity']>, t: ReturnType<typeof useTranslation>['t']): string {
  return t(`common.errorSeverity.${severity}`, {
    defaultValue: severity.charAt(0).toUpperCase() + severity.slice(1),
  });
}

function isAbsoluteUrl(value: string): boolean {
  return /^https?:\/\//i.test(value);
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
  const severityLabel = error.severity ? getSeverityLabel(error.severity, t) : null;
  const hasMeta = Boolean(severityLabel || typeof error.status === 'number' || error.code);
  const hasGuidance = Boolean(error.operatorHint || error.runbookUrl);

  return (
    <Card className={[styles.panel, className].filter(Boolean).join(' ')}>
      <div className={styles.body} role="alert">
        {hasMeta ? (
          <div className={styles.meta}>
            {severityLabel ? (
              <span className={styles.severity} data-severity={error.severity}>
                {severityLabel}
              </span>
            ) : null}
            {typeof error.status === 'number' ? (
              <span className={styles.status}>
                {t('common.error', { defaultValue: 'Error' })} {error.status}
              </span>
            ) : null}
            {error.code ? (
              <code className={styles.code}>
                {t('common.errorCode', { defaultValue: 'Code' })}: {error.code}
              </code>
            ) : null}
          </div>
        ) : null}
        <TitleTag className={styles.title}>{error.title}</TitleTag>
        {error.detail ? <p className={styles.detail}>{error.detail}</p> : null}
        {hasGuidance ? (
          <div className={styles.guidance}>
            {error.operatorHint ? (
              <div className={styles.operatorHint}>
                <span className={styles.guidanceLabel}>
                  {t('common.operatorHint', { defaultValue: 'Operator hint' })}
                </span>
                <p className={styles.guidanceText}>{error.operatorHint}</p>
              </div>
            ) : null}
            {error.runbookUrl ? (
              <a
                className={styles.runbook}
                href={error.runbookUrl}
                target={isAbsoluteUrl(error.runbookUrl) ? '_blank' : undefined}
                rel={isAbsoluteUrl(error.runbookUrl) ? 'noreferrer' : undefined}
              >
                {t('common.runbook', { defaultValue: 'Runbook' })}
              </a>
            ) : null}
          </div>
        ) : null}
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
