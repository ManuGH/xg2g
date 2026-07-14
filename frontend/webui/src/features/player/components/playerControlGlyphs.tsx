import styles from './V3Player.module.css';

export function PlayGlyph() {
  return (
    <svg className={[styles.controlIcon, styles.playPauseIcon].join(' ')} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M8 6.5v11l9-5.5-9-5.5Z" fill="currentColor" />
    </svg>
  );
}

export function PauseGlyph() {
  return (
    <svg className={[styles.controlIcon, styles.playPauseIcon].join(' ')} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M7 6h3.5v12H7V6Zm6.5 0H17v12h-3.5V6Z" fill="currentColor" />
    </svg>
  );
}

export function VolumeGlyph({ muted }: { muted: boolean }) {
  return muted ? (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M10 8.25 6.9 11H4v2h2.9L10 15.75V8.25Z" fill="currentColor" />
      <path d="m14.25 9.25 5.5 5.5M19.75 9.25l-5.5 5.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  ) : (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M10 8.25 6.9 11H4v2h2.9L10 15.75V8.25Z" fill="currentColor" />
      <path d="M14.5 9.3a4.4 4.4 0 0 1 0 5.4M17.2 7a7.6 7.6 0 0 1 0 10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

export function FullscreenGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M8 4H4v4M16 4h4v4M8 20H4v-4M20 20h-4v-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function PipGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M3.5 5.5h17v13h-17z" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <rect x="12" y="11" width="7.5" height="5.5" rx="1.2" fill="currentColor" />
    </svg>
  );
}

export function StatsGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M5 13v6M12 8v11M19 4v15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

export function ChannelsGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M4 6h16M4 12h16M4 18h16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

export function AudioTracksGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M12 21a9 9 0 1 0-9-9c0 1.48.36 2.89 1 4.12l-1.5 3.5 3.5-1.5A8.9 8.9 0 0 0 12 21Z" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M8 12h8M8 16h4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function SettingsGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
