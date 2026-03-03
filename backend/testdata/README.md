# Test Data Directory

This directory contains test assets used for development and testing. These files are excluded from version control to keep the repository clean and fast to clone.

## Structure

```
testdata/
├── videos/          # Test video files (MP4, etc.)
├── segments/        # Transport stream segments (TS files)
├── logs/            # Test output logs
├── scripts/         # Test helper scripts
└── README.md        # This file
```

## Usage

### Video Files

Test videos for codec validation, playback testing, and FFmpeg parameter tuning.

**Examples:**

- `test_hevc_*.mp4` - HEVC codec test files with various encoding parameters
- `manual_test.mp4` - General-purpose test video

### Segments

Downloaded or generated transport stream segments for HLS testing.

**Examples:**

- `verify_*.ts` - Verification segments
- `downloaded_seg.ts` - Sample downloaded segments

### Logs

Test output logs from various test runs.

**Example:**

- `test_output_*.log` - Timestamped test execution logs

### Scripts

Helper scripts for test setup and execution.

**Example:**

- `start_test_server.sh` - Test server startup script

## Adding New Test Assets

1. Place files in appropriate subdirectory
2. Use descriptive names: `test_<feature>_<variation>.ext`
3. Document purpose if non-obvious
4. Keep files <10MB when possible (consider external hosting for larger files)

## Cleanup

Test assets are local-only and not tracked by Git (.gitignore). Clean up manually as needed:

```bash
# Remove all test data
rm -rf testdata/videos/* testdata/segments/* testdata/logs/*

# Or selectively
rm testdata/logs/test_output_*.log
```

## Git Ignore

This directory is excluded from Git via `.gitignore`. If you need to commit specific test files, add them explicitly:

```bash
git add -f testdata/videos/important_test.mp4
```

## Best Practices

- ✅ Keep test files organized by type
- ✅ Use descriptive names
- ✅ Document non-obvious test cases
- ❌ Don't commit large binaries to Git (use Git LFS or external hosting)
- ❌ Don't leave temporary files (clean up after tests)

---

**Last Updated:** 2026-01-07

## Fetching Test Data

Test assets are not committed to Git to keep the repository lightweight and clone times fast.

### Quick Start

```bash
# Fetch test data (requires TESTDATA_URL env var)
./scripts/fetch-testdata.sh

# Or with custom CDN source
TESTDATA_URL=https://my-cdn.com/test-assets ./scripts/fetch-testdata.sh
```

### Why not commit test files?

- Faster repository clones
- Smaller .git folder
- CI/CD efficiency
- Better developer experience

### Local Development

Test files in `testdata/` are gitignored but can be regenerated locally:

1. Set `TESTDATA_URL` to your CDN endpoint
2. Run `./scripts/fetch-testdata.sh`
3. Files populated in `testdata/{videos,segments,logs}`

