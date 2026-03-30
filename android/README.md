# Android Client

This directory contains the Android Studio project for the `xg2g` client.

Current scope:

- Thin Android host for the existing server-side Web UI
- No duplicate product logic for auth, routing, or playback decisions
- Separate Android build/release lifecycle from the Docker server image

## Structure

- `app/` Android application module
- `gradle/` Gradle wrapper assets

## Current Approach

The app starts a `WebView` and loads the existing xg2g UI from the server.
This keeps the backend and `frontend/webui/` as the single source of product
behavior while still creating a proper Android client shell.

Native additions should be limited to Android-specific concerns such as:

- Media3 / ExoPlayer playback
- Picture-in-picture
- Notifications
- File pickers / share targets
- Better deep-link handling

## Build Flavors

The app defines three product flavors:

- `dev`
- `staging`
- `prod`

Defaults:

- `dev` points to `http://10.0.2.2:8080/ui/` for the Android emulator
- `staging` points to `https://staging.example.invalid/ui/`
- `prod` points to `https://xg2g.example.invalid/ui/`

You should change the `staging` and `prod` URLs before shipping.

## Opening In Android Studio

1. Open the `android/` directory in Android Studio.
2. Let Android Studio provision the required JDK/SDK if prompted.
3. Sync the Gradle project.
4. Select a build variant such as `devDebug`.
5. Run on an emulator or device.

## Local Development

The `dev` flavor is wired for the Android emulator:

- backend UI host: `http://10.0.2.2:8080/ui/`
- xg2g dev workflow in this repo: see [docs/guides/DEVELOPMENT.md](../docs/guides/DEVELOPMENT.md)

Typical local flow:

```bash
make backend-dev-ui
make webui-dev
```

Then run the Android app as `devDebug` inside the emulator.

## Runtime URL Override

For ad-hoc testing, you can override the base UI URL through an intent extra:

```bash
adb shell am start \
  -n io.github.manugh.xg2g.android.dev/io.github.manugh.xg2g.android.MainActivity \
  --es base_url http://10.0.2.2:8080/ui/
```

## Security Notes

- `dev` allows cleartext traffic for emulator-local HTTP.
- `staging` and `prod` are HTTPS-only by default.
- This matches the server-side expectation that browser session cookie flows
  should not rely on plain HTTP outside loopback.

## Next Steps

- Add app icon and signing config
- Add deep links
- Decide whether playback remains in WebView or moves to Media3
- Add Android CI and AAB signing/release flow
