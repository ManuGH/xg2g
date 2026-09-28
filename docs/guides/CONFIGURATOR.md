# Deployment Configurator & Codec Calculator

Generate production-ready deployment configurations and evaluate hardware-accelerated transcoding decisions in real time based on your target client devices and incoming Enigma2 broadcast streams.

---

## 1. Interactive Deployment Configurator

Select your target infrastructure and hardware acceleration capabilities below to generate your tailored runtime deployment configuration:

<div style="background: rgba(15, 23, 42, 0.6); border: 1px solid rgba(56, 189, 248, 0.25); border-radius: 12px; padding: 20px; margin: 20px 0;">
  <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; margin-bottom: 16px;">
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Deployment Target</label>
      <select id="cfg-target" onchange="renderDeployConfig()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;">
        <option value="compose">Docker Compose (Recommended)</option>
        <option value="docker">Docker CLI (One-Liner)</option>
        <option value="systemd">Linux systemd Service</option>
      </select>
    </div>
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Hardware Acceleration</label>
      <select id="cfg-accel" onchange="renderDeployConfig()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;">
        <option value="vaapi">Intel / AMD VAAPI (/dev/dri)</option>
        <option value="nvenc">NVIDIA NVENC (GPU Reservation)</option>
        <option value="cpu">CPU Software (Transcode Fallback)</option>
      </select>
    </div>
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Enigma2 Host URL</label>
      <input type="text" id="cfg-e2host" value="http://receiver.local" oninput="renderDeployConfig()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;" />
    </div>
  </div>

  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
    <span id="cfg-output-label" style="font-size: 0.8rem; font-weight: 700; color: #38bdf8; text-transform: uppercase; letter-spacing: 0.05em;">compose.yaml</span>
    <button onclick="copyDeployConfig()" id="cfg-copy-btn" style="padding: 4px 12px; border-radius: 6px; background: #0284c7; color: white; border: none; font-size: 0.8rem; font-weight: 600; cursor: pointer;">Copy Config</button>
  </div>
  <pre style="background: #070b10; border: 1px solid #1e293b; border-radius: 8px; padding: 14px; overflow-x: auto; margin: 0;"><code id="cfg-output-code" style="color: #38bdf8; font-family: monospace; font-size: 0.85rem;"></code></pre>
</div>

---

## 2. Interactive Codec Decision Engine Calculator

Test how the dynamic decision engine arbitrates incoming transport streams against target playback devices to avoid unnecessary transcode overhead:

<div style="background: rgba(15, 23, 42, 0.6); border: 1px solid rgba(0, 229, 255, 0.25); border-radius: 12px; padding: 20px; margin: 20px 0;">
  <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 16px;">
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Broadcast Video</label>
      <select id="calc-video" onchange="runDecisionCalc()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;">
        <option value="h264">H.264 (720p / 1080i HD)</option>
        <option value="hevc">HEVC / H.265 (4K UHD)</option>
        <option value="mpeg2">MPEG-2 (Legacy SD)</option>
      </select>
    </div>
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Broadcast Audio</label>
      <select id="calc-audio" onchange="runDecisionCalc()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;">
        <option value="ac3">AC-3 (Dolby Digital 5.1)</option>
        <option value="eac3">E-AC-3 (Dolby Digital Plus)</option>
        <option value="aac">AAC Stereo</option>
        <option value="mp2">MP2 (MPEG-1 Layer II)</option>
      </select>
    </div>
    <div>
      <label style="display: block; font-size: 0.85rem; font-weight: 600; color: #94a3b8; margin-bottom: 6px;">Client Player</label>
      <select id="calc-client" onchange="runDecisionCalc()" style="width: 100%; padding: 8px 12px; border-radius: 6px; background: #0b1116; border: 1px solid #334155; color: #f8fafc; font-size: 0.9rem;">
        <option value="safari">Apple Safari (iOS / macOS)</option>
        <option value="chrome">Google Chrome / Chromium</option>
        <option value="android_tv">Android TV / FireTV</option>
        <option value="apple_tv">Apple TV (tvOS Native)</option>
      </select>
    </div>
  </div>

  <div id="calc-result-box" style="padding: 16px; border-radius: 8px; border: 1px solid #00e5ff; background: rgba(0, 229, 255, 0.1); color: #e0f2fe;">
    <div style="font-size: 1.1rem; font-weight: 700; margin-bottom: 4px;" id="calc-action-title">Direct Stream Copy (fMP4 Passthrough)</div>
    <div style="font-size: 0.85rem; color: #94a3b8;" id="calc-action-details">Video: Copy (0% CPU) • Audio: Pass-through • Target Container: fMP4</div>
  </div>
</div>

