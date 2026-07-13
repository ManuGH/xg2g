import { useEffect, useMemo, useRef, useState, type CSSProperties, type RefObject } from 'react';
import type { Service } from '../../../client-ts';
import { Button, Card, StatusChip } from '../../../components/ui';
import { useUiSurface } from '../../../context/UiSurfaceContext';
import type { VideoElementRef } from '../../../types/v3-player';
import type {
  PlaybackOrchestratorActions,
  V3PlayerViewState,
} from '../usePlaybackOrchestrator';
import styles from './V3Player.module.css';
import { DvrScrubSlider } from './DvrScrubSlider';

interface V3PlayerViewProps {
  containerRef: RefObject<HTMLDivElement | null>;
  videoRef: RefObject<VideoElementRef>;
  resumePrimaryActionRef: RefObject<HTMLButtonElement | null>;
  viewState: V3PlayerViewState;
  actions: PlaybackOrchestratorActions;
  channelList?: Service[];
  currentChannel?: Service;
  onSelectChannel?: (channel: Service) => void;
}

function channelIdentity(channel: Service | undefined): string {
  return String(channel?.serviceRef || channel?.id || '');
}

function channelFallbackIdentity(channel: Service | undefined): string {
  return [
    channelIdentity(channel),
    channel?.number,
    channel?.name,
    channel?.group,
  ].filter(Boolean).join('|');
}

function isSameChannel(left: Service | undefined, right: Service | undefined): boolean {
  const leftStableId = channelIdentity(left);
  const rightStableId = channelIdentity(right);

  if (leftStableId && rightStableId) {
    return leftStableId === rightStableId;
  }

  return channelFallbackIdentity(left) !== '' && channelFallbackIdentity(left) === channelFallbackIdentity(right);
}

function getChannelInitial(channel: Service): string {
  const label = String(channel.name || channel.number || '?').trim();
  return label.charAt(0).toUpperCase() || '?';
}

function calculateContainedVideoRect(wrapper: HTMLDivElement, video: VideoElementRef) {
  const wrapperWidth = wrapper.clientWidth;
  const wrapperHeight = wrapper.clientHeight;

  if (wrapperWidth <= 0 || wrapperHeight <= 0) {
    return null;
  }

  const intrinsicWidth = video?.videoWidth && video.videoWidth > 0 ? video.videoWidth : 16;
  const intrinsicHeight = video?.videoHeight && video.videoHeight > 0 ? video.videoHeight : 9;
  const videoAspect = intrinsicWidth / intrinsicHeight;
  const wrapperAspect = wrapperWidth / wrapperHeight;

  if (wrapperAspect > videoAspect) {
    const height = wrapperHeight;
    const width = Math.round(height * videoAspect);
    return {
      left: Math.round((wrapperWidth - width) / 2),
      top: 0,
      width,
      height,
    };
  }

  const width = wrapperWidth;
  const height = Math.round(width / videoAspect);
  return {
    left: 0,
    top: Math.round((wrapperHeight - height) / 2),
    width,
    height,
  };
}

