import { useState, useRef, useEffect, useLayoutEffect, ReactNode } from 'react';
import styles from './DropdownMenu.module.css';

export interface DropdownOption {
  id: string | number;
  label: string;
  description?: string;
  badge?: string;
}

interface DropdownMenuProps {
  icon: ReactNode;
  options: DropdownOption[];
  activeId: string | number;
  onSelect: (id: string | number) => void;
  title?: string;
  disabled?: boolean;
}

// Space kept free between the popup and the top of its clipping box.
const POPUP_EDGE_MARGIN_PX = 8;
// Distance between trigger and popup (.popup bottom offset in the CSS module).
const POPUP_TRIGGER_GAP_PX = 12;

// The nearest ancestor that clips overflow bounds how far the upward-opening
// popup may extend; without one it is the viewport.
function clippingTop(node: HTMLElement): number {
  for (let el = node.parentElement; el; el = el.parentElement) {
    const { overflow, overflowY } = window.getComputedStyle(el);
    if (/(hidden|clip|auto|scroll)/.test(`${overflow} ${overflowY}`)) {
      return Math.max(0, el.getBoundingClientRect().top);
    }
  }
  return 0;
}

export function DropdownMenu({ icon, options, activeId, onSelect, title, disabled }: DropdownMenuProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [popupMaxHeight, setPopupMaxHeight] = useState<number | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Landscape phones leave little height above the control bar; cap the popup to
  // the space that is actually visible so the list scrolls instead of being cut.
  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!isOpen || !container) {
      setPopupMaxHeight(null);
      return;
    }
    const measure = () => {
      const available = container.getBoundingClientRect().top - clippingTop(container) - POPUP_TRIGGER_GAP_PX - POPUP_EDGE_MARGIN_PX;
      setPopupMaxHeight(Math.max(0, Math.floor(available)));
    };
    measure();
    window.addEventListener('resize', measure);
    window.addEventListener('orientationchange', measure);
    return () => {
      window.removeEventListener('resize', measure);
      window.removeEventListener('orientationchange', measure);
    };
  }, [isOpen]);

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
        <div
          className={styles.popup}
          style={popupMaxHeight !== null ? { maxHeight: `${popupMaxHeight}px` } : undefined}
        >
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
                <div className={styles.optionContent}>
                  <div className={styles.optionHeader}>
                    <span className={styles.optionLabel}>{option.label}</span>
                    {option.badge && <span className={styles.optionBadge}>{option.badge}</span>}
                  </div>
                  {option.description && <span className={styles.optionDescription}>{option.description}</span>}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
