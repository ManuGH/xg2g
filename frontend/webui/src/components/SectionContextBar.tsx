import { Button } from './ui';
import styles from './SectionContextBar.module.css';

interface SectionContextSegment {
  label: string;
  onClick?: () => void;
}

interface SectionContextBarProps {
  segments: SectionContextSegment[];
  actionLabel?: string;
  onAction?: () => void;
}

export default function SectionContextBar({
  segments,
  actionLabel,
  onAction,
}: SectionContextBarProps) {
  return (
    <section className={styles.bar}>
      <nav className={styles.path} aria-label="Section path">
        {segments.map((segment, index) => {
          const isCurrent = index === segments.length - 1;

          return (
            <div key={`${segment.label}-${index}`} className={styles.segment}>
              {segment.onClick && !isCurrent ? (
                <button
                  type="button"
                  className={styles.link}
                  onClick={segment.onClick}
                >
                  {segment.label}
                </button>
              ) : (
                <span className={isCurrent ? styles.current : styles.label}>
                  {segment.label}
                </span>
              )}
              {!isCurrent ? <span className={styles.separator}>/</span> : null}
            </div>
          );
        })}
      </nav>

      {actionLabel && onAction ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={onAction}
        >
          {actionLabel}
        </Button>
      ) : null}
    </section>
  );
}