export function V3PlayerView({
  containerRef,
  videoRef,
  resumePrimaryActionRef,
  viewState,
  actions,
  channelList = [],
  currentChannel,
  onSelectChannel,
}: V3PlayerViewProps) {
  // On phone-sized surfaces apply the compact mobile player layout (full-bleed
  // video, repositioned chrome). The styles existed in V3Player.module.css but
  // were never wired up, so the player rendered letterboxed on phones.
  const { surface } = useUiSurface();
  const isCompactSurface = surface === 'small';
  const videoWrapperRef = useRef<HTMLDivElement>(null);
  const [videoOverlayFrameStyle, setVideoOverlayFrameStyle] = useState<CSSProperties | undefined>();
  const [channelPanelOpen, setChannelPanelOpen] = useState(false);
  const [channelSearch, setChannelSearch] = useState('');
  const canShowChannelSelector = Boolean(onSelectChannel && channelList.length > 0);
  const visibleChannels = useMemo(() => {
    const query = channelSearch.trim().toLowerCase();

    if (!query) {
      return channelList;
    }

    return channelList.filter((channel) => [
      channel.name,
      channel.number,
      channel.group,
      channel.serviceRef,
    ].some((value) => String(value || '').toLowerCase().includes(query)));
  }, [channelList, channelSearch]);

  useEffect(() => {
    if (!canShowChannelSelector && channelPanelOpen) {
      setChannelPanelOpen(false);
    }
  }, [canShowChannelSelector, channelPanelOpen]);

  useEffect(() => {
    if (!channelPanelOpen) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setChannelPanelOpen(false);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [channelPanelOpen]);

  useEffect(() => {
    const wrapper = videoWrapperRef.current;
    const initialVideo = videoRef.current;

    if (!wrapper) {
      return undefined;
    }

    let animationFrame = 0;
    const measure = () => {
      window.cancelAnimationFrame(animationFrame);
      animationFrame = window.requestAnimationFrame(() => {
        const rect = calculateContainedVideoRect(wrapper, videoRef.current);

        if (!rect) {
          setVideoOverlayFrameStyle(undefined);
          return;
        }

        const nextStyle = {
          left: `${rect.left}px`,
          top: `${rect.top}px`,
          width: `${rect.width}px`,
          height: `${rect.height}px`,
        };

        setVideoOverlayFrameStyle((current) => (
          current?.left === nextStyle.left &&
          current?.top === nextStyle.top &&
          current?.width === nextStyle.width &&
          current?.height === nextStyle.height
            ? current
            : nextStyle
        ));
      });
    };

    measure();

    const resizeObserver = typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(measure)
      : null;

    resizeObserver?.observe(wrapper);
    if (initialVideo) {
      resizeObserver?.observe(initialVideo);
      initialVideo.addEventListener('loadedmetadata', measure);
      initialVideo.addEventListener('loadeddata', measure);
      initialVideo.addEventListener('resize', measure);
    }
    window.addEventListener('resize', measure);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      resizeObserver?.disconnect();
      if (initialVideo) {
        initialVideo.removeEventListener('loadedmetadata', measure);
        initialVideo.removeEventListener('loadeddata', measure);
        initialVideo.removeEventListener('resize', measure);
      }
      window.removeEventListener('resize', measure);
    };
  }, [videoRef]);

  const handleChannelSelect = (channel: Service) => {
    setChannelPanelOpen(false);
    setChannelSearch('');
    onSelectChannel?.(channel);
  };

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

      <div
        ref={videoWrapperRef}
        className={[
          styles.videoWrapper,
          viewState.showNativeBufferingMask ? styles.videoWrapperMasked : null,
        ].filter(Boolean).join(' ')}
      >
        <div className={styles.videoOverlayFrame} style={videoOverlayFrameStyle}>
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
          {viewState.showPlaybackChrome && (viewState.programmeTitle || viewState.channelName) && (
            <div className={styles.overlayTitle}>
              {viewState.channelName && viewState.programmeTitle && viewState.programmeTitle !== viewState.channelName && (
                <span className={styles.overlayChannelEyebrow}>{viewState.channelName}</span>
              )}
              <span className={styles.overlayProgrammeTitle}>{viewState.programmeTitle ?? viewState.channelName}</span>
              {viewState.programmeDesc && (
                <span className={styles.overlayProgrammeDesc}>{viewState.programmeDesc}</span>
              )}
            </div>
          )}
        </div>
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
              <div className={[styles.seekButtons, styles.seekBackButtons].join(' ')}>
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
                <span className={styles.currentPositionLabel}>{viewState.currentPositionDisplay}</span>
                <div className={styles.seekSliderRow}>
                  <span className={styles.vodTime}>{viewState.startTimeDisplay}</span>
                  <DvrScrubSlider
                    value={viewState.relativePosition}
                    max={viewState.windowDuration}
                    sliderClassName={styles.vodSlider}
                    onSeek={(offset) => actions.seekTo(viewState.seekableStart + offset)}
                    previewBaseUrl={viewState.dvrPreviewBaseUrl}
                    windowStartUnix={viewState.dvrPreviewWindowStartUnix}
                  />
                  <span className={styles.vodTimeTotal}>{viewState.endTimeDisplay}</span>
                </div>
              </div>

              <div className={[styles.seekButtons, styles.seekForwardButtons].join(' ')}>
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
                  onClick={() => actions.seekToLiveEdge()}
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
            {canShowChannelSelector && (
              <Button
                variant="ghost"
                size="sm"
                className={styles.channelListButton}
                active={channelPanelOpen}
                onPointerDown={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  setChannelPanelOpen((open) => !open);
                }}
                onClick={(event) => {
                  event.stopPropagation();
                }}
                onKeyDown={(event) => {
                  if (event.key !== 'Enter' && event.key !== ' ') {
                    return;
                  }
                  event.preventDefault();
                  event.stopPropagation();
                  setChannelPanelOpen((open) => !open);
                }}
                title="Senderliste"
                aria-label={channelPanelOpen ? 'Senderliste schließen' : 'Senderliste öffnen'}
              >
                <span className={styles.channelListIcon} aria-hidden="true">☷</span>
                <span>Sender</span>
              </Button>
            )}

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
                <span className={styles.fullscreenGlyph} aria-hidden="true"></span>
                <span>{viewState.fullscreenLabel}</span>
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

      {canShowChannelSelector && channelPanelOpen && (
        <div className={styles.channelPanelLayer}>
          <button
            type="button"
            className={styles.channelPanelScrim}
            aria-label="Senderliste schließen"
            onClick={() => setChannelPanelOpen(false)}
          />
          <aside className={styles.channelPanel} role="dialog" aria-label="Senderliste">
            <div className={styles.channelPanelHeader}>
              <div>
                <div className={styles.channelPanelEyebrow}>Live TV</div>
                <h2 className={styles.channelPanelTitle}>Sender</h2>
              </div>
              <button
                type="button"
                className={styles.channelPanelClose}
                aria-label="Senderliste schließen"
                onClick={() => setChannelPanelOpen(false)}
              >
                ✕
              </button>
            </div>
            <input
              className={styles.channelSearchInput}
              type="search"
              placeholder="Sender suchen"
              value={channelSearch}
              onChange={(event) => setChannelSearch(event.target.value)}
              autoFocus
            />
            <div className={styles.channelList} role="listbox" aria-label="Sender">
              {visibleChannels.length > 0 ? (
                visibleChannels.map((channel, index) => {
                  const current = isSameChannel(channel, currentChannel);
                  const channelKey = channelFallbackIdentity(channel) || `channel-${index}`;
                  const channelName = channel.name || channel.serviceRef || 'Unbenannter Sender';

                  return (
                    <button
                      type="button"
                      key={channelKey}
                      role="option"
                      aria-selected={current}
                      className={[
                        styles.channelOption,
                        current ? styles.channelOptionActive : null,
                      ].filter(Boolean).join(' ')}
                      onClick={() => handleChannelSelect(channel)}
                    >
                      <span className={styles.channelLogoWrap}>
                        {channel.logoUrl ? (
                          <img className={styles.channelLogo} src={channel.logoUrl} alt="" loading="lazy" />
                        ) : (
                          <span className={styles.channelLogoFallback}>{getChannelInitial(channel)}</span>
                        )}
                      </span>
                      <span className={styles.channelOptionText}>
                        <span className={styles.channelOptionName}>{channelName}</span>
                        <span className={styles.channelOptionMeta}>
                          {channel.number ? `#${channel.number}` : 'Live'}{channel.group ? ` · ${channel.group}` : ''}
                        </span>
                      </span>
                      {current && <span className={styles.channelNowBadge}>Jetzt</span>}
                    </button>
                  );
                })
              ) : (
                <div className={styles.channelEmptyState}>Keine Sender gefunden.</div>
              )}
            </div>
          </aside>
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
