# Upgrade Guide

## Upgrading to v3.1 (Universal Policy)

**Release Date**: Jan 2026  
**Status**: Pre-Release / Main Branch

> [!WARNING]
> **BREAKING CHANGE**: Streaming Profiles have been removed.
> The system now enforces a single `universal` delivery policy.

### 1. Breaking Changes

#### 1.1. Removal of Streaming Profiles

The concept of "profiles" (`auto`, `safari`, `native`) is gone.

- **Old**: Client selects profile, or auto-switches based on error.
- **New**: Server delivers **H.264/AAC/fMP4/HLS** to all clients. No switching.

#### 1.2. Environment Variable Changes

The `XG2G_STREAM_PROFILE` environment variable is **REMOVED**.

- If this variable is present, the application will **REFUSE TO START** to prevent misconfiguration.
- **Action**: Unset `XG2G_STREAM_PROFILE`.
- **Action**: (Optional) Set `XG2G_STREAMING_POLICY=universal` (this is the default).

#### 1.3. Configuration File (`config.yaml`)

- **Removed**: `streaming.default_profile`
- **Removed**: `streaming.allowed_profiles`
- **Added**: `streaming.delivery_policy` (must be `universal`)

### 2. Migration Steps

1. **Stop the service**.
2. **Update Configuration**:
   - Remove `XG2G_STREAM_PROFILE` from `docker-compose.yml` or env files.
   - Remove `default_profile` / `allowed_profiles` from `config.yaml`.
3. **Pull new image**: `ghcr.io/manugh/xg2g:latest` (or v3.1 tag).
4. **Start the service**.
5. **Verify**: Check logs. If it crashes with `XG2G_STREAM_PROFILE removed`, you missed Step 2.

### 3. Troubleshooting

**"My stream stopped working in Safari!"**

- This is likely a pipeline/codec issue, not a "profile" issue.
- Do NOT try to find a "native" profile setting.
- Check server logs for FFmpeg errors (`vaapi` or `nvenc` failures).
- Report a bug if the server logs look clean but playback fails.
