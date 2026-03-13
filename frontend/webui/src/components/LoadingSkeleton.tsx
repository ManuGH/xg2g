import { useTranslation } from 'react-i18next';
import styles from './LoadingSkeleton.module.css';

export type LoadingSkeletonVariant = 'gate' | 'page' | 'section';

interface LoadingSkeletonProps {
  variant?: LoadingSkeletonVariant;
  label?: string;
  className?: string;
}

export default function LoadingSkeleton({
  variant = 'section',
  label,
  className,
}: LoadingSkeletonProps) {
  const { t } = useTranslation();
  const accessibleLabel = label ?? t('common.loading', { defaultValue: 'Loading…' });
  const classes = [styles.root, styles[variant], className].filter(Boolean).join(' ');

  if (variant === 'gate') {
    return (
      <div className={classes} role="status" aria-live="polite" aria-label={accessibleLabel} data-loading-variant={variant}>
        <span className={styles.srOnly}>{accessibleLabel}</span>
        <div className={styles.gateFrame}>
          <div className={['animate-skeleton', styles.line, styles.lineShort].join(' ')} />
          <div className={['animate-skeleton', styles.block].join(' ')} />
          <div className={['animate-skeleton', styles.line, styles.lineMedium].join(' ')} />
          <div className={['animate-skeleton', styles.block].join(' ')} />
        </div>
      </div>
    );
  }

  if (variant === 'page') {
    return (
      <div className={classes} role="status" aria-live="polite" aria-label={accessibleLabel} data-loading-variant={variant}>
        <span className={styles.srOnly}>{accessibleLabel}</span>
        <div className={styles.pageHeader}>
          <div className={['animate-skeleton', styles.line, styles.lineShort].join(' ')} />
          <div className={['animate-skeleton', styles.line, styles.lineMedium].join(' ')} />
          <div className={['animate-skeleton', styles.block].join(' ')} />
        </div>
        <div className={styles.pageGrid}>
          <div className={['animate-skeleton', styles.panel].join(' ')} />
          <div className={['animate-skeleton', styles.panel].join(' ')} />
        </div>
      </div>
    );
  }

  return (
    <div className={classes} role="status" aria-live="polite" aria-label={accessibleLabel} data-loading-variant={variant}>
      <span className={styles.srOnly}>{accessibleLabel}</span>
      <div className={styles.sectionRow}>
        <div className={['animate-skeleton', styles.line, styles.lineShort].join(' ')} />
        <div className={['animate-skeleton', styles.line, styles.lineWide].join(' ')} />
        <div className={['animate-skeleton', styles.line, styles.lineMedium].join(' ')} />
      </div>
      <div className={['animate-skeleton', styles.block].join(' ')} />
    </div>
  );
}
