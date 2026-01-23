# Client Capability Profiles

Operator-contracted capability sets for common client classes.

## Profile: Safari iOS (Cellular)

```yaml
name: safari_ios_cellular
description: Safari on iOS via cellular network (conservative)
version: 1
capabilities:
  containers: ["mp4", "mov", "m4v"]
  video_codecs: ["h264"]  # HEVC only on newer devices
  audio_codecs: ["aac", "mp3"]
  supports_hls: true
  supports_range: false  # Cellular proxies often strip Range
  device_type: "safari_ios"
known_issues:
  - "Range requests may be stripped by carrier proxies"
  - "Private Relay may interfere with streaming"
recommended_mode: "DirectStream"  # Avoid DirectPlay on cellular
```

## Profile: Apple TV

```yaml
name: apple_tv
description: Apple TV (tvOS) - full capability
version: 1
capabilities:
  containers: ["mp4", "mov", "m4v", "mkv"]
  video_codecs: ["h264", "hevc"]
  audio_codecs: ["aac", "ac3", "eac3"]
  supports_hls: true
  supports_range: true
  device_type: "apple_tv"
known_issues:
  - "Dolby Vision requires specific profile"
  - "AC3 passthrough depends on audio receiver"
recommended_mode: "DirectPlay"
```

## Profile: Generic HLS Client

```yaml
name: generic_hls
description: Any HLS-capable client (conservative defaults)
version: 1
capabilities:
  containers: []  # No direct container playback
  video_codecs: ["h264"]  # Baseline only
  audio_codecs: ["aac"]
  supports_hls: true
  supports_range: false  # Unknown, assume no
  device_type: "generic_hls"
known_issues:
  - "Unknown client - use conservative settings"
recommended_mode: "DirectStream"
```

## Usage

```go
profile := LoadProfile("safari_ios_cellular")
input.Capabilities = profile.ToCapabilities()
```

Profiles are operator-managed contracts, not auto-detected.
