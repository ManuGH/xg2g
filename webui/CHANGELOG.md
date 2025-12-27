# Changelog

## [3.0.0] - 2025-12-27

### Added - V3 Player UX Enhancements

- **Robust Buffering**: Spinner now correctly reflects `waiting`, `stalled`, and `seeking` states.
- **Improved Error Handling**: Error toasts now clear automatically on successful playback (`playing` event).
- **Strict Latency Metrics**: Latency is now typed as `number | null` and guarded for live streams only (clamped to 0+).
- **Semantic Status**: Added `paused` and `stopped` states to `PlayerStatus` for accurate UI/telemetry.
- **Mobile Support**: Player controls are now always visible on touch devices (`hover: none`).
- **Unmount Safety**: Implemented forced stop intent on component unmount to prevent zombie sessions.
- **Accessibility**: Added `aria-live="polite"` to error toasts for screen reader support.
