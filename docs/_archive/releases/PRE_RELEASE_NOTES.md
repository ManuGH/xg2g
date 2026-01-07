# Pre-Release Notes: v3.1 (The "Universal" Update)

**Tag**: `v3.1.0-rc1` (Proposed)
**Codename**: "Clean Slate"

## 🚀 The Big Change: Universal Policy

This release marks a architectural pivot. We have stopped chasing browser-specific quirks with complex "Profiles" and "Auto-Switching".
Instead, we now deliver **one, high-quality, standard-compliant stream** that works everywhere.

- **Universal**: H.264 Video + AAC Audio + fMP4 Container + HLS.
- **Simple**: No configuration dropdowns. No "try Safari mode". It just works.
- **Robust**: If it doesn't work, we fix the server pipeline, not add a client hack.

## ⚠️ Breaking Changes

1. **Profiles Deleted**: The `profile` config is gone.
2. **Env Var Trap**: If you leave `XG2G_STREAM_PROFILE` in your `.env`, xg2g will crash on startup/fail-start (by design). **Check your env!**

## 🛠️ Upgrade Instructions

See [UPGRADE.md](../guides/UPGRADE.md) for detailed steps.

## 🙏 Acknowledgements

Thanks to the team for the clean implementation of PR 5.2 and the Docs Reset in PR 5.3.
