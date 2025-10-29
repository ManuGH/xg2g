# FFI Integration Testing Guide

This guide helps you validate the Go + Rust FFI integration on your system.

## Prerequisites Check

### 1. Verify Rust Installation

```bash
# Check Rust version (needs 1.70+)
rustc --version
cargo --version

# If not installed:
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source $HOME/.cargo/env
```

### 2. Verify Go Installation

```bash
# Check Go version (needs 1.21+)
go version

# Verify CGO is enabled
go env CGO_ENABLED  # should output: 1
```

### 3. Verify C Compiler

```bash
# macOS
xcode-select --version

# If not installed:
xcode-select --install

# Linux
gcc --version
```

## Step-by-Step Testing

### Test 1: Build Rust Library

```bash
cd /Users/manuel/xg2g

# Build Rust transcoder library
make build-rust

# Verify library was built
ls -lh transcoder/target/release/libxg2g_transcoder.*
```

**Expected output:**
```
-rw-r--r--  libxg2g_transcoder.a      # Static library (macOS/Linux)
-rw-r--r--  libxg2g_transcoder.dylib  # Dynamic library (macOS)
-rw-r--r--  libxg2g_transcoder.so     # Dynamic library (Linux)
```

**Troubleshooting:**
- If cargo command not found: Restart terminal after Rust install
- If build fails: Check `transcoder/Cargo.toml` syntax
- If linker errors: Install system dependencies (see FFI_INTEGRATION.md)

### Test 2: Run Rust Unit Tests

```bash
cd transcoder

# Run Rust tests
cargo test

# Expected: All tests pass
```

**Expected output:**
```
running 4 tests
test ffi::tests::test_remuxer_init_and_free ... ok
test ffi::tests::test_remuxer_process ... ok
test ffi::tests::test_version ... ok
test ffi::tests::test_null_handle ... ok

test result: ok. 4 passed; 0 failed; 0 ignored; 0 measured
```

### Test 3: Run Go FFI Integration Tests

```bash
cd /Users/manuel/xg2g

# Run Go tests with FFI (requires Rust library)
make test-ffi
```

**Expected output:**
```
Running FFI integration tests...
Building Rust library first...
✅ Rust library built: transcoder/target/release/libxg2g_transcoder.a
Running Go tests with CGO enabled...
=== RUN   TestNewRustAudioRemuxer
--- PASS: TestNewRustAudioRemuxer (0.00s)
=== RUN   TestRustAudioRemuxer_Process
--- PASS: TestRustAudioRemuxer_Process (0.00s)
=== RUN   TestRustAudioRemuxer_ProcessEmptyInput
--- PASS: TestRustAudioRemuxer_ProcessEmptyInput (0.00s)
=== RUN   TestRustAudioRemuxer_ProcessAfterClose
--- PASS: TestRustAudioRemuxer_ProcessAfterClose (0.00s)
=== RUN   TestRustAudioRemuxer_MultipleClose
--- PASS: TestRustAudioRemuxer_MultipleClose (0.00s)
=== RUN   TestRustAudioRemuxer_ConcurrentAccess
--- PASS: TestRustAudioRemuxer_ConcurrentAccess (0.01s)
=== RUN   TestRustAudioRemuxer_Finalizer
--- PASS: TestRustAudioRemuxer_Finalizer (0.10s)
=== RUN   TestVersion
--- PASS: TestVersion (0.00s)
    rust_test.go:169: Rust transcoder version: 1.0.0
=== RUN   TestLastError
--- PASS: TestLastError (0.00s)
PASS
ok      github.com/ManuLpz4/xg2g/internal/transcoder    0.123s
✅ FFI integration tests passed
```

**Troubleshooting:**
- If CGO_ENABLED=0: Set `export CGO_ENABLED=1`
- If library not found: Run `make build-rust` first
- If undefined symbols: Check LDFLAGS in rust.go match library path

### Test 4: Run Benchmarks

```bash
# Run performance benchmarks
CGO_ENABLED=1 go test -v -bench=. -benchtime=5s ./internal/transcoder
```

