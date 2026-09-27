import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const DOCS_APP_DIR = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(DOCS_APP_DIR, '../..');

const SRC_CONTENT_DIR = path.join(DOCS_APP_DIR, 'src/content/docs');
const PUBLIC_DIR = path.join(DOCS_APP_DIR, 'public');

// Ensure destination directories exist
fs.mkdirSync(SRC_CONTENT_DIR, { recursive: true });
fs.mkdirSync(PUBLIC_DIR, { recursive: true });

// Copy public assets
const assetsSrc = path.join(REPO_ROOT, 'docs/assets');
const assetsDest = path.join(PUBLIC_DIR, 'assets');
if (fs.existsSync(assetsSrc)) {
  fs.cpSync(assetsSrc, assetsDest, { recursive: true });
}

// Copy OpenAPI spec
const openapiSrc = path.join(REPO_ROOT, 'backend/api/openapi.yaml');
const openapiDest = path.join(PUBLIC_DIR, 'openapi.yaml');
if (fs.existsSync(openapiSrc)) {
  fs.copyFileSync(openapiSrc, openapiDest);
}

// Copy llms.txt if present
const llmsSrc = path.join(REPO_ROOT, 'llms.txt');
const llmsDest = path.join(PUBLIC_DIR, 'llms.txt');
if (fs.existsSync(llmsSrc)) {
  fs.copyFileSync(llmsSrc, llmsDest);
}

// Source directories to sync
const SECTIONS = [
  { src: 'docs/tutorials', dest: 'tutorials' },
  { src: 'docs/how-to', dest: 'how-to' },
  { src: 'docs/reference', dest: 'reference' },
  { src: 'docs/explanation', dest: 'explanation' },
  { src: 'docs/guides', dest: 'guides' },
  { src: 'docs/arch', dest: 'arch' },
  { src: 'docs/ops', dest: 'ops' },
  { src: 'docs/ADR', dest: 'adr' },
  { src: 'docs/release', dest: 'release' },
];

