/**
 * Safari DVR Diagnostic Tool
 *
 * This script helps diagnose DVR scrubber issues in Safari by checking:
 * - video.seekable TimeRanges (what Safari uses for scrubber)
 * - HLS manifest tags (EXT-X-START, PLAYLIST-TYPE, etc.)
 * - Video element state
 *
 * Usage:
 * 1. Open Safari Developer Console (Cmd+Option+C)
 * 2. Load this script or paste it into the console
 * 3. Run: diagnoseDVR()
 */

/* eslint-disable no-unused-vars */
/* global Hls */
function diagnoseDVR(recordingUrl) {
    const video = document.querySelector('video');

    if (!video) {
        console.error('❌ No video element found on page');
        return;
    }

    console.log('%c=== Safari DVR Diagnostic ===', 'font-weight: bold; font-size: 14px;');
    console.log('Video source:', video.src || video.currentSrc || 'Unknown');
    console.log('');

    // Check seekable ranges (critical for Safari DVR scrubber)
    console.log('%c📊 Seekable Ranges:', 'font-weight: bold;');
    if (video.seekable && video.seekable.length > 0) {
        for (let i = 0; i < video.seekable.length; i++) {
            const start = video.seekable.start(i);
            const end = video.seekable.end(i);
            const duration = end - start;

            console.table({
                'Seekable Range (s)': duration.toFixed(0),
                'Seekable Range (min)': (duration / 60).toFixed(1),
                'Start (s)': start.toFixed(2),
                'End (s)': end.toFixed(2),
                'Current Time (s)': video.currentTime.toFixed(2),
                'Playback Rate': video.playbackRate,
                'Ready State': getReadyStateLabel(video.readyState),
                'Network State': getNetworkStateLabel(video.networkState),
                'Expected Scrubber': duration > 1200 ? '✅ YES (≥20min)' : '❌ NO (<20min)'
            });

            if (duration < 1200) {
                console.warn('⚠️  Seekable range is less than 20 minutes.');
                console.warn('   Safari may show Live-UI only instead of DVR scrubber.');
                console.warn('   Wait for more segments to accumulate or check DVR configuration.');
            } else {
                console.log('✅ Seekable range is sufficient for DVR scrubber in Safari fullscreen');
            }
        }
    } else {
        console.warn('❌ No seekable ranges available');
        console.warn('   Possible causes: video not loaded, manifest issue, or stream just started');
    }

    console.log('');
    console.log('%c📺 Video Element Properties:', 'font-weight: bold;');
    console.table({
        'Duration': isFinite(video.duration) ? video.duration.toFixed(2) + 's' : 'Infinity (live)',
        'Current Time': video.currentTime.toFixed(2) + 's',
        'Paused': video.paused,
        'Muted': video.muted,
        'Volume': video.volume,
        'Playback Rate': video.playbackRate,
        'Ready State': getReadyStateLabel(video.readyState),
        'Network State': getNetworkStateLabel(video.networkState),
        'Video Width': video.videoWidth,
        'Video Height': video.videoHeight
    });

    // Check for HLS.js vs native
    console.log('');
    if (window.Hls && video.hls) {
        console.log('%c📡 HLS.js Detected (NOT Safari native):', 'font-weight: bold;');
        console.log('  Version:', Hls.version);
        console.log('  Note: This diagnostic is designed for Safari native HLS');
    } else {
        console.log('%c📱 Native HLS (Safari):', 'font-weight: bold;');
        console.log('  ✅ Using Safari\'s native HLS stack');
    }

    // Fetch and analyze manifest
    const manifestUrl = video.src || video.currentSrc;
    if (manifestUrl && manifestUrl.includes('.m3u8')) {
        console.log('');
        console.log('%c🔍 Fetching manifest...', 'font-weight: bold;');
        console.log('URL:', manifestUrl);

        fetch(manifestUrl)
            .then(r => {
                if (!r.ok) throw new Error(`HTTP ${r.status}`);
                return r.text();
            })
            .then(manifest => {
                console.log('');
                console.log('%c📄 Manifest Analysis:', 'font-weight: bold;');

                const hasEvent = manifest.includes('PLAYLIST-TYPE:EVENT');
                const hasVOD = manifest.includes('PLAYLIST-TYPE:VOD');
                const hasStart = manifest.includes('EXT-X-START');
                const hasPDT = manifest.includes('PROGRAM-DATE-TIME');
                const hasEndlist = manifest.includes('EXT-X-ENDLIST');

                console.table({
                    'PLAYLIST-TYPE:EVENT': hasEvent ? '✅' : '❌',
                    'PLAYLIST-TYPE:VOD': hasVOD ? '✅' : '⚪',
                    'EXT-X-START': hasStart ? '✅' : '❌ CRITICAL - DVR scrubber won\'t appear!',
                    'PROGRAM-DATE-TIME': hasPDT ? '✅' : '⚪',
                    'EXT-X-ENDLIST': hasEndlist ? '✅ (VOD)' : '⚪ (Live/DVR)'
                });

                if (hasStart) {
                    const startMatch = manifest.match(/EXT-X-START:TIME-OFFSET=(-?\d+)/);
                    if (startMatch) {
                        const offset = parseInt(startMatch[1]);
                        console.log('✅ Start offset:', Math.abs(offset) + 's (' + (Math.abs(offset) / 60).toFixed(1) + ' min)');
                    }
                }

                const segmentMatches = manifest.match(/\.ts|\.m4s/g);
                const segmentCount = segmentMatches ? segmentMatches.length : 0;
                console.log('📦 Segment count:', segmentCount);

                const targetDuration = manifest.match(/EXT-X-TARGETDURATION:(\d+)/);
                if (targetDuration) {
                    const totalDuration = segmentCount * parseInt(targetDuration[1]);
                    console.log('⏱️  Estimated duration:', totalDuration + 's (~' + (totalDuration / 60).toFixed(1) + ' min)');

                    if (totalDuration < 1200) {
                        console.warn('⚠️  Manifest duration < 20min - DVR scrubber may not appear');
                    }
                }

                console.log('');
                console.log('%c=== Diagnosis Summary ===', 'font-weight: bold; font-size: 14px;');

                if (hasStart && hasEvent && segmentCount >= 600) {
                    console.log('%c✅ PASS: Playlist is correctly configured for Safari DVR', 'color: green; font-weight: bold;');
                    console.log('   Expected behavior: Scrubber should appear in native fullscreen');
                } else {
                    console.log('%c❌ FAIL: Playlist configuration issues detected:', 'color: red; font-weight: bold;');
                    if (!hasStart) console.log('   - Missing EXT-X-START tag (CRITICAL)');
                    if (!hasEvent) console.log('   - Missing PLAYLIST-TYPE:EVENT');
                    if (segmentCount < 600) console.log('   - Too few segments (' + segmentCount + ' < 600)');
                }
            })
            .catch(err => {
                console.error('❌ Failed to fetch manifest:', err.message);
                console.log('Try accessing the URL directly in the Network tab');
            });
    } else {
        console.warn('⚠️  No .m3u8 manifest URL detected');
    }
}

function getReadyStateLabel(state) {
    const labels = ['HAVE_NOTHING', 'HAVE_METADATA', 'HAVE_CURRENT_DATA', 'HAVE_FUTURE_DATA', 'HAVE_ENOUGH_DATA'];
    return labels[state] || state;
}

function getNetworkStateLabel(state) {
    const labels = ['NETWORK_EMPTY', 'NETWORK_IDLE', 'NETWORK_LOADING', 'NETWORK_NO_SOURCE'];
    return labels[state] || state;
}

// Auto-run message
console.log('%c💡 Safari DVR Diagnostic Tool Loaded', 'font-size: 12px; font-weight: bold;');
console.log('%cRun diagnoseDVR() to check your Safari DVR configuration', 'font-style: italic;');
