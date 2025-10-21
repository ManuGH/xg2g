# CPU-Specific Build Optimizations

This document explains how to build xg2g with CPU-specific optimizations for maximum performance.

## Quick Start

### Default Build (Compatible with most CPUs)

```bash
# Works on Intel Haswell+ (2013+) and AMD Zen+ (2018+)
docker compose build
```

**Default optimizations:**
- Rust: `x86-64-v3` (AVX2, FMA, BMI2)
- Go: `GOAMD64=v2` (POPCNT, SSE4.2)

### Optimized Build for Your Specific CPU

Use the build script to automatically detect and optimize for your CPU:

```bash
./build-optimized.sh
```

Or manually specify your CPU architecture:

```bash
# For AMD Zen 4 (Ryzen 7000/8000 series)
docker build --build-arg RUST_TARGET_CPU=znver4 \
             --build-arg GO_AMD64_LEVEL=v3 \
             -t xg2g-gpu-transcoder:production ./transcoder

docker build --build-arg GO_AMD64_LEVEL=v3 \
             -t xg2g:latest .
```

## CPU Architecture Support

### Rust `target-cpu` Options

| CPU Architecture | Target | Features | Example CPUs |
|-----------------|--------|----------|--------------|
| **Generic x86-64** | `x86-64` | SSE2 | All 64-bit CPUs (default fallback) |
| **Modern Generic** | `x86-64-v3` | AVX2, FMA, BMI2 | Intel Haswell+, AMD Zen+ ✅ **Default** |
| **Intel Haswell** | `haswell` | AVX2, FMA, BMI2 | Intel 4th gen (2013+) |
| **Intel Skylake** | `skylake` | AVX2, FMA, BMI2, AES | Intel 6th gen (2015+) |
| **Intel Cascade Lake** | `cascadelake` | AVX512 | Intel Xeon (2019+) |
| **AMD Zen** | `znver1` | AVX2, FMA, BMI2 | Ryzen 1000 series |
| **AMD Zen 2** | `znver2` | AVX2, FMA, BMI2 | Ryzen 3000 series |
| **AMD Zen 3** | `znver3` | AVX2, FMA, BMI2, VAES | Ryzen 5000 series |
| **AMD Zen 4** | `znver4` | AVX512, VAES, VPCLMULQDQ | Ryzen 7000/8000 series |

### Go `GOAMD64` Levels

| Level | Features | Minimum CPU | Notes |
|-------|----------|-------------|-------|
| **v1** | SSE2 | All 64-bit x86 | Go default |
| **v2** | POPCNT, SSE4.2, CX16, SSSE3 | Intel Nehalem (2008+), AMD Bulldozer (2011+) | ✅ **xg2g default** |
| **v3** | AVX, AVX2, BMI1, BMI2, FMA | Intel Haswell (2013+), AMD Zen (2017+) | Recommended for modern CPUs |
| **v4** | AVX512 | Intel Skylake-X (2017+), AMD Zen 4 (2022+) | Cutting edge |

## Build Examples

### AMD Ryzen 7000/8000 Series (Zen 4)

```bash
# Transcoder
docker build \
  --build-arg RUST_TARGET_CPU=znver4 \
  --build-arg RUST_OPT_LEVEL=3 \
  -t xg2g-gpu-transcoder:production \
  ./transcoder

# Go service
docker build \
  --build-arg GO_AMD64_LEVEL=v3 \
  -t xg2g:latest .
```

**Performance gain:** 15-30% faster CPU operations (deinterlacing, audio processing)

### AMD Ryzen 5000 Series (Zen 3)

```bash
docker build --build-arg RUST_TARGET_CPU=znver3 -t xg2g-gpu-transcoder:production ./transcoder
docker build --build-arg GO_AMD64_LEVEL=v3 -t xg2g:latest .
```

### Intel 6th Gen+ (Skylake, Kaby Lake, Coffee Lake, etc.)

```bash
docker build --build-arg RUST_TARGET_CPU=skylake -t xg2g-gpu-transcoder:production ./transcoder
docker build --build-arg GO_AMD64_LEVEL=v3 -t xg2g:latest .
```

### Intel Xeon with AVX-512

```bash
docker build --build-arg RUST_TARGET_CPU=cascadelake -t xg2g-gpu-transcoder:production ./transcoder
docker build --build-arg GO_AMD64_LEVEL=v4 -t xg2g:latest .
```

### Maximum Compatibility (Older CPUs)

```bash
docker build --build-arg RUST_TARGET_CPU=x86-64 -t xg2g-gpu-transcoder:production ./transcoder
docker build --build-arg GO_AMD64_LEVEL=v1 -t xg2g:latest .
```

## Detecting Your CPU

```bash
# On Linux
lscpu | grep "Model name"
cat /proc/cpuinfo | grep flags | head -1

# Check for AVX2 support
grep -o 'avx2' /proc/cpuinfo | head -1

# Check for AVX-512 support
grep -o 'avx512' /proc/cpuinfo | head -1
```

## Performance Considerations

### What gets optimized?

**CPU-bound operations:**
- ✅ Deinterlacing (yadif filter)
- ✅ Audio resampling and encoding
- ✅ HTTP request parsing
- ✅ JSON encoding/decoding
- ✅ Stream multiplexing

**GPU-bound operations (NOT affected):**
- Hardware video decoding (VAAPI)
- Hardware video encoding (h264_vaapi, hevc_vaapi)

### When should you optimize?

- **High concurrent streams** (>5 simultaneous users)
- **High-resolution content** (1080p+)
- **Heavy deinterlacing** (50i/60i sources)
- **Multiple audio tracks** being transcoded

### When can you skip it?

- Low user count (<3 users)
- Primarily direct play (no transcoding)
- Pre-deinterlaced content

## Troubleshooting

### "Illegal instruction" error

Your CPU doesn't support the compiled instructions. Rebuild with a more compatible target:

```bash
# Try x86-64-v3 first
docker build --build-arg RUST_TARGET_CPU=x86-64-v3 -t xg2g-gpu-transcoder:production ./transcoder

# If that fails, use x86-64
docker build --build-arg RUST_TARGET_CPU=x86-64 -t xg2g-gpu-transcoder:production ./transcoder
```

### Verify binary CPU requirements

```bash
# Check what CPU features the binary needs
docker run --rm xg2g-gpu-transcoder:production /bin/bash -c "readelf -p .comment /app/xg2g-transcoder"
```

## CI/CD and Distribution

For GitHub Actions or public Docker images, use the **default build** without custom arguments. This ensures compatibility across different systems:

```yaml
# GitHub Actions example
- name: Build Docker images
  run: |
    docker build -t ghcr.io/user/xg2g-transcoder:latest ./transcoder
    docker build -t ghcr.io/user/xg2g:latest .
```

Users can then rebuild locally with optimizations if needed.

## References

- [Rust target-cpu options](https://doc.rust-lang.org/rustc/codegen-options/index.html#target-cpu)
- [Go AMD64 microarchitecture levels](https://github.com/golang/go/wiki/MinimumRequirements#amd64)
- [x86-64 microarchitecture levels](https://en.wikipedia.org/wiki/X86-64#Microarchitecture_levels)
