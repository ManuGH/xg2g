// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useTranslation } from 'react-i18next';
import styles from '../Settings.module.css';

interface PlaybackSettingsSectionProps {
  audioMode: 'stereo' | 'surround';
  setAudioMode: (mode: 'stereo' | 'surround') => void;
  dvrMode: 'live_only' | '1h' | '2h' | '4h';
  setDvrMode: (mode: 'live_only' | '1h' | '2h' | '4h') => void;
}

export default function PlaybackSettingsSection({
  audioMode,
  setAudioMode,
  dvrMode,
  setDvrMode,
}: PlaybackSettingsSectionProps) {
  const { t } = useTranslation();

  return (
    <div className={styles.section}>
      <h2>{t('settings.streaming.title')}</h2>
      <p className={styles.subtitle}>{t('settings.streaming.engineSubtitle')}</p>

      <div className={styles.capabilityGrid}>
        <div className={styles.capabilityCard}>
          <div className={styles.capabilityCardHeader}>
            <span className={[styles.capabilityBadge, styles.capabilityBadgeInfo].join(' ')}>
              ADAPTIVE ENGINE
            </span>
            <strong className={styles.capabilityCardTitle}>CPU & GPU Transcoding</strong>
          </div>
          <p className={styles.capabilityCardCopy}>
            {t('settings.streaming.cardEngineText')}
          </p>
        </div>

        <div className={styles.capabilityCard}>
          <div className={styles.capabilityCardHeader}>
            <span className={[styles.capabilityBadge, styles.capabilityBadgeSuccess].join(' ')}>
              CODECS
            </span>
            <strong className={styles.capabilityCardTitle}>AV1 · HEVC · H.264 · MPEG-2</strong>
          </div>
          <p className={styles.capabilityCardCopy}>
            {t('settings.streaming.cardCodecsText')}
          </p>
        </div>

        <div className={styles.capabilityCard}>
          <div className={styles.capabilityCardHeader}>
            <span className={[styles.capabilityBadge, styles.capabilityBadgeInfo].join(' ')}>
              CONTAINER
            </span>
            <strong className={styles.capabilityCardTitle}>fMP4 · CMAF · MPEG-TS</strong>
          </div>
          <p className={styles.capabilityCardCopy}>
            {t('settings.streaming.cardContainersText')}
          </p>
        </div>

        <div className={styles.capabilityCard}>
          <div className={styles.capabilityCardHeader}>
            <span className={[styles.capabilityBadge, styles.capabilityBadgeWarning].join(' ')}>
              DEINTERLACING
            </span>
            <strong className={styles.capabilityCardTitle}>Interlaced vs. Progressive</strong>
          </div>
          <p className={styles.capabilityCardCopy}>
            {t('settings.streaming.cardInterlacedText')}
          </p>
        </div>
      </div>

      <div className={[styles.group, styles.optionGroup].join(' ')}>
        <label className={styles.optionGroupTitle}>{t('settings.streaming.audioMode.title')}</label>
        <div className={styles.optionList}>
          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="audioMode"
              value="stereo"
              checked={audioMode === 'stereo'}
              onChange={() => {
                setAudioMode('stereo');
                try { localStorage.setItem('xg2g.settings.audioMode', 'stereo'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.audioMode.stereo.label')}</div>
              <div className={styles.hint}>
                {t('settings.streaming.audioMode.stereo.hint')}
              </div>
            </div>
          </label>

          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="audioMode"
              value="surround"
              checked={audioMode === 'surround'}
              onChange={() => {
                setAudioMode('surround');
                try { localStorage.setItem('xg2g.settings.audioMode', 'surround'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.audioMode.surround.label')}</div>
              <div className={styles.hint}>
                {t('settings.streaming.audioMode.surround.hint')}
                <div className={styles.warningHint}>
                  ⚠️ <strong>{t('settings.streaming.audioMode.surround.warningTitle')}</strong> {t('settings.streaming.audioMode.surround.warningText')}
                </div>
              </div>
            </div>
          </label>
        </div>
      </div>

      <div className={[styles.group, styles.optionGroup].join(' ')}>
        <label className={styles.optionGroupTitle}>{t('settings.streaming.dvrMode.title')}</label>
        <div className={styles.optionList}>
          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="dvrMode"
              value="live_only"
              checked={dvrMode === 'live_only'}
              onChange={() => {
                setDvrMode('live_only');
                try { localStorage.setItem('xg2g.settings.dvrMode', 'live_only'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.liveOnly.label')}</div>
              <div className={styles.hint}>{t('settings.streaming.dvrMode.liveOnly.hint')}</div>
            </div>
          </label>

          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="dvrMode"
              value="1h"
              checked={dvrMode === '1h'}
              onChange={() => {
                setDvrMode('1h');
                try { localStorage.setItem('xg2g.settings.dvrMode', '1h'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr1h.label')}</div>
              <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr1h.hint')}</div>
            </div>
          </label>

          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="dvrMode"
              value="2h"
              checked={dvrMode === '2h'}
              onChange={() => {
                setDvrMode('2h');
                try { localStorage.setItem('xg2g.settings.dvrMode', '2h'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr2h.label')}</div>
              <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr2h.hint')}</div>
            </div>
          </label>

          <label className={styles.optionChoice}>
            <input
              type="radio"
              name="dvrMode"
              value="4h"
              checked={dvrMode === '4h'}
              onChange={() => {
                setDvrMode('4h');
                try { localStorage.setItem('xg2g.settings.dvrMode', '4h'); } catch { /* ignore */ }
              }}
              className={styles.optionInput}
            />
            <div>
              <div className={styles.optionLabel}>{t('settings.streaming.dvrMode.dvr4h.label')}</div>
              <div className={styles.hint}>{t('settings.streaming.dvrMode.dvr4h.hint')}</div>
            </div>
          </label>
        </div>
      </div>
    </div>
  );
}
