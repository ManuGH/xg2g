const { chromium } = require('playwright');
const fs = require('fs');

(async () => {
  console.log("=== P6.1c 3-Tier Driving Profile Automated Test with Strict Assertions (Playwright) ===");
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();

  page.on('console', msg => console.log('  [BROWSER LOG]', msg.text()));

  console.log("[1/7] Navigating to http://localhost:8899/...");
  await page.goto("http://localhost:8899/", { waitUntil: "domcontentloaded" });

  await page.waitForFunction(() => window.playbackMetrics !== undefined, null, { timeout: 10000 });

  // Stage 1: City / 5G (Unthrottled >12 Mbps) -> Expect 1080p Initial Level
  console.log("\n--- [STAGE 1: City / 5G (Unthrottled)] ---");
  await page.evaluate(() => window.notifyStageChange(1, "City / 5G (>12 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=0'));
  await page.waitForTimeout(10000);

  let initialLevel = await page.evaluate(() => window.playbackMetrics ? window.playbackMetrics.initial_level : "pending");
  console.log(`[STAGE 1 RESULT] Initial Level: ${initialLevel}`);

  if (initialLevel !== "1080p") {
    console.error(`[ASSERTION FAIL] Stage 1 initial_level must be '1080p', got '${initialLevel}'`);
    process.exit(1);
  }

  // Stage 2: Normal LTE (3.5 Mbps) -> Expect 720p
  console.log("\n--- [STAGE 2: Normal LTE (3.5 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(2, "Normal LTE (3.5 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=3500'));
  await page.waitForTimeout(20000);

  let activeLevelStage2 = await page.evaluate(() => {
    const el = document.getElementById("currentLevel");
    return el ? el.textContent.trim() : "";
  });
  console.log(`[STAGE 2 RESULT] Active Level: ${activeLevelStage2}`);

  if (activeLevelStage2 !== "720p") {
    console.error(`[ASSERTION FAIL] Stage 2 active level must be '720p', got '${activeLevelStage2}'`);
    process.exit(1);
  }

  // Stage 3: Bad Cell (1.8 Mbps) -> Expect 480p
  console.log("\n--- [STAGE 3: Bad Cell (1.8 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(3, "Bad Cell (1.8 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=1800'));
  await page.waitForTimeout(20000);

  let activeLevelStage3 = await page.evaluate(() => {
    const el = document.getElementById("currentLevel");
    return el ? el.textContent.trim() : "";
  });
  let preTunnelStalls = await page.evaluate(() => window.playbackMetrics.rebuffer_count);

  console.log(`[STAGE 3 RESULT] Active Level: ${activeLevelStage3}, Pre-tunnel stalls: ${preTunnelStalls}`);

  if (activeLevelStage3 !== "480p") {
    console.error(`[ASSERTION FAIL] Stage 3 active level must be '480p', got '${activeLevelStage3}'`);
    process.exit(1);
  }
  if (preTunnelStalls > 0) {
    console.error(`[ASSERTION FAIL] Pre-tunnel stall occurred! rebuffer_count before tunnel: ${preTunnelStalls}`);
    process.exit(1);
  }

  // Stage 4: Tunnel Drop (Silent 0 Mbps for 8s) -> Expect Buffer Drain & Stall
  console.log("\n--- [STAGE 4: Tunnel Drop (Silent 0 Mbps for 8s)] ---");
  await page.evaluate(() => window.notifyStageChange(4, "Tunnel Drop (Silent Drop 8s)"));
  await page.evaluate(() => fetch('/api/network?state=drop'));
  await page.waitForTimeout(8000);

  // Stage 5: Recovery LTE (3.5 Mbps) -> Expect Auto Resume & Upswitch to 720p
  console.log("\n--- [STAGE 5: Recovery LTE (3.5 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(5, "Recovery LTE (3.5 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=3500'));
  await page.waitForTimeout(22000);

  let activeLevelStage5 = await page.evaluate(() => {
    const el = document.getElementById("currentLevel");
    return el ? el.textContent.trim() : "";
  });
  console.log(`[STAGE 5 RESULT] Active Level: ${activeLevelStage5}`);

  if (activeLevelStage5 !== "720p") {
    console.error(`[ASSERTION FAIL] Stage 5 recovery level must reach '720p', got '${activeLevelStage5}'`);
    process.exit(1);
  }

  // Stage 6: City / 5G (Unthrottled >12 Mbps) -> Expect 1080p
  console.log("\n--- [STAGE 6: City / 5G (Unthrottled)] ---");
  await page.evaluate(() => window.notifyStageChange(6, "City / 5G (>12 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=0'));

  // Allow MSE player to play through pre-buffered 720p segments and switch active rendering level to 1080p (pass null as arg2 for Playwright options)
  await page.waitForFunction(() => {
    const el = document.getElementById("currentLevel");
    return el && el.textContent.trim() === "1080p";
  }, null, { timeout: 45000 });

  let activeLevelStage6 = await page.evaluate(() => {
    const el = document.getElementById("currentLevel");
    return el ? el.textContent.trim() : "";
  });
  console.log(`[STAGE 6 RESULT] Active Level: ${activeLevelStage6}`);

  if (activeLevelStage6 !== "1080p") {
    console.error(`[ASSERTION FAIL] Stage 6 level must reach '1080p', got '${activeLevelStage6}'`);
    process.exit(1);
  }

  // Extract final metrics JSON
  const finalMetrics = await page.evaluate(() => window.playbackMetrics);

  try {
    const pidStr = fs.readFileSync("/tmp/xg2g_abr_p61c/ffmpeg.pid", "utf8").trim();
    finalMetrics.ffmpeg_pid = parseInt(pidStr, 10);
  } catch (e) {
    finalMetrics.ffmpeg_pid = "unknown";
  }

  let maxObservedBuffer = Math.max(...finalMetrics.buffer_seconds_before_switch, 0);
  console.log(`[BUFFER CHECK] Max observed buffer before level switch: ${maxObservedBuffer.toFixed(2)}s`);

  console.log("\n================ METRICS DUMP JSON ================");
  console.log(JSON.stringify(finalMetrics, null, 2));
  console.log("===================================================\n");

  fs.writeFileSync("/tmp/xg2g_abr_p61c/metrics_dump.json", JSON.stringify(finalMetrics, null, 2));
  console.log("Metrics saved to /tmp/xg2g_abr_p61c/metrics_dump.json");

  console.log("=== ALL P6.1c DRIVING PROFILE STAGE ASSERTIONS PASSED ===");
  await browser.close();
  process.exit(0);
})();
