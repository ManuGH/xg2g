import { type RefObject } from 'react';
import { Button, Card, StatusChip } from '../../../components/ui';
import { useUiSurface } from '../../../context/UiSurfaceContext';
import type { VideoElementRef } from '../../../types/v3-player';
import type {
  PlaybackOrchestratorActions,
  V3PlayerViewState,
} from '../usePlaybackOrchestrator';
import styles from './V3Player.module.css';
import { DvrScrubSlider } from './DvrScrubSlider';
import { DropdownMenu } from './DropdownMenu';
import { ChannelsGlyph, FullscreenGlyph, PipGlyph, VolumeGlyph, AudioTracksGlyph, SettingsGlyph, PlayGlyph, PauseGlyph, StopGlyph, SeekBackGlyph, SeekForwardGlyph } from './playerControlGlyphs';

interface V3PlayerViewProps {
  containerRef: RefObject<HTMLDivElement | null>;
  videoRef: RefObject<VideoElementRef>;
  resumePrimaryActionRef: RefObject<HTMLButtonElement | null>;
  viewState: V3PlayerViewState;
  actions: PlaybackOrchestratorActions;
  /** Live-only: opens the in-player channel list. Absent => no Sender button. */
  onOpenChannels?: () => void;
  children?: React.ReactNode;
}

