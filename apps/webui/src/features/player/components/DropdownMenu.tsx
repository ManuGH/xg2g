import { useState, useRef, useEffect, ReactNode } from 'react';
import styles from './DropdownMenu.module.css';

interface DropdownOption {
  id: string | number;
  label: string;
}

interface DropdownMenuProps {
  icon: ReactNode;
  options: DropdownOption[];
  activeId: string | number;
  onSelect: (id: string | number) => void;
  title?: string;
  disabled?: boolean;
}

export function DropdownMenu({ icon, options, activeId, onSelect, title, disabled }: DropdownMenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  return (
    <div className={styles.container} ref={containerRef}>
      <button
        className={`${styles.trigger} ${isOpen ? styles.active : ''}`}
        onClick={() => !disabled && setIsOpen(!isOpen)}
        title={title}
        disabled={disabled}
      >
        {icon}
      </button>

      {isOpen && options.length > 0 && (
        <div className={styles.popup}>
          {title && <div className={styles.popupTitle}>{title}</div>}
          <div className={styles.optionsList}>
            {options.map((option) => (
              <button
                key={option.id}
                className={`${styles.optionItem} ${activeId === option.id ? styles.selected : ''}`}
                onClick={() => {
                  onSelect(option.id);
                  setIsOpen(false);
                }}
              >
                <div className={styles.optionCheckIndicator}>
                  {activeId === option.id && (
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  )}
                </div>
                <span className={styles.optionLabel}>{option.label}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