**Expected output:**
```
BenchmarkRustAudioRemuxer_Process-8             300000    0.025 ms/op    7680 MB/s
BenchmarkRustAudioRemuxer_ProcessSmall-8       1000000    0.001 ms/op     1880 MB/s
```

**What to check:**
- Latency should be <1ms per call
- No memory leaks (run with `-benchmem`)
- Stable performance across iterations

### Test 5: Build Single Binary with FFI

```bash
cd /Users/manuel/xg2g

# Build complete binary with embedded Rust
make build-ffi

# Verify binary was created
ls -lh bin/daemon-ffi

# Check binary size (should be ~15-20MB)
du -h bin/daemon-ffi
```

**Expected output:**
```
-rwxr-xr-x  bin/daemon-ffi  18M
✅ FFI build complete: bin/daemon-ffi
   This binary includes the Rust audio remuxer!
```

**Troubleshooting:**
- If build fails: Check CGO linker flags in Makefile
- If binary too large: Normal - includes Rust library
- If undefined symbols: Rebuild Rust library with `make clean-rust build-rust`

### Test 6: Smoke Test the Binary

```bash
# Run the binary to verify it starts
./bin/daemon-ffi --version

# Check if FFI functions are accessible
nm bin/daemon-ffi | grep xg2g_audio_remux
```

**Expected output:**
```
xg2g version: <version>

# nm output should show:
xg2g_audio_remux_init
xg2g_audio_remux_process
xg2g_audio_remux_free
xg2g_transcoder_version
```

## Validation Checklist

Use this checklist to verify your FFI integration:

- [ ] ✅ Rust toolchain installed (rustc, cargo)
- [ ] ✅ Go CGO enabled (`go env CGO_ENABLED` = 1)
- [ ] ✅ C compiler available (xcode-select or gcc)
- [ ] ✅ Rust library builds (`make build-rust`)
- [ ] ✅ Rust unit tests pass (`cargo test`)
- [ ] ✅ Go FFI tests pass (`make test-ffi`)
- [ ] ✅ Benchmarks run successfully
- [ ] ✅ Single binary builds (`make build-ffi`)
- [ ] ✅ Binary includes FFI symbols (`nm` check)
- [ ] ✅ No memory leaks (finalizer test passes)
- [ ] ✅ Concurrent access works (10+ goroutines)

## Common Issues and Solutions

### Issue: "cargo: command not found"

**Solution:**
```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Restart terminal or:
source $HOME/.cargo/env

# Verify
cargo --version
```

### Issue: "CGO_ENABLED=0" or "cgo: C compiler not available"

**Solution:**
```bash
# macOS: Install Xcode Command Line Tools
xcode-select --install

# Enable CGO
export CGO_ENABLED=1

# Verify
go env CGO_ENABLED
```

### Issue: "cannot find -lxg2g_transcoder"

**Solution:**
```bash
# Clean and rebuild Rust library
make clean-rust
make build-rust

# Verify library exists
ls transcoder/target/release/libxg2g_transcoder.a
```

### Issue: "undefined reference to `xg2g_audio_remux_init`"

**Solution:**
```bash
# Check Cargo.toml has correct crate-type
cat transcoder/Cargo.toml | grep crate-type
# Should show: crate-type = ["cdylib", "rlib", "bin"]

# Rebuild with clean state
cd transcoder
cargo clean
cargo build --release
cd ..
```

### Issue: Tests pass but binary doesn't start

**Solution:**
```bash
# Check for missing dynamic libraries
otool -L bin/daemon-ffi  # macOS
ldd bin/daemon-ffi       # Linux

# If libraries missing, use static linking:
cd transcoder
cargo build --release --target-feature=+crt-static
```

### Issue: "panic: runtime error" when calling FFI

**Solution:**
- Check Rust side has `catch_unwind()` (already implemented)
- Verify null pointer checks (already implemented)
- Check for buffer size issues (input/output capacity)
- Enable Rust panic backtrace:
  ```bash
  RUST_BACKTRACE=1 go test -v ./internal/transcoder
  ```

## Performance Validation

After tests pass, validate performance characteristics:

### Memory Usage

```bash
# Run with memory profiling
go test -v -memprofile=mem.prof ./internal/transcoder

# Analyze memory profile
go tool pprof mem.prof
# Commands: top, list, web
```