export function V3PlayerView({
  containerRef,
  videoRef,
  resumePrimaryActionRef,
  viewState,
  actions,
  onOpenChannels,
  children,
}: V3PlayerViewProps) {
  // On phone-sized surfaces apply the compact mobile player layout (full-bleed
  // video, repositioned chrome). The styles existed in V3Player.module.css but
  // were never wired up, so the player rendered letterboxed on phones.
  const { surface } = useUiSurface();
  const isCompactSurface = surface === 'small';
  return (
    <div
      ref={containerRef}
      className={[
        styles.container,
        'animate-enter',
        viewState.useOverlayLayout ? styles.overlay : null,
        viewState.userIdle ? styles.userIdle : null,
        isCompactSurface ? styles.surfaceCompact : null,
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
        {viewState.showNativeBufferingMask && (
          <div className={styles.nativeBufferingMask} aria-hidden="true"></div>
        )}
        {viewState.showStartupBackdrop && (
          <div className={styles.startupBackdrop} aria-hidden="true"></div>
        )}
        {viewState.showSpinnerCard && (
          <div
            className={[
              styles.spinnerOverlay,
              viewState.useNativeBufferingSafeOverlay ? styles.spinnerOverlaySafe : null,
            ].filter(Boolean).join(' ')}
            aria-live="polite"
          >
            <div className={styles.spinnerBadge}>
              {viewState.channelLogoUrl ? (
                <img
                  className={styles.startupChannelLogo}
                  src={viewState.channelLogoUrl}
                  alt={viewState.channelName || ''}
                  loading="lazy"
                />
              ) : viewState.channelName ? (
                <div className={styles.startupChannelInitials} aria-hidden="true">
                  {viewState.channelName.substring(0, 2)}
                </div>
              ) : (
                <div className={styles.startupRing} aria-hidden="true">
                  <span className={styles.startupRingArc}></span>
                  <span className={styles.startupRingCore}></span>
                </div>
              )}
            </div>
            <div className={styles.spinnerContent}>
              {viewState.channelName && <h2 className={styles.spinnerTitle}>{viewState.channelName}</h2>}
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
          {viewState.showSeekControls && (
            <div className={styles.seekSliderGroup}>
              <div className={styles.seekSliderRow}>
                <span className={styles.vodTime}>{viewState.startTimeDisplay}</span>
                <DvrScrubSlider
                  value={viewState.relativePosition}
                  max={viewState.windowDuration}
                  sliderClassName={styles.vodSlider}
                  onSeek={(offset) => actions.seekTo(viewState.seekableStart + offset)}
                  previewBaseUrl={viewState.dvrPreviewBaseUrl}
                  windowStartUnix={viewState.dvrPreviewWindowStartUnix}
                  segmentSeconds={viewState.dvrPreviewSegmentSeconds}
                />
                <span className={styles.vodTimeTotal}>{viewState.endTimeDisplay}</span>
              </div>
            </div>
          )}

          <div className={styles.controlsBottomRow}>
            <div className={styles.controlsLeft}>
              {viewState.showSeekControls && (
                <div className={styles.transportControls}>
                  <Button variant="ghost" size="sm" onClick={() => actions.seekBy(-15)} title={viewState.seekBack15sLabel} aria-label={viewState.seekBack15sLabel}>
                    <SeekBackGlyph /> 15s
                  </Button>

                  <Button
                    variant="primary"
                    size="icon"
                    className={styles.playPauseButton}
                    onClick={actions.togglePlayPause}
                    title={viewState.playPauseLabel}
                    aria-label={viewState.playPauseLabel}
                  >
                    {viewState.playPauseIcon === '⏸' ? <PauseGlyph /> : <PlayGlyph />}
                  </Button>

                  <Button variant="ghost" size="sm" onClick={() => actions.seekBy(60)} title={viewState.seekForward60sLabel} aria-label={viewState.seekForward60sLabel}>
                    <SeekForwardGlyph /> 60s
                  </Button>

                  {viewState.isLiveMode && (
                    <button
                      className={[styles.liveButton, viewState.isAtLiveEdge ? styles.liveButtonActive : null].filter(Boolean).join(' ')}
                      onClick={() => actions.seekToLiveEdge()}
                      title={viewState.liveButtonLabel}
                      aria-label={viewState.liveButtonLabel}
                    >
                      LIVE
                    </button>
                  )}
                </div>
              )}

              {!viewState.showSeekControls && viewState.showServiceInput && (
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
              )}

              {viewState.showManualStartButton && (
                <Button
                  onClick={() => actions.startStream()}
                  disabled={viewState.manualStartDisabled}
                >
                  <PlayGlyph /> {viewState.manualStartLabel}
                </Button>
              )}

              {viewState.showDvrModeButton && (
                <Button onClick={actions.enterDVRMode} title={viewState.dvrModeLabel}>
                  <PipGlyph /> DVR
                </Button>
              )}

              {/* Inlined Title */}
              {(viewState.programmeTitle || viewState.channelName) && (
                <div className={styles.inlineTitleGroup}>
                  {viewState.channelName && viewState.programmeTitle && viewState.programmeTitle !== viewState.channelName && (
                    <span className={styles.inlineChannelEyebrow}>{viewState.channelName}</span>
                  )}
                  <span className={styles.inlineProgrammeTitle}>{viewState.programmeTitle ?? viewState.channelName}</span>
                </div>
              )}
            </div>

            <div className={styles.utilityControls}>
              {onOpenChannels && (
                <Button
                  variant="ghost"
                  size="sm"
                  className={styles.channelsButton}
                  onClick={onOpenChannels}
                  title="Sender wechseln"
                  aria-label="Sender wechseln"
                >
                  <ChannelsGlyph />
                  <span className="sr-only">Sender</span>
                </Button>
              )}

              {viewState.audioTracks && viewState.audioTracks.length > 1 && (
                <DropdownMenu
                  icon={<AudioTracksGlyph />}
                  title="Tonspur"
                  activeId={viewState.activeAudioTrack}
                  onSelect={(id) => actions.changeAudioTrack(id as number)}
                  options={viewState.audioTracks.map((t) => ({
                    id: t.engineIndex !== undefined ? t.engineIndex : t.id,
                    label: t.label || t.name || t.language || `Track ${t.engineIndex !== undefined ? t.engineIndex : t.id}`,
                  }))}
                />
              )}

              <DropdownMenu
                icon={<SettingsGlyph />}
                title="Profil"
                activeId={viewState.explicitProfile}
                onSelect={(id) => actions.changeProfile(id as string)}
                options={[
                  { id: 'auto', label: 'Auto (Smart)' },
                  { id: 'direct', label: 'Direct Play' },
                  { id: 'quality', label: 'Quality' },
                  { id: 'compatible', label: 'Compatible' },
                  { id: 'repair', label: 'Repair' },
                ]}
              />

              {viewState.showNativeFullscreenButton && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={actions.enterNativeFullscreen}
                  title={viewState.nativeFullscreenTitle}
                >
                  <FullscreenGlyph /> {viewState.nativeFullscreenLabel}
                </Button>
              )}

              {viewState.showFullscreenButton && (
                <Button
                  variant="ghost"
                  size="sm"
                  active={viewState.fullscreenActive}
                  onClick={() => void actions.toggleFullscreen()}
                  title={viewState.fullscreenLabel}
                  aria-label={viewState.fullscreenLabel}
                >
                  <FullscreenGlyph />
                  <span className="sr-only">Vollbild</span>
                </Button>
              )}

              {viewState.showVolumeControls && (
                <div className={styles.volumeControl}>
                  <Button
                    variant="ghost"
                    size="sm"
                    className={[styles.audioToggleButton, viewState.audioToggleActive ? null : styles.audioMuted].filter(Boolean).join(' ')}
                    onClick={actions.toggleMute}
                    title={viewState.audioToggleLabel}
                    aria-label={viewState.audioToggleLabel}
                    aria-pressed={viewState.audioToggleActive}
                  >
                    <span className={styles.audioToggleIcon} aria-hidden="true">
                      <VolumeGlyph muted={!viewState.audioToggleActive} />
                    </span>
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
                  onClick={() => void actions.togglePiP()}
                  title={viewState.pipTitle}
                  aria-label={viewState.pipLabel}
                >
                  <PipGlyph />
                  <span className="sr-only">PiP</span>
                </Button>
              )}

              {/* Removed the stats button from the main UI for cleaner look */}
              
              {viewState.showStopButton && (
                <Button variant="danger" onClick={() => void actions.stopStream()}>
                  <StopGlyph /> {viewState.stopLabel}
                </Button>
              )}
            </div>
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
      {/* Render children inside container to allow proper overlays (like ChannelSwitcher) */}
      {children}
    </div>
  );
}