<script>
function renderDeployConfig() {
  const target = document.getElementById('cfg-target').value;
  const accel = document.getElementById('cfg-accel').value;
  const host = document.getElementById('cfg-e2host').value.trim() || 'http://receiver.local';
  const label = document.getElementById('cfg-output-label');
  const code = document.getElementById('cfg-output-code');
  if (!code || !label) return;

  if (target === 'compose') {
    label.innerText = 'docker-compose.yml';
    let devBlock = '';
    if (accel === 'vaapi') {
      devBlock = '    devices:\n      - /dev/dri:/dev/dri # Intel/AMD VAAPI acceleration\n';
    } else if (accel === 'nvenc') {
      devBlock = '    deploy:\n      resources:\n        reservations:\n          devices:\n            - driver: nvidia\n              count: all\n              capabilities: [gpu, video]\n';
    }
    code.innerText = `services:
  xg2g:
    image: ghcr.io/manugh/xg2g:v3.12.0
    container_name: xg2g
    restart: unless-stopped
    ports:
      - "8088:8088"
      - "9091:9091"
    environment:
      - XG2G_E2_HOST=\${XG2G_E2_HOST:-${host}}
      - XG2G_DECISION_SECRET=\${XG2G_DECISION_SECRET:-generate-secure-min-32-byte-hex-key}
      - XG2G_STREAM_PASSTHROUGH=true
      - XG2G_METRICS_LISTEN=:9091
${devBlock}`;
  } else if (target === 'docker') {
    label.innerText = 'Terminal Command (docker run)';
    let devArg = '';
    if (accel === 'vaapi') devArg = '  --device /dev/dri:/dev/dri \\\n';
    if (accel === 'nvenc') devArg = '  --gpus all \\\n';
    code.innerText = `docker run -d --name xg2g --restart unless-stopped \\
  -p 8088:8088 -p 9091:9091 \\
${devArg}  -e XG2G_E2_HOST="${host}" \\
  -e XG2G_DECISION_SECRET="$(openssl rand -hex 32)" \\
  -e XG2G_STREAM_PASSTHROUGH=true \\
  -e XG2G_METRICS_LISTEN=":9091" \\
  ghcr.io/manugh/xg2g:v3.12.0`;
  } else {
    label.innerText = 'Linux systemd Service';
    code.innerText = `# 1. Clone repository
git clone https://github.com/ManuGH/xg2g.git && cd xg2g

# 2. Run automated systemd installer
sudo ./infra/systemd/setup-linux.sh

# 3. Verify daemon health
curl -fsS http://localhost:8088/readyz`;
  }
}

function copyDeployConfig() {
  const code = document.getElementById('cfg-output-code')?.innerText;
  if (!code) return;
  navigator.clipboard.writeText(code).then(() => {
    const btn = document.getElementById('cfg-copy-btn');
    if (!btn) return;
    const orig = btn.innerText;
    btn.innerText = 'Copied!';
    setTimeout(() => { btn.innerText = orig; }, 2000);
  });
}

function runDecisionCalc() {
  const v = document.getElementById('calc-video').value;
  const a = document.getElementById('calc-audio').value;
  const c = document.getElementById('calc-client').value;
  const title = document.getElementById('calc-action-title');
  const details = document.getElementById('calc-action-details');
  const box = document.getElementById('calc-result-box');
  if (!title || !details || !box) return;

  let action = 'Direct Stream Copy';
  let isTranscode = false;
  let videoNote = 'Video Copy (0% CPU)';
  let audioNote = 'Native Pass-Through';
  let container = 'fMP4';

  if (v === 'mpeg2') {
    action = 'Hardware Transcode (MPEG-2 → H.264)';
    videoNote = 'VAAPI/NVENC Transcode';
    isTranscode = true;
  } else if (v === 'hevc') {
    if (c === 'safari' || c === 'apple_tv') {
      action = 'Direct Stream (HEVC Native Copy)';
      videoNote = 'HEVC Copy (0% CPU, Apple Silicon)';
    } else {
      action = 'Hardware Transcode (HEVC → H.264)';
      videoNote = 'Transcode Fallback for Client';
      isTranscode = true;
    }
  }

  if (a === 'mp2') {
    audioNote = 'Audio Transcode (MP2 → AAC)';
    isTranscode = true;
  } else if (a === 'ac3' || a === 'eac3') {
    if (c === 'safari' || c === 'android_tv' || c === 'apple_tv') {
      audioNote = 'Dolby Bitstream Pass-Through';
    } else {
      audioNote = 'Audio Remux (AC3 → AAC)';
      isTranscode = true;
    }
  }

  title.innerText = action;
  details.innerText = `${videoNote} • ${audioNote} • Target: ${container}`;
  if (isTranscode) {
    box.style.borderColor = '#f59e0b';
    box.style.background = 'rgba(245, 158, 11, 0.12)';
    title.style.color = '#fbbf24';
  } else {
    box.style.borderColor = '#00e5ff';
    box.style.background = 'rgba(0, 229, 255, 0.1)';
    title.style.color = '#38bdf8';
  }
}

// Initial calculation on render
if (typeof document !== 'undefined') {
  renderDeployConfig();
  runDecisionCalc();
}
</script>

---

## Technical Invariants

1. **Deterministic Transcoding Policy**: Incoming streams are only transcoded when the client player lacks hardware support for the underlying codec.
2. **Zero-Stall Buffer Guarantee**: All streaming sessions enforce heartbeat leases to avoid leaving orphaned Enigma2 tuners locked.
3. **SSoT Contract**: To inspect codec boundaries in detail, reference [CODEC_MATRIX.md](../arch/CODEC_MATRIX.md).
