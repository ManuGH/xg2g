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
- TV / remote integration

## Build Flavors

The app defines three product flavors:

- `dev`
- `staging`
- `prod`

Flavor differences today:

- `dev` allows cleartext traffic for emulator-local HTTP during development
- `staging` and `prod` remain HTTPS-only
- deep/app-link hosts are flavor-specific

The server URL itself is configured at runtime through setup UI, app links, or
an explicit `base_url` intent extra.

## Opening In Android Studio

1. Open the `android/` directory in Android Studio.
2. Let Android Studio provision the required JDK/SDK if prompted.
3. Sync the Gradle project.
4. Select a build variant such as `devDebug`.
5. Run on an emulator or device.

On macOS, `./gradlew` also falls back to Android Studio's bundled JBR when
`JAVA_HOME` is not set, so terminal builds can work without a separate JDK
install as long as Android Studio is installed in `/Applications` or
`~/Applications`.

## Local Development

The `dev` flavor is intended for emulator/device testing against a local xg2g
server and permits cleartext `http://10.0.2.2/...` URLs when needed.

- xg2g dev workflow in this repo: see [docs/guides/DEVELOPMENT.md](../docs/guides/DEVELOPMENT.md)

Typical local flow:

```bash
make backend-dev-ui
make webui-dev
```

Then run the Android app as `devDebug` inside the emulator.
Use either the in-app setup screen or the intent override below to point the
client at `http://10.0.2.2:8080/ui/`.

## Runtime URL Override

For ad-hoc testing, you can override the base UI URL through an intent extra:

```bash
adb shell am start \
  -n io.github.manugh.xg2g.android.dev/io.github.manugh.xg2g.android.MainActivity \
  --es base_url http://10.0.2.2:8080/ui/
```

The app also accepts a browser/app link for TV-friendly onboarding:

```text
xg2g://connect?base_url=https%3A%2F%2Fyour-server.example%2Fui%2F
```

When launched from a browser on Android TV / Fire TV, the app stores that base
URL and uses it as the new default server.

## Security Notes

- `dev` allows cleartext traffic so emulator-local HTTP targets such as
  `http://10.0.2.2:8080/ui/` can be used during development.
- `staging` and `prod` are HTTPS-only by default.
- This matches the server-side expectation that browser session cookie flows
  should not rely on plain HTTP outside loopback.

## Next Steps

- Add release signing config
- Decide whether playback remains in WebView or moves to Media3
- Add Android CI and AAB signing/release flow
