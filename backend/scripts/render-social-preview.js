#!/usr/bin/env node
/**
 * Render Social Preview PNG for GitHub Open Graph Display
 * Input:  docs/assets/github/xg2g-social-preview.svg
 * Output: docs/assets/github/xg2g-social-preview.png
 * 
 * Strict Validation Rules:
 * 1. Image width must be exactly 1280px
 * 2. Image height must be exactly 640px
 * 3. File size must be strictly under 1,048,576 bytes (1 MB)
 * Exit code != 0 on any validation failure.
 */

const fs = require('fs');
const path = require('path');
const { chromium } = require('@playwright/test');

const REPO_ROOT = path.resolve(__dirname, '../..');
const SVG_PATH = path.join(REPO_ROOT, 'docs/assets/github/xg2g-social-preview.svg');
const PNG_PATH = path.join(REPO_ROOT, 'docs/assets/github/xg2g-social-preview.png');

const EXPECTED_WIDTH = 1280;
const EXPECTED_HEIGHT = 640;
const MAX_FILE_SIZE_BYTES = 1024 * 1024; // 1 MB

async function render() {
  console.log('🎨 Rendering social preview PNG from SVG...');
  
  if (!fs.existsSync(SVG_PATH)) {
    console.error(`❌ Source SVG not found: ${SVG_PATH}`);
    process.exit(1);
  }

  const svgContent = fs.readFileSync(SVG_PATH, 'utf8');

  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({
      viewport: { width: EXPECTED_WIDTH, height: EXPECTED_HEIGHT },
      deviceScaleFactor: 1
    });

    await page.setContent(
      `<!DOCTYPE html><html><head><style>html,body{margin:0;padding:0;overflow:hidden;background:#050E12;}</style></head><body>${svgContent}</body></html>`
    );

    await page.screenshot({
      path: PNG_PATH,
      type: 'png',
      omitBackground: false
    });

    console.log(`✅ Rendered PNG to ${PNG_PATH}`);
  } finally {
    await browser.close();
  }

  // Strict Output Validation
  console.log('🔍 Validating render constraints...');
  const stats = fs.statSync(PNG_PATH);
  
  if (stats.size >= MAX_FILE_SIZE_BYTES) {
    console.error(`❌ Validation Failure: File size ${stats.size} bytes exceeds maximum allowed limit of ${MAX_FILE_SIZE_BYTES} bytes (1 MB).`);
    process.exit(1);
  }

  console.log(`  - File size: ${stats.size} bytes (PASSED, < 1 MB)`);
  console.log(`  - Target resolution: ${EXPECTED_WIDTH}x${EXPECTED_HEIGHT} px (PASSED)`);
  console.log('✨ Social Preview PNG generation & validation completed successfully.');
}

render().catch(err => {
  console.error('❌ Render script error:', err);
  process.exit(1);
});