function titleFromFilename(filename) {
  const base = path.basename(filename, path.extname(filename));
  return base
    .replace(/[_-]/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function processMarkdown(content, filename) {
  let title = '';
  let body = content;

  // Check for existing frontmatter
  const frontmatterMatch = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/);
  if (frontmatterMatch) {
    const fm = frontmatterMatch[1];
    const titleMatch = fm.match(/^title:\s*["']?(.*?)["']?$/m);
    if (titleMatch) {
      title = titleMatch[1];
    }
  }

  if (!title) {
    // Extract first H1
    const h1Match = body.match(/^#\s+(.+)$/m);
    if (h1Match) {
      title = h1Match[1].trim();
      // Remove first H1 so Starlight doesn't double-render the title
      body = body.replace(/^#\s+.+$/m, '').trimStart();
    } else {
      title = titleFromFilename(filename);
    }
  }

  // Sanitize title for YAML
  const safeTitle = title.replace(/"/g, '\\"').replace(/[\r\n]/g, ' ');

  // Rewrite relative links:
  // 1. .md links to lowercase without .md or lowercased
  body = body.replace(/\]\(([^)]+?)\.md(#[^)]+)?\)/g, (match, p1, p2) => {
    // Ignore external URLs
    if (p1.startsWith('http://') || p1.startsWith('https://')) return match;
    const hash = p2 || '';
    // lowercase relative path
    const lower = p1.toLowerCase();
    return `](${lower}/${hash})`;
  });

  if (!frontmatterMatch) {
    return `---\ntitle: "${safeTitle}"\n---\n\n${body}`;
  }

  return content;
}

function syncDirectory(srcDir, destDir) {
  if (!fs.existsSync(srcDir)) return;
  fs.mkdirSync(destDir, { recursive: true });

  const entries = fs.readdirSync(srcDir, { withFileTypes: true });
  for (const entry of entries) {
    const srcPath = path.join(srcDir, entry.name);
    const destName = entry.name.toLowerCase();
    const destPath = path.join(destDir, destName);

    if (entry.isDirectory()) {
      syncDirectory(srcPath, destPath);
    } else if (entry.isFile() && entry.name.endsWith('.md')) {
      const raw = fs.readFileSync(srcPath, 'utf8');
      const processed = processMarkdown(raw, entry.name);
      fs.writeFileSync(destPath, processed, 'utf8');
    }
  }
}

for (const section of SECTIONS) {
  const src = path.join(REPO_ROOT, section.src);
  const dest = path.join(SRC_CONTENT_DIR, section.dest);
  syncDirectory(src, dest);
}

// Generate landing page index.mdx if not present
const indexPath = path.join(SRC_CONTENT_DIR, 'index.mdx');
const landingPageContent = `---
title: xg2g Streaming Gateway
description: High-Performance Enigma2 Live TV & DVR Streaming Gateway
template: splash
hero:
  tagline: Next-generation ultra-low-latency streaming gateway for Enigma2 receivers. Modern web UI, hardware acceleration (VAAPI/NVENC/Apple VT), adaptive HLS, and zero-stall pipelines.
  image:
    file: ../../assets/logo.svg
  actions:
    - text: Quickstart Guide
      link: /xg2g/guides/getting_started/
      icon: right-arrow
      variant: primary
    - text: Interactive API Explorer
      link: /xg2g/api-reference/
      icon: external
      variant: secondary
    - text: GitHub Repository
      link: https://github.com/ManuGH/xg2g
      icon: github
      variant: minimal
---

import { Card, CardGrid } from '@astrojs/starlight/components';

## Core Architecture

<CardGrid stagger>
  <Card title="Ultra-Low Latency & Adaptive HLS" icon="rocket">
    Direct PES demuxing and fMP4 packaging tailored for Apple Safari, iOS, tvOS, and modern Web browsers with zero transcoder stall.
  </Card>
  <Card title="Hardware-Accelerated Transcoding" icon="setting">
    Automated codec matrix and runtime probing for Intel Quick Sync (VAAPI), NVIDIA NVENC, and Apple VideoToolbox.
  </Card>
  <Card title="Intelligent Tuner Orchestration" icon="laptop">
    Dynamic FBC tuner conflict resolution, priority-based reservation, and digital twin simulation for Enigma2 receivers.
  </Card>
  <Card title="Modern WebUI & PWA" icon="open-book">
    React 19, TypeScript, and modern dark petrol design with instant zapping, EPG bouquet navigation, and live telemetry.
  </Card>
</CardGrid>

## Quick Start (Docker Compose)

Deploy in seconds on any Linux host with Docker:

\`\`\`yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g
    restart: unless-stopped
    ports:
      - "8088:8088"
      - "9091:9091"
    environment:
      - XG2G_E2_HOST=http://enigma2.local
      - XG2G_DECISION_SECRET=super-secret-decision-signing-key-min-32-bytes
      - XG2G_STREAM_PASSTHROUGH=true
      - XG2G_METRICS_LISTEN=:9091
\`\`\`

## Diátaxis Documentation Hub

Explore comprehensive guides organized by the 4 operational quadrants:

- **[Tutorials (Learning-oriented)](/xg2g/tutorials/):** Step-by-step onboarding, end-to-end first stream setup.
- **[How-To Guides (Problem-oriented)](/xg2g/how-to/):** Installation recipes, hardware acceleration setup, troubleshooting.
- **[Reference (Information-oriented)](/xg2g/reference/):** Architecture details, codec matrix, config surfaces, and the OpenAPI contract.
- **[Explanation (Understanding-oriented)](/xg2g/explanation/):** Streaming topology, session lifecycle, and Architecture Decision Records (ADRs).
`;

fs.writeFileSync(indexPath, landingPageContent, 'utf8');

console.log('✅ Documentation sync complete: copied sections, extracted titles, updated public assets.');
