# Docker Image Tags & Optimization

xg2g provides multiple image tags for different use cases and CPU architectures.

### Universal Image (Recommended)

The `latest` tag is a **Universal Image** that works on almost all hardware.

- **Multi-Arch**: Automatically pulls the correct version for your CPU (AMD64 or ARM64).
- **Smart Optimization**: On AMD64, it uses the `v2` optimization level (SSE4.2), which balances high performance with broad compatibility (works on CPUs from 2009+).
- **Zero Config**: Just use `ghcr.io/manugh/xg2g:latest` and it works.

| Tag | Description | Use Case | Updated |
|-----|-------------|----------|---------|
| `latest` | **Universal** (AMD64/ARM64) | **Production** | On version tags (`v*`) |
| `main` | Latest development | **Staging/Testing** | Every push to main |
| `v1.2.3` | Specific version | **Pinned deployments** | On version tags |

### Power User Tags (AMD64 only)

For users who want to squeeze every last drop of performance out of their hardware, we offer specialized CPU-optimized tags. **Most users do not need these.**

| Tag | CPU Level | Min CPU Year | Target CPUs | Performance | Compatibility |
|-----|-----------|--------------|-------------|-------------|---------------|
| `v1-compat` | x86-64-v1 | 2003+ | Any AMD64 CPU | Baseline | ✅ Maximum |
| `v3-performance` | x86-64-v3 | 2015+ | Haswell, Zen+ (AVX2) | Excellent | ⚠️ Modern only |

**CPU Level Details:**

- **v1** (x86-64): SSE2 only - runs on any 64-bit CPU (Pentium 4+, Athlon 64+)
- **v2** (x86-64-v2): +SSE3, SSE4.1, SSE4.2, POPCNT - **default**, best balance
- **v3** (x86-64-v3): +AVX, AVX2, BMI1/2, FMA - 10-20% faster for audio/video

## Architecture-Specific Tags

| Tag | Architecture | Description | Availability |
|-----|--------------|-------------|--------------|
| `main-arm64` | ARM64 | Latest dev for ARM | ❌ Releases only |
| `v1.2.3-arm64` | ARM64 | Version for ARM | ✅ On releases |
| `sha-abc123-amd64-v2` | AMD64 | Specific commit + CPU level | ✅ Every push |

**⚠️ ARM64 Build Strategy:**

- **main branch**: AMD64 only (fast CI, ~2-3 min)
- **Release tags** (`v*`): AMD64 + ARM64 (slower, ~60-90 min via QEMU)
- **Nightly canary**: ARM64 cross-compile test (no push, validates builds)
- **Reason**: ARM64 emulation via QEMU is 20-30x slower than native AMD64

## Choosing the Right Image

**How to check your CPU level:**

```bash
# On Linux
grep -o 'avx2\|avx\|sse4_2' /proc/cpuinfo | sort -u

# Result interpretation:
# - avx2 present → Use :v3-performance
# - sse4_2 present (no avx2) → Use :latest (v2)
# - neither → Use :v1-compat
```

**Recommendation by hardware:**

- 🖥️ **Modern server** (2015+): `v3-performance` - Best performance
- 🏠 **Home server/NAS** (2010+): `latest` - Balanced (default)
- 📦 **Old hardware** (<2010): `v1-compat` - Maximum compatibility
- 🍇 **Raspberry Pi / ARM**: `latest` - Auto-selects ARM64

## Toolchain Versions

**Current (2025):**

- Go: 1.25
- Rust: 1.84
- Alpine: 3.22.2
- FFmpeg: 7.x (Alpine package)

**Pinning Strategy:**

- Docker base images: Pinned to minor version
- Go/Rust toolchains: Pinned to patch version for reproducibility
- Cross-compilation: cargo-zigbuild 0.19.7
