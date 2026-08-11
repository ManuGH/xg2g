const { chromium } = require('playwright');
const fs = require('fs');

(async () => {
  console.log("=== P6.1c 3-Tier Driving Profile Automated Test (Playwright) ===");
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();

  page.on('console', msg => console.log('  [BROWSER LOG]', msg.text()));

  console.log("[1/7] Navigating to http://localhost:8899/...");
  await page.goto("http://localhost:8899/", { waitUntil: "domcontentloaded" });

  await page.waitForFunction(() => window.playbackMetrics !== undefined, { timeout: 10000 });

  // Stage 1: City / 5G (Unthrottled >12 Mbps) -> Expected 1080p
  console.log("\n--- [STAGE 1: City / 5G (Unthrottled)] ---");
  await page.evaluate(() => window.notifyStageChange(1, "City / 5G (>12 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=0'));
  await page.waitForTimeout(10000);

  let initialLevel = await page.evaluate(() => window.playbackMetrics ? window.playbackMetrics.initial_level : "pending");
  console.log(`[STAGE 1 RESULT] Level: ${initialLevel}`);

  // Stage 2: Normal LTE (4.0 Mbps) -> Expected 720p (2.8M fits)
  console.log("\n--- [STAGE 2: Normal LTE (4.0 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(2, "Normal LTE (4.0 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=4000'));
  await page.waitForTimeout(16000);

  // Stage 3: Bad Cell (1.8 Mbps) -> Expected 480p (1.4M fits)
  console.log("\n--- [STAGE 3: Bad Cell (1.8 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(3, "Bad Cell (1.8 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=1800'));
  await page.waitForTimeout(16000);

  // Stage 4: Tunnel Drop (Silent Drop 8s) -> Expected Stall & Buffer Drain
  console.log("\n--- [STAGE 4: Tunnel Drop (Silent 0 Mbps for 8s)] ---");
  await page.evaluate(() => window.notifyStageChange(4, "Tunnel Drop (Silent Drop 8s)"));
  await page.evaluate(() => fetch('/api/network?state=drop'));
  await page.waitForTimeout(8000);

  // Stage 5: Recovery LTE (4.0 Mbps) -> Expected Resume & Upswitch to 720p
  console.log("\n--- [STAGE 5: Recovery LTE (4.0 Mbps)] ---");
  await page.evaluate(() => window.notifyStageChange(5, "Recovery LTE (4.0 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=4000'));
  await page.waitForTimeout(18000);

  // Stage 6: City / 5G (Unthrottled >12 Mbps) -> Expected Upswitch to 1080p
  console.log("\n--- [STAGE 6: City / 5G (Unthrottled)] ---");
  await page.evaluate(() => window.notifyStageChange(6, "City / 5G (>12 Mbps)"));
  await page.evaluate(() => fetch('/api/network?state=online&kbps=0'));
  await page.waitForTimeout(18000);

  // Extract final metrics JSON
  const finalMetrics = await page.evaluate(() => window.playbackMetrics);

  try {
    const pidStr = fs.readFileSync("/tmp/xg2g_abr_p61c/ffmpeg.pid", "utf8").trim();
    finalMetrics.ffmpeg_pid = parseInt(pidStr, 10);
  } catch (e) {
    finalMetrics.ffmpeg_pid = "unknown";
  }

  console.log("\n================ METRICS DUMP JSON ================");
  console.log(JSON.stringify(finalMetrics, null, 2));
  console.log("===================================================\n");

  fs.writeFileSync("/tmp/xg2g_abr_p61c/metrics_dump.json", JSON.stringify(finalMetrics, null, 2));
  console.log("Metrics saved to /tmp/xg2g_abr_p61c/metrics_dump.json");

  await browser.close();
  process.exit(0);
})();
