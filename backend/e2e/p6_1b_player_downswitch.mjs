import { chromium } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

// Parse command line arguments: --stream-url <url> --signal-url <url> --hls-js-path <path>
let streamUrl = '';
let signalUrl = '';
let hlsJsPath = '';

for (let i = 2; i < process.argv.length; i++) {
  if (process.argv[i] === '--stream-url' && i + 1 < process.argv.length) {
    streamUrl = process.argv[++i];
  } else if (process.argv[i] === '--signal-url' && i + 1 < process.argv.length) {
    signalUrl = process.argv[++i];
  } else if (process.argv[i] === '--hls-js-path' && i + 1 < process.argv.length) {
    hlsJsPath = process.argv[++i];
  }
}

if (!streamUrl || !signalUrl) {
  console.error(JSON.stringify({ success: false, error: 'Missing --stream-url or --signal-url' }));
  process.exit(1);
}

if (!hlsJsPath || !fs.existsSync(hlsJsPath)) {
  console.error(JSON.stringify({ success: false, error: `hls.js file not found at: ${hlsJsPath}` }));
  process.exit(1);
}

const hlsJsCode = fs.readFileSync(hlsJsPath, 'utf8');

(async () => {
  const browser = await chromium.launch({
    headless: true,
    args: ['--autoplay-policy=no-user-gesture-required', '--no-sandbox']
  });

  const page = await browser.newPage();

  // Create HTML page with inline hls.js and video element
  const htmlContent = `
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>P6.1b Downswitch Test</title>
  <script>${hlsJsCode}</script>
</head>
<body>
  <video id="testVideo" muted autoplay playsinline style="width: 640px; height: 360px;"></video>
  <script>
    window.testState = {
      highStarted: false,
      highSignaled: false,
      downswitched: false,
      highLevelIndex: -1,
      lowLevelIndex: -1,
      currentTimeSamples: [],
      fatalErrors: [],
      hlsInstanceId: Math.random().toString(36).substring(7)
    };

    const video = document.getElementById('testVideo');
    if (!Hls.isSupported()) {
      window.testState.fatalErrors.push('Hls.isSupported() returned false');
    } else {
      const hls = new Hls({
        debug: false,
        enableWorker: false,
        maxBufferLength: 2,
        maxMaxBufferLength: 4,
        abrEmaFastLive: 0.5,
        abrEmaSlowLive: 1,
        abrBandWidthUpFactor: 0.5,
        abrBandWidthFactor: 0.75,
        maxStarvationDelay: 1,
        lowBufferWatchdogPeriod: 0.5
      });
      window.hlsInstance = hls;

      hls.loadSource('${streamUrl}');
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, (event, data) => {
        let maxBw = -1;
        let maxIdx = 0;
        data.levels.forEach((lvl, idx) => {
          console.error('[hls.js] Level ' + idx + ': ' + lvl.width + 'x' + lvl.height + ' @ ' + lvl.bitrate + ' bps');
          if (lvl.bitrate > maxBw) {
            maxBw = lvl.bitrate;
            maxIdx = idx;
          }
        });
        window.testState.highLevelIndex = maxIdx;
        hls.startLevel = maxIdx;
        console.error('[hls.js] Selected High Level Index: ' + maxIdx);
      });

      hls.on(Hls.Events.FRAG_LOADED, (event, data) => {
        if (data.frag && data.frag.level === window.testState.highLevelIndex) {
          if (!window.testState.highSignaled) {
            window.testState.highSignaled = true;
            console.error('[hls.js] High fragment loaded, signaling high_started to server');
            fetch('${signalUrl}', { method: 'POST' }).catch(() => {});
          }
        }
      });

      const handleLevelSwitch = (level) => {
        console.error('[hls.js] Level switch event to level ' + level);
        if (level === window.testState.highLevelIndex) {
          window.testState.highStarted = true;
        } else if (window.testState.highStarted && level !== window.testState.highLevelIndex) {
          window.testState.downswitched = true;
          window.testState.lowLevelIndex = level;
        }
      };

      hls.on(Hls.Events.LEVEL_SWITCHING, (event, data) => {
        handleLevelSwitch(data.level);
      });

      hls.on(Hls.Events.LEVEL_SWITCHED, (event, data) => {
        handleLevelSwitch(data.level);
      });

      hls.on(Hls.Events.ERROR, (event, data) => {
        console.error('[hls.js] ERROR: type=' + data.type + ', details=' + data.details + ', fatal=' + data.fatal);
        if (data.fatal) {
          window.testState.fatalErrors.push(data.type + ': ' + data.details);
        }
      });

      setInterval(() => {
        if (video && !video.paused) {
          window.testState.currentTimeSamples.push({
            time: video.currentTime,
            ts: Date.now()
          });
        }
      }, 250);
    }
  </script>
</body>
</html>
  `;

  await page.setContent(htmlContent);

  // Poll state inside browser until downswitch observed and playback progressed post-switch
  const deadline = Date.now() + 15000; // 15 seconds max
  let result = null;

  while (Date.now() < deadline) {
    const state = await page.evaluate(() => window.testState);
    if (state.fatalErrors.length > 0) {
      result = { success: false, error: 'Fatal hls.js error: ' + state.fatalErrors.join('; ') };
      break;
    }
    if (state.downswitched && state.currentTimeSamples.length > 8) {
      const firstSample = state.currentTimeSamples[0].time;
      const lastSample = state.currentTimeSamples[state.currentTimeSamples.length - 1].time;
      if (lastSample - firstSample >= 1.5) {
        result = {
          success: true,
          highLevelIndex: state.highLevelIndex,
          lowLevelIndex: state.lowLevelIndex,
          highStartedSignaled: state.highSignaled,
          downswitchedObserved: state.downswitched,
          sampleCount: state.currentTimeSamples.length,
          startTime: firstSample,
          endTime: lastSample,
          progressSeconds: lastSample - firstSample,
          fatalErrorCount: state.fatalErrors.length
        };
        break;
      }
    }
    await new Promise(r => setTimeout(r, 250));
  }

  if (!result) {
    const finalState = await page.evaluate(() => window.testState);
    result = {
      success: false,
      error: 'Timeout waiting for downswitch and playback progress',
      finalState
    };
  }

  await browser.close();
  console.log(JSON.stringify(result));
})();
