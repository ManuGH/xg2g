# Contributing to xg2g

Thanks for your interest in contributing to **xg2g**!
This project is in an early stage. Pull requests and issues are welcome.

## Development Setup

> ℹ️  Please review the branch protection rules in [`.github/BRANCH_POLICY.md`](.github/BRANCH_POLICY.md) before opening a pull request.

```bash
# Clone the repository
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# Download dependencies
go mod download

# Build
go build ./cmd/daemon

# Run locally
./xg2g
```

## Language Policy

**This project follows an English-only policy** for all communication and documentation:

- **Issues and Pull Requests**: Must be written in English (titles, descriptions, comments)
- **Code Comments**: English only
- **Documentation**: All `.md` files must be in English
- **Commit Messages**: English preferred

This ensures accessibility for the global open-source community and maintains consistency.

## Code Quality Standards

Before submitting a pull request, ensure your code passes all quality checks:

```bash
# Run tests
go test ./... -race

# Run linter
make lint

# Run build
go build ./cmd/daemon
```

## Submission Guidelines

1. **Issues**: Use clear, descriptive titles in English
2. **Pull Requests**: Include clear description of changes
3. **Testing**: Add tests for new functionality
4. **Documentation**: Update relevant documentation

## Working with the Rust GPU Transcoder

The xg2g project includes a high-performance GPU transcoder written in Rust located in the [`transcoder/`](../transcoder/) directory.

### Prerequisites

Before working on the transcoder, ensure you have:

- **Rust 1.84+**: Install via [rustup](https://rustup.rs/)
- **FFmpeg 7.0+**: With VAAPI support compiled
- **VAAPI Drivers**: AMD (`mesa-va-drivers`) or Intel (`intel-media-va-driver`)
- **GPU Access**: AMD or Intel GPU with `/dev/dri/renderD128` device

### Setting Up Local Development

```bash
# Navigate to transcoder directory
cd transcoder/

# Check Rust installation
rustc --version  # Should be 1.84+

# Install dependencies
cargo fetch

# Verify VAAPI is available
vainfo

# Should show your GPU and supported profiles
```

### Building the Transcoder

```bash
# Debug build (faster compilation, slower runtime)
cargo build

# Release build (optimized for performance)
cargo build --release

# The binary will be at:
# - Debug: target/debug/xg2g-transcoder
# - Release: target/release/xg2g-transcoder
```

### Running Tests

```bash
# Run all tests
cargo test

# Run tests with output
cargo test -- --nocapture

# Run specific test
cargo test test_vaapi_detection

# Run with debug logging
RUST_LOG=debug cargo test -- --nocapture
```

### Local Development Workflow

```bash
# 1. Set environment variables
export VAAPI_DEVICE=/dev/dri/renderD128
export VIDEO_BITRATE=5000k
export RUST_LOG=debug
export FFMPEG_PATH=ffmpeg

# 2. Run in development mode (with auto-reload)
cargo watch -x run

# 3. Or run directly
cargo run --release

# 4. Test the transcoder
# Health check
curl http://localhost:8081/health

# Transcode test stream
curl "http://localhost:8081/transcode?source_url=http://devimages.apple.com/iphone/samples/bipbop/bipbopall.m3u8" \
  | ffplay -
```

### Debugging VAAPI Issues

If you encounter VAAPI-related issues:

```bash
# Check device permissions
ls -la /dev/dri/

# Should show renderD128 with video group access:
# crw-rw----+ 1 root video ... renderD128

# Add your user to video and render groups
sudo usermod -a -G video $USER
sudo usermod -a -G render $USER

# Log out and back in for group changes to take effect

# Verify VAAPI device
vainfo --display drm --device /dev/dri/renderD128

# Check FFmpeg VAAPI support
ffmpeg -hwaccels
# Should list: vaapi

# Test VAAPI encoding
ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 \
  -f lavfi -i testsrc -t 5 -c:v h264_vaapi test.mp4
```

### Code Quality for Rust

Before submitting Rust changes:

```bash
# Format code
cargo fmt

# Check for issues
cargo clippy -- -D warnings

# Run tests
cargo test

# Check for unused dependencies
cargo udeps

# Security audit
cargo audit
```

### Docker Development

To test the transcoder in Docker:

```bash
cd transcoder/

# Build Docker image
docker build -t xg2g-transcoder:dev .

# Run with GPU access
docker run -d \
  --name transcoder-dev \
  --device /dev/dri:/dev/dri \
  --group-add video \
  -p 8081:8081 \
  -e RUST_LOG=debug \
  -e VAAPI_DEVICE=/dev/dri/renderD128 \
  xg2g-transcoder:dev

# View logs
docker logs -f transcoder-dev

# Test
curl http://localhost:8081/health
```

### Integration with Go Service

To test the transcoder with the main Go service:

```bash
# Terminal 1: Run transcoder
cd transcoder/
RUST_LOG=debug cargo run --release

# Terminal 2: Run Go service with GPU transcoding enabled
cd ..
export XG2G_GPU_TRANSCODE=true
export XG2G_GPU_TRANSCODER_URL=http://localhost:8081
go run ./cmd/daemon

# Terminal 3: Test streaming
curl "http://localhost:8080/stream/channel_id" | ffplay -
```

### Performance Profiling

To profile the transcoder:

```bash
# Install flamegraph
cargo install flamegraph

# Generate flamegraph (requires perf on Linux)
cargo flamegraph

# For detailed profiling
RUST_LOG=info cargo build --release
perf record --call-graph dwarf ./target/release/xg2g-transcoder
perf report
```

### Common Development Tasks

#### Adding a New Codec

1. Update `src/transcoder.rs` to add codec configuration
2. Modify FFmpeg command generation in `build_ffmpeg_command()`
3. Add tests in `tests/codec_tests.rs`
4. Update documentation in `README.md`
5. Test with real streams

#### Modifying Transcoding Parameters

1. Update environment variable parsing in `src/config.rs`
2. Modify FFmpeg arguments in `src/transcoder.rs`
3. Add validation in `src/validator.rs`
4. Update tests
5. Document changes in README

#### Adding Metrics

1. Add new metric in `src/metrics.rs`
2. Increment/update in relevant code paths
3. Document in API section of README
4. Test with `/metrics` endpoint

### Troubleshooting

**Cargo build fails with linking errors:**
```bash
# Install system dependencies (Debian/Ubuntu)
sudo apt install build-essential pkg-config libssl-dev
```

**VAAPI not detected in tests:**
```bash
# Tests run in isolated environment
# Use `cargo test -- --test-threads=1` for sequential execution
# Check that /dev/dri/renderD128 exists and is accessible
```

**FFmpeg process hangs:**
```bash
# Enable debug logging
RUST_LOG=debug cargo run

# Check FFmpeg logs in application output
# Verify source stream is accessible
curl -I <source_url>
```

### Resources

- [Rust GPU Transcoder README](../transcoder/README.md)
- [GPU Transcoding Documentation](GPU_TRANSCODING.md)
- [FFmpeg VAAPI Guide](https://trac.ffmpeg.org/wiki/Hardware/VAAPI)
- [Rust Book](https://doc.rust-lang.org/book/)
- [Tokio Async Runtime](https://tokio.rs/)
- [Axum Web Framework](https://docs.rs/axum/latest/axum/)
