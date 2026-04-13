import type { RefObject } from 'react';
import { Button, Card, StatusChip } from '../../../components/ui';
import type { VideoElementRef } from '../../../types/v3-player';
import type {
  PlaybackOrchestratorActions,
  V3PlayerViewState,
} from '../usePlaybackOrchestrator';
import styles from './V3Player.module.css';

interface V3PlayerViewProps {
  containerRef: RefObject<HTMLDivElement | null>;
  videoRef: RefObject<VideoElementRef>;
  resumePrimaryActionRef: RefObject<HTMLButtonElement | null>;
  viewState: V3PlayerViewState;
  actions: PlaybackOrchestratorActions;
}

export function V3PlayerView({
  containerRef,
  videoRef,
  resumePrimaryActionRef,
  viewState,
  actions,
}: V3PlayerViewProps) {
  return (
    <div
      ref={containerRef}
      className={[
        styles.container,
        'animate-enter',
        viewState.useOverlayLayout ? styles.overlay : null,
        viewState.userIdle ? styles.userIdle : null,
      ].filter(Boolean).join(' ')}
    >
      {viewState.showCloseButton && (
        <button
          onClick={() => void actions.stopStream()}
          className={styles.closeButton}
          aria-label={viewState.closeButtonLabel}
        >
          ✕
        </button>
      )}

      {viewState.showStatsOverlay && (
        <div className={styles.statsOverlay}>
          <Card variant="standard">
            <Card.Header>
              <Card.Title>{viewState.statsTitle}</Card.Title>
            </Card.Header>
            <Card.Content className={styles.statsGrid}>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{viewState.statusLabel}</span>
                <StatusChip state={viewState.statusChipState} label={viewState.statusChipLabel} />
              </div>
              {viewState.statsRows.map((row) => (
                <div className={styles.statsRow} key={row.label}>
                  <span className={styles.statsLabel}>{row.label}</span>
                  <span className={styles.statsValue}>{row.value}</span>
                </div>
              ))}
            </Card.Content>
          </Card>
        </div>
      )}

      <div
        className={[
          styles.videoWrapper,
          viewState.showNativeBufferingMask ? styles.videoWrapperMasked : null,
        ].filter(Boolean).join(' ')}
      >
        {viewState.channelName && viewState.showPlaybackChrome && (
          <h3 className={styles.overlayTitle}>{viewState.channelName}</h3>
        )}
        {viewState.showNativeBufferingMask && (
          <div className={styles.nativeBufferingMask} aria-hidden="true"></div>
        )}
        {viewState.showStartupBackdrop && (
          <div className={styles.startupBackdrop} aria-hidden="true"></div>
        )}
        {viewState.showStartupOverlay && (
          <div
            className={[
              styles.spinnerOverlay,
              viewState.useNativeBufferingSafeOverlay ? styles.spinnerOverlaySafe : null,
            ].filter(Boolean).join(' ')}
            aria-live="polite"
          >
            <div className={styles.spinnerBadge}>
              <div className={`${styles.spinner} spinner-base`}></div>
            </div>
            <div className={styles.spinnerContent}>
              <div className={styles.spinnerEyebrow}>{viewState.spinnerEyebrow}</div>
              {viewState.channelName && <h2 className={styles.spinnerTitle}>{viewState.channelName}</h2>}
              <div className={styles.spinnerStatusRow}>
                <StatusChip state={viewState.overlayStatusState} label={viewState.overlayStatusLabel} />
              </div>
              <div className={styles.spinnerLabel}>{viewState.spinnerLabel}</div>
              <div className={styles.spinnerSupport}>{viewState.spinnerSupport}</div>
              <div className={styles.spinnerMeta}>
                <div className={styles.spinnerProgressTrack} aria-hidden="true">
                  <div className={`${styles.spinnerProgressFill} animate-startup-progress`}></div>
                </div>
                <div className={styles.spinnerElapsed}>{viewState.startupElapsedLabel}</div>
              </div>
              {viewState.showOverlayStopAction && (
                <div className={styles.spinnerActions}>
                  <Button variant="danger" size="sm" onClick={() => void actions.stopStream()}>
                    ⏹ {viewState.overlayStopLabel}
                  </Button>
                </div>
              )}
            </div>
          </div>
        )}

        <video
          ref={videoRef}
          controls={false}
          playsInline
          webkit-playsinline=""
          x-webkit-airplay="allow"
          preload="metadata"
          autoPlay={viewState.autoPlay}
          className={[
            styles.videoElement,
            viewState.hideVideoElement ? styles.videoElementHidden : null,
          ].filter(Boolean).join(' ')}
        />
      </div>

      {viewState.error && (
        <div className={styles.errorToast} aria-live="polite" role="alert">
          <div className={styles.errorMain}>
            <span className={styles.errorText}>⚠ {viewState.error.title}</span>
            {viewState.error.retryable ? (
              <Button variant="secondary" size="sm" onClick={() => void actions.retry()}>
                {viewState.errorRetryLabel}
              </Button>
            ) : null}
          </div>
          {viewState.errorTelemetryRows.length > 0 && (
            <div className={styles.errorTelemetry}>
              {viewState.errorTelemetryRows.map((row) => (
                <div className={styles.errorTelemetryRow} key={row.label}>
                  <span className={styles.errorTelemetryLabel}>{row.label}</span>
                  <span className={styles.errorTelemetryValue}>{row.value}</span>
                </div>
              ))}
            </div>
          )}
          {viewState.errorDetailToggleLabel && (
            <button
              onClick={actions.toggleErrorDetails}
              className={styles.errorDetailsButton}
            >
              {viewState.errorDetailToggleLabel}
            </button>
          )}
          {viewState.showErrorDetails && viewState.error.detail && (
            <div className={styles.errorDetailsContent}>
              <pre className={styles.errorDetailsPre}>{viewState.error.detail}</pre>
              <br />
              {viewState.errorSessionLabel}
            </div>
          )}
        </div>
      )}

      {viewState.showPlaybackChrome && (
        <div className={styles.controlsHeader}>
          {viewState.showSeekControls ? (
            <div className={[styles.vodControls, styles.seekControls].join(' ')}>
              <div className={styles.seekButtons}>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(-900)} title={viewState.seekBack15mLabel} aria-label={viewState.seekBack15mLabel}>
                  ↺ 15m
                </Button>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(-60)} title={viewState.seekBack60sLabel} aria-label={viewState.seekBack60sLabel}>
                  ↺ 60s
                </Button>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(-15)} title={viewState.seekBack15sLabel} aria-label={viewState.seekBack15sLabel}>
                  ↺ 15s
                </Button>
              </div>

              <Button
                variant="primary"
                size="icon"
                className={styles.playPauseButton}
                onClick={actions.togglePlayPause}
                title={viewState.playPauseLabel}
                aria-label={viewState.playPauseLabel}
              >
                {viewState.playPauseIcon}
              </Button>

              <div className={styles.seekSliderGroup}>
                <span className={styles.vodTime}>{viewState.startTimeDisplay}</span>
                <input
                  type="range"
                  min="0"
                  max={viewState.windowDuration}
                  step="0.1"
                  className={styles.vodSlider}
                  value={viewState.relativePosition}
                  onChange={(e) => {
                    const newVal = parseFloat(e.target.value);
                    actions.seekTo(viewState.seekableStart + newVal);
                  }}
                />
                <span className={styles.vodTimeTotal}>{viewState.endTimeDisplay}</span>
              </div>

              <div className={styles.seekButtons}>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(15)} title={viewState.seekForward15sLabel} aria-label={viewState.seekForward15sLabel}>
                  +15s
                </Button>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(60)} title={viewState.seekForward60sLabel} aria-label={viewState.seekForward60sLabel}>
                  +60s
                </Button>
                <Button variant="ghost" size="sm" onClick={() => actions.seekBy(900)} title={viewState.seekForward15mLabel} aria-label={viewState.seekForward15mLabel}>
                  +15m
                </Button>
              </div>

              {viewState.isLiveMode && (
                <button
                  className={[styles.liveButton, viewState.isAtLiveEdge ? styles.liveButtonActive : null].filter(Boolean).join(' ')}
                  onClick={() => actions.seekTo(viewState.seekableEnd)}
                  title={viewState.liveButtonLabel}
                  aria-label={viewState.liveButtonLabel}
                >
                  LIVE
                </button>
              )}
            </div>
          ) : (
            viewState.showServiceInput && (
              <input
                type="text"
                className={styles.serviceInput}
                value={viewState.serviceRef}
                onChange={(e) => actions.updateServiceRef(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    actions.submitServiceRef(e.currentTarget.value);
                  }
                }}
              />
            )
          )}

          {viewState.showManualStartButton && (
            <Button
              onClick={() => actions.startStream()}
              disabled={viewState.manualStartDisabled}
            >
              ▶ {viewState.manualStartLabel}
            </Button>
          )}

          {viewState.showDvrModeButton && (
            <Button onClick={actions.enterDVRMode} title={viewState.dvrModeLabel}>
              📺 DVR
            </Button>
          )}

          <div className={styles.utilityControls}>
            {viewState.showNativeFullscreenButton && (
              <Button
                variant="ghost"
                size="sm"
                onClick={actions.enterNativeFullscreen}
                title={viewState.nativeFullscreenTitle}
              >
                TV {viewState.nativeFullscreenLabel}
              </Button>
            )}

            {viewState.showFullscreenButton && (
              <Button
                variant="ghost"
                size="sm"
                active={viewState.fullscreenActive}
                onClick={() => void actions.toggleFullscreen()}
                title={viewState.fullscreenLabel}
              >
                ⛶ {viewState.fullscreenLabel}
              </Button>
            )}

            {viewState.showVolumeControls && (
              <div className={styles.volumeControl}>
                <Button
                  variant={viewState.audioToggleActive ? 'ghost' : 'primary'}
                  size="sm"
                  className={styles.audioToggleButton}
                  onClick={actions.toggleMute}
                  title={viewState.audioToggleLabel}
                  aria-label={viewState.audioToggleLabel}
                  aria-pressed={viewState.audioToggleActive}
                >
                  <span className={styles.audioToggleIcon} aria-hidden="true">{viewState.audioToggleIcon}</span>
                  <span>{viewState.audioToggleLabel}</span>
                </Button>
                {viewState.canAdjustVolume ? (
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.05"
                    className={styles.volumeSlider}
                    value={viewState.volume}
                    onChange={(e) => actions.changeVolume(parseFloat(e.target.value))}
                  />
                ) : (
                  <span className={styles.deviceVolumeHint}>{viewState.deviceVolumeHint}</span>
                )}
              </div>
            )}

            {viewState.showPipButton && (
              <Button
                variant="ghost"
                size="sm"
                active={viewState.pipActive}
                onClick={() => void actions.togglePiP()}
                title={viewState.pipTitle}
              >
                📺 {viewState.pipLabel}
              </Button>
            )}

            <Button
              variant="ghost"
              size="sm"
              active={viewState.statsActive}
              onClick={actions.toggleStats}
              title={viewState.statsTitle}
            >
              📊 {viewState.statsLabel}
            </Button>

            {viewState.showStopButton && (
              <Button variant="danger" onClick={() => void actions.stopStream()}>
                ⏹ {viewState.stopLabel}
              </Button>
            )}
          </div>
        </div>
      )}

      {viewState.showResumeOverlay && viewState.resumePositionSeconds !== null && (
        <div className={styles.resumeOverlay}>
          <div className={styles.resumeContent}>
            <h3>{viewState.resumeTitle}</h3>
            <p>{viewState.resumePrompt}</p>
            <div className={styles.resumeActions}>
              <Button
                ref={resumePrimaryActionRef}
                autoFocus
                onClick={() => actions.resumeFrom(viewState.resumePositionSeconds!)}
              >
                {viewState.resumeActionLabel}
              </Button>
              <Button variant="secondary" onClick={actions.startOver}>
                {viewState.startOverLabel}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