**Expected:**
- No memory leaks
- ~30MB baseline usage
- Stable memory across iterations

### CPU Usage

```bash
# Run with CPU profiling
go test -v -cpuprofile=cpu.prof -bench=. ./internal/transcoder

# Analyze CPU profile
go tool pprof cpu.prof
```

**Expected:**
- <5% CPU for passthrough (current implementation)
- Will increase in Phase 4 with real remuxing

### Latency

```bash
# Run detailed benchmarks
go test -v -bench=BenchmarkRustAudioRemuxer_Process -benchtime=10s -benchmem ./internal/transcoder
```

**Expected:**
- <1ms per Process() call
- Consistent latency (no spikes)
- Low allocation overhead

## Integration Smoke Test

Create a simple test program to verify end-to-end:

```bash
# Create test program
cat > /tmp/ffi_test.go <<'EOF'
package main

import (
    "fmt"
    "log"
    "github.com/ManuLpz4/xg2g/internal/transcoder"
)

func main() {
    fmt.Println("=== FFI Integration Smoke Test ===")

    // Test 1: Version
    version := transcoder.Version()
    fmt.Printf("✓ Rust transcoder version: %s\n", version)

    // Test 2: Create remuxer
    remuxer, err := transcoder.NewRustAudioRemuxer(48000, 2, 192000)
    if err != nil {
        log.Fatalf("✗ Failed to create remuxer: %v", err)
    }
    fmt.Println("✓ Remuxer created successfully")
    defer remuxer.Close()

    // Test 3: Get config
    sr, ch, br := remuxer.Config()
    fmt.Printf("✓ Config: %dHz, %dch, %dbps\n", sr, ch, br)

    // Test 4: Process data
    input := make([]byte, 188*10) // 10 TS packets
    for i := 0; i < 10; i++ {
        input[i*188] = 0x47 // MPEG-TS sync byte
    }
    output, err := remuxer.Process(input)
    if err != nil {
        log.Fatalf("✗ Failed to process: %v", err)
    }
    fmt.Printf("✓ Processed %d bytes → %d bytes\n", len(input), len(output))

    fmt.Println("\n=== All FFI Tests Passed ✓ ===")
}
EOF

# Build and run
cd /Users/manuel/xg2g
CGO_ENABLED=1 go run /tmp/ffi_test.go
```

**Expected output:**
```
=== FFI Integration Smoke Test ===
✓ Rust transcoder version: 1.0.0
✓ Remuxer created successfully
✓ Config: 48000Hz, 2ch, 192000bps
✓ Processed 1880 bytes → 1880 bytes

=== All FFI Tests Passed ✓ ===
```

## Next Steps

Once all tests pass:

1. **Document Results:**
   - Note any platform-specific issues
   - Record actual performance numbers
   - Save benchmark results

2. **Report Status:**
   ```bash
   # Create status report
   cat > /tmp/ffi_status.txt <<EOF
   FFI Integration Test Results
   ============================
   Date: $(date)
   System: $(uname -s) $(uname -m)
   Go: $(go version)
   Rust: $(rustc --version)

   Rust Build: ✓ PASS
   Rust Tests: ✓ PASS
   Go FFI Tests: ✓ PASS
   Benchmarks: ✓ PASS
   Binary Build: ✓ PASS
   Smoke Test: ✓ PASS

   Ready for Phase 4: Native Audio Remuxing Implementation
   EOF

   cat /tmp/ffi_status.txt
   ```

3. **Proceed to Phase 4:**
   - MPEG-TS Parser Integration
   - Audio Decoder Setup (Symphonia)
   - AAC Encoder Setup (fdk-aac)
   - Stream Processing Pipeline

## Support

If you encounter issues not covered here:
- Check [FFI_INTEGRATION.md](FFI_INTEGRATION.md) for detailed docs
- Review [SINGLE_BINARY_FFI.md](architecture/SINGLE_BINARY_FFI.md) for architecture
- Check Rust FFI implementation: [transcoder/src/ffi.rs](../transcoder/src/ffi.rs)
- Check Go bindings: [internal/transcoder/rust.go](../internal/transcoder/rust.go)
