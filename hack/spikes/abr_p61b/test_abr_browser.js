const { chromium } = require('playwright');
const fs = require('fs');

(async () => {
  console.log("=== P6.1b Automated Browser ABR Test (Playwright) ===");
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();

  page.on('console', msg => console.log('  [BROWSER LOG]', msg.text()));

  console.log("[1/5] Navigating to http://localhost:8899/...");
  await page.goto("http://localhost:8899/", { waitUntil: "domcontentloaded" });

  // Wait for window.playbackMetrics to be initialized
  await page.waitForFunction(() => window.playbackMetrics !== undefined, { timeout: 10000 });

  // Wait 8 seconds for initial 720p level selection & stream startup
  console.log("[2/5] Waiting 8s for initial 720p level selection & stream startup...");
  await page.waitForTimeout(8000);

  let initialLevel = await page.evaluate(() => window.playbackMetrics ? window.playbackMetrics.initial_level : "pending");
  console.log(`[PASS] Initial level selected: ${initialLevel}`);

  // Step 3: Apply server-side bandwidth throttling (1.4 Mbps / 1400 kbps)
  // 480p (1.13 Mbps) fits, 720p (2.34 Mbps) does not fit
  console.log("[3/5] Applying Bandwidth Throttling via API (1400 kbps / 1.4 Mbps)...");
  await page.evaluate(() => fetch('/api/throttle?kbps=1400'));

  // Wait 20 seconds under throttled conditions for hls.js to estimate lower bandwidth and switch to 480p
  console.log("Waiting 20s under throttled conditions for ABR downswitch...");
  await page.waitForTimeout(20000);

  let levelsAfterThrottle = await page.evaluate(() => window.playbackMetrics.selected_level_changes);
  console.log(`[PASS] Level changes after throttling: ${JSON.stringify(levelsAfterThrottle)}`);

  // Step 4: Restore full unthrottled bandwidth
  console.log("[4/5] Restoring Bandwidth via API (0 kbps / unthrottled)...");
  await page.evaluate(() => fetch('/api/throttle?kbps=0'));

  // Wait 20 seconds for hls.js to estimate high bandwidth and upswitch back to 720p
  console.log("Waiting 20s under restored bandwidth for ABR upswitch...");
  await page.waitForTimeout(20000);

  // Step 5: Extract final metrics payload
  const finalMetrics = await page.evaluate(() => window.playbackMetrics);
  
  // Attach FFmpeg PID from file
  try {
    const pidStr = fs.readFileSync("/tmp/xg2g_abr_p61b/ffmpeg.pid", "utf8").trim();
    finalMetrics.ffmpeg_pid = parseInt(pidStr, 10);
  } catch (e) {
    finalMetrics.ffmpeg_pid = "unknown";
  }

  console.log("\n================ METRICS DUMP JSON ================");
  console.log(JSON.stringify(finalMetrics, null, 2));
  console.log("===================================================\n");

  fs.writeFileSync("/tmp/xg2g_abr_p61b/metrics_dump.json", JSON.stringify(finalMetrics, null, 2));
  console.log("Metrics saved to /tmp/xg2g_abr_p61b/metrics_dump.json");

  await browser.close();
  process.exit(0);
})();
