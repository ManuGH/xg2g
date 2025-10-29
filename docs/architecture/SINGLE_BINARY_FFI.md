# Single Binary Architecture: Go + Rust via FFI

## Executive Summary

This document describes how to combine Go and Rust code into a **single executable binary** using FFI (Foreign Function Interface). This provides the best user experience: one binary, simple deployment, unified configuration.

## Architecture Overview

```
┌─────────────────────────────────────────┐
│        xg2g (Single Binary)             │
├─────────────────────────────────────────┤
│                                         │
│  ┌────────────────────────────────┐    │
│  │      Go Main Binary            │    │
│  │  • HTTP API                    │    │
│  │  • Business Logic              │    │
│  │  • Configuration               │    │
│  │  • Orchestration               │    │
│  └────────────┬───────────────────┘    │
│               │                         │
│               │ CGO/FFI                 │
│               │                         │
│  ┌────────────▼───────────────────┐    │
│  │  Rust Library (libxg2g.so)    │    │
│  │  • Audio Remuxing              │    │
│  │  • Stream Processing           │    │
│  │  • Performance-Critical Code   │    │
│  └────────────────────────────────┘    │
│                                         │
└─────────────────────────────────────────┘

Single executable: xg2g (statically linked)
```

## Implementation Strategy

### 1. Rust as C-Compatible Library

Convert Rust transcoder to a **cdylib** (C-compatible dynamic library):

```toml
# transcoder/Cargo.toml
[lib]
name = "xg2g_transcoder"
crate-type = ["cdylib", "rlib"]  # cdylib for FFI, rlib for Rust tests
```

### 2. C-Compatible API

Create simple C-compatible functions in Rust:

```rust
// transcoder/src/ffi.rs
use std::os::raw::{c_char, c_int, c_void};
use std::ffi::CStr;

/// Initialize the audio remuxer
/// Returns: Handle to remuxer instance or NULL on error
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_init(
    sample_rate: c_int,
    channels: c_int,
    bitrate: c_int,
) -> *mut c_void {
    // Implementation
    std::ptr::null_mut()
}

/// Process audio data
/// Returns: Number of bytes written or -1 on error
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_process(
    handle: *mut c_void,
    input: *const u8,
    input_len: usize,
    output: *mut u8,
    output_capacity: usize,
) -> c_int {
    // Implementation
    -1
}

/// Free the remuxer
#[no_mangle]
pub extern "C" fn xg2g_audio_remux_free(handle: *mut c_void) {
    // Implementation
}
```

### 3. Go CGO Bindings

Call Rust library from Go using CGO:

```go
// internal/transcoder/rust.go
package transcoder

// #cgo LDFLAGS: -L${SRCDIR}/../../target/release -lxg2g_transcoder
// #include <stdlib.h>
// #include "rust_bindings.h"
import "C"
import (
    "unsafe"
    "errors"
)

type RustAudioRemuxer struct {
    handle unsafe.Pointer
}

func NewRustAudioRemuxer(sampleRate, channels, bitrate int) (*RustAudioRemuxer, error) {
    handle := C.xg2g_audio_remux_init(
        C.int(sampleRate),
        C.int(channels),
        C.int(bitrate),
    )

    if handle == nil {
        return nil, errors.New("failed to initialize Rust remuxer")
    }

    return &RustAudioRemuxer{handle: handle}, nil
}

func (r *RustAudioRemuxer) Process(input []byte) ([]byte, error) {
    output := make([]byte, len(input)*2)  // Allocate output buffer

    written := C.xg2g_audio_remux_process(
        r.handle,
        (*C.uchar)(unsafe.Pointer(&input[0])),
        C.size_t(len(input)),
        (*C.uchar)(unsafe.Pointer(&output[0])),
        C.size_t(len(output)),
    )

    if written < 0 {
        return nil, errors.New("remuxing failed")
    }

    return output[:written], nil
}

func (r *RustAudioRemuxer) Close() {
    if r.handle != nil {
        C.xg2g_audio_remux_free(r.handle)
        r.handle = nil
    }
}
```

### 4. C Header File

Create header file for CGO:

```c
// internal/transcoder/rust_bindings.h
#ifndef XG2G_TRANSCODER_H
#define XG2G_TRANSCODER_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// Initialize audio remuxer
void* xg2g_audio_remux_init(int sample_rate, int channels, int bitrate);

// Process audio data
int xg2g_audio_remux_process(
    void* handle,
    const uint8_t* input,
    size_t input_len,
    uint8_t* output,
    size_t output_capacity
);

// Free remuxer
void xg2g_audio_remux_free(void* handle);

#ifdef __cplusplus
}
#endif

#endif // XG2G_TRANSCODER_H
```

## Build Process

### Development Build

```bash
# 1. Build Rust library
cd transcoder
cargo build --release

# 2. Build Go binary (links against Rust library)
cd ..
CGO_ENABLED=1 go build -o xg2g ./cmd/daemon
```

### Production Build (Static Linking)

For maximum portability, statically link Rust library:

```bash
# Build Rust as static library
cd transcoder
cargo rustc --release -- -C prefer-dynamic=no

# Build Go with static linking
cd ..
CGO_ENABLED=1 CGO_LDFLAGS="-static" \
  go build -o xg2g ./cmd/daemon
```

### Docker Multi-Stage Build

