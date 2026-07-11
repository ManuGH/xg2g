# Performance & Quality Tuning

xg2g's live-transcode defaults are chosen to be **safe for any deployment** shipped
via GitHub (unknown relays, hosts, bandwidth). A few env vars let you trade startup
latency / bitrate / robustness for your specific setup.

## Sharpened defaults (no config needed)

These ship on by default now — they are a bit more aggressive than the old
ultra-conservative values, but still safe for typical broadcast sources:

| What | Old | Now | Effect |
|---|---|---|---|
| Stream-relay transcode probe | 10s | **5s** | ~halves channel start time; detection stays correct on normal broadcast TS |
| AV1 quality target (QVBR) | 110 | **90** | spends more of the available bitrate on cleaner motion (still bounded by the maxrate ceiling) |
| 50p motion (interlaced) | — | **auto** | enabled when the host benchmark can sustain it; weak hosts stay 25p (no overload) |

The 50p host-gate and the configurable startup probe are **code** (apply
automatically once you update the image). The values below are **deployment env
tuning** — set them per install.

Transcode enhancement defaults are source-aware when scan truth includes the
source height:

| Source | Denoise | Sharpen | Deband | Rationale |
|---|---:|---:|---:|---|
| unknown | `0.6` | `1.5` | on | preserve the established fail-safe behavior |
| SD (up to 576) | `0.6` | `1.0` | on | clean broadcast noise before upscaling |
| 720p | `0.3` | `0.75` | off | mild cleanup without flattening HD texture |
| 1080p and above | off | `0.5` | off | preserve motion detail; 10-bit AV1 protects new gradients |

Explicit `XG2G_TRANSCODE_DENOISE`, `XG2G_TRANSCODE_SHARPEN`, and
`XG2G_TRANSCODE_DEBAND` values always override this table. VAAPI AV1 hosts also
run a one-second 1080i25-to-1080p50 production-chain preflight; automatic AV1
selection and HQ50 promotion use that result instead of trusting the short
encoder-only probe.

## Optional tuning

| Env | Default | Lower → | Raise → |
|---|---|---|---|
| `XG2G_STREAMRELAY_ANALYZE_DURATION` | `5000000` (5s) | faster start (e.g. `3000000` for fast relays) | safer detection for slow/bursty relays |
| `XG2G_AV1_QVBR_QUALITY` | `90` (scale 1–255) | higher quality, more bitrate (e.g. `25`) | lower bitrate |
| `XG2G_SAFARI_HQ50_MAXRATE_K` | `12000–14000` (resolution-scaled) | — | higher quality ceiling for demanding channels (needs the bandwidth) |
| `XG2G_SAFARI_HQ50_BUFSIZE_K` | `2× maxrate` | — | smoother / more consistent bit allocation (e.g. `3× maxrate`) |

## Recommended profiles

**Home LAN / WireGuard, bandwidth not a constraint — maximum quality**
(verified: AV1 1080p50 10-bit, ~12 Mbit/s avg, smooth):
```
XG2G_STREAMRELAY_ANALYZE_DURATION=3000000
XG2G_AV1_QVBR_QUALITY=25
XG2G_SAFARI_HQ50_BUFSIZE_K=44000
```

**Bandwidth-constrained / remote over a slow link — keep it lean:**
```
# leave the defaults; optionally raise QVBR to spend fewer bits
XG2G_AV1_QVBR_QUALITY=110
```

## Honest trade-offs

- **Lower QVBR / higher maxrate = more bitrate** — free on a LAN, but can rebuffer
  over a slow remote link. The maxrate ceiling bounds the peak either way.
- **Shorter analyze duration = faster start, less margin** — `3000000` was verified
  clean on a 4K relay, but a slow/bursty relay may need the `5000000` default (or
  more) to resolve dimensions/audio reliably.
- **There is no ABR ladder yet**: a single rendition is served, so a congested
  network rebuffers instead of downshifting. Keep the bitrate within the link's
  sustained throughput.
