# Bundled font assets

Provenance for the typefaces in `android/app/src/main/res/font/`. Both families are
licensed under the SIL Open Font License 1.1; the license texts sit next to this file
and must ship with any redistribution of the fonts.

| Resource | Upstream file | Family / weight | SHA-256 |
| --- | --- | --- | --- |
| `ibm_plex_sans_regular.ttf` | `IBMPlexSans-Regular.ttf` | IBM Plex Sans 400 | `975dcda37d80f038dcd143c22e33ca2d97a0cc5a929aace1c749153b0fe1afa5` |
| `ibm_plex_sans_medium.ttf` | `IBMPlexSans-Medium.ttf` | IBM Plex Sans 500 | `331c8639d7598b2cde62a911a71db195e30cb655cd6bdf2e324a7e984955f907` |
| `ibm_plex_sans_bold.ttf` | `IBMPlexSans-Bold.ttf` | IBM Plex Sans 700 | `9e6c74a889a700d707613d24548fe4ffa6bc59559a0689d2cf9e133bdcdafb2f` |
| `space_grotesk_ttf.ttf` | `SpaceGrotesk-Regular.ttf` | Space Grotesk 400 | `5ede28c4425f3fe4830c8f4754b39e9a87a93d0c3baa5e0a9924532aaa8a98bd` |

## Sources

IBM Plex Sans — release `@ibm/plex-sans@1.1.0`, static TrueType builds from
`ibm-plex-sans/fonts/complete/ttf/` inside the release archive:

    https://github.com/IBM/plex/releases/download/%40ibm/plex-sans%401.1.0/ibm-plex-sans.zip

Space Grotesk — static instances from the upstream repository:

    https://raw.githubusercontent.com/floriankarsten/space-grotesk/master/fonts/ttf/static/SpaceGrotesk-Regular.ttf

## Re-fetching

Fetch with `curl -fL` so an HTTP error fails the download instead of writing the error
page to disk — every file here was previously a GitHub 404 page committed
with a `.ttf` extension, which AAPT2 packages without complaint and which only fails
at runtime, as text silently falling back to the system font.

After replacing any file, confirm the bytes and refresh the checksums above:

    ./backend/scripts/ci/check-font-assets.sh
    shasum -a 256 android/app/src/main/res/font/*.ttf

That check also runs in CI, in the `Verify Android Font Assets` job of
`.github/workflows/repo-health.yml`.