```dockerfile
# Stage 1: Build Rust library
FROM rust:1.75 AS rust-builder
WORKDIR /build
COPY transcoder/ ./transcoder/
WORKDIR /build/transcoder
RUN cargo build --release

# Stage 2: Build Go binary with Rust library
FROM golang:1.25 AS go-builder
WORKDIR /build
COPY --from=rust-builder /build/transcoder/target/release/libxg2g_transcoder.so /usr/local/lib/
COPY --from=rust-builder /build/transcoder/target/release/libxg2g_transcoder.a /usr/local/lib/
COPY . .
RUN CGO_ENABLED=1 go build -o xg2g ./cmd/daemon

# Stage 3: Runtime
FROM alpine:3.22
RUN apk add --no-cache ca-certificates libgcc
COPY --from=go-builder /build/xg2g /usr/local/bin/
COPY --from=rust-builder /build/transcoder/target/release/libxg2g_transcoder.so /usr/local/lib/
RUN ldconfig /usr/local/lib
CMD ["/usr/local/bin/xg2g"]
```

## Memory Safety

### Rust Side
- Use `Box` for heap allocations
- Ensure proper cleanup in `_free` functions
- No panics across FFI boundary (use `catch_unwind`)

```rust
use std::panic::catch_unwind;

#[no_mangle]
pub extern "C" fn xg2g_safe_function() -> c_int {
    let result = catch_unwind(|| {
        // Rust code that might panic
        do_something()
    });

    match result {
        Ok(value) => value,
        Err(_) => -1,  // Return error code instead of panicking
    }
}
```

### Go Side
- Keep references to prevent GC
- Use `runtime.KeepAlive()` when passing pointers
- Proper cleanup with `defer`

```go
func (r *RustAudioRemuxer) ProcessSafe(input []byte) ([]byte, error) {
    defer runtime.KeepAlive(input)  // Prevent GC during C call

    // ... FFI call ...

    return output, nil
}
```

## Advantages of Single Binary

✅ **User Experience**
- One file to download and run
- No separate services to manage
- Simpler configuration

✅ **Deployment**
- Single binary → Easy distribution
- No inter-service networking issues
- Reduced attack surface

✅ **Performance**
- Direct function calls (no HTTP overhead)
- Shared memory (no serialization)
- Lower latency

✅ **Development**
- Unified debugging
- Single build process
- Consistent versioning

## Disadvantages & Trade-offs

❌ **Build Complexity**
- Requires CGO (slower builds)
- Platform-specific compilation
- More complex CI/CD

❌ **Flexibility**
- Can't scale Go and Rust independently
- Harder to hot-swap Rust component
- Tighter coupling

❌ **Debugging**
- CGO can complicate debugging
- Stack traces span languages
- GDB/LLDB setup more complex

## Hybrid Approach (Recommended)

Support **BOTH** architectures:

```
1. Single Binary (default)
   - Best for simple deployments
   - One file, easy to use

2. Microservice Mode (optional)
   - Flag: --external-transcoder=http://localhost:3000
   - For advanced users
   - Better scalability
```

## Platform Support

| Platform | Static Linking | Dynamic Linking | Notes |
|----------|----------------|-----------------|-------|
| Linux x86_64 | ✅ | ✅ | Full support |
| Linux ARM64 | ✅ | ✅ | Full support |
| macOS x86_64 | ⚠️ | ✅ | Static linking difficult |
| macOS ARM64 | ⚠️ | ✅ | Static linking difficult |
| Windows x86_64 | ❌ | ✅ | Use DLL |

## Testing Strategy

### Unit Tests
- Rust: `cargo test` (without FFI)
- Go: `go test` (mock FFI calls)

### Integration Tests
- Build single binary
- Test FFI boundary
- Verify memory safety

### Performance Tests
- Compare FFI vs HTTP overhead
- Measure latency
- Profile memory usage

## Migration Path

### Phase 1: Add FFI Layer (Week 1)
- Create Rust FFI wrapper
- Add Go CGO bindings
- Keep HTTP mode as fallback

### Phase 2: Single Binary Build (Week 2)
- Update Makefile/build scripts
- Docker multi-stage build
- CI/CD updates

### Phase 3: Testing & Optimization (Week 3)
- Performance benchmarks
- Memory leak testing
- Production testing

### Phase 4: Documentation & Release (Week 4)
- User documentation
- Migration guide
- Release v2.0

## Example Usage

```go
// internal/proxy/proxy.go
package proxy

import (
    "github.com/ManuGH/xg2g/internal/transcoder"
)

type StreamProxy struct {
    remuxer *transcoder.RustAudioRemuxer
}

func (p *StreamProxy) TranscodeAudio(stream io.Reader) (io.Reader, error) {
    // Use embedded Rust library
    var buf bytes.Buffer

    chunk := make([]byte, 188 * 1024)  // 1024 TS packets
    for {
        n, err := stream.Read(chunk)
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }

        // Process through Rust FFI
        output, err := p.remuxer.Process(chunk[:n])
        if err != nil {
            return nil, err
        }

        buf.Write(output)
    }

    return &buf, nil
}
```

## Performance Comparison

| Approach | Latency | Throughput | Memory | Complexity |
|----------|---------|------------|--------|------------|
| HTTP API | 5-10ms | 100 Mbps | High | Low |
| Unix Socket | 2-5ms | 200 Mbps | Medium | Medium |
| FFI (Single Binary) | <1ms | 500+ Mbps | Low | High |

## Conclusion

The **Single Binary approach** provides:
- ✅ Best user experience
- ✅ Optimal performance
- ✅ Simplified deployment

While requiring:
- ⚠️ More complex build process
- ⚠️ CGO dependency
- ⚠️ Careful memory management

**Recommendation:** Implement both modes, default to Single Binary for simplicity.

---

**Status:** Design Complete, Implementation Pending
**Estimated Effort:** 2-3 weeks
**Priority:** High (after Phase 2 audio remuxing basics)
