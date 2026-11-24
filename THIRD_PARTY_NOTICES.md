# Third-party notices

## link-studio

Camera protocol knowledge — control ranges and defaults, the Insta360 UVC
extension-unit selectors (unit 9: device info 0x03, exposure compensation 0x09,
feature mask 0x1B with HDR bit 2 / mirror bit 3) and their value encodings — is
derived from [link-studio](https://github.com/jweaver60/link-studio)
(MIT License, Copyright (c) 2026 Link Studio contributors). No source code was
copied; see `internal/camera/specs.go` and `internal/camera/camera.go`.

## Web UI assets (embedded in the binary)

- `internal/web/ui/vendor/preact-htm.js` — htm 3.1.1 `preact/standalone`
  build: Preact (MIT) and htm (Apache-2.0).
- `internal/web/ui/fonts/jetbrains-mono-*.woff2` — JetBrains Mono, SIL Open
  Font License 1.1 (`internal/web/ui/fonts/OFL-JetBrainsMono.txt`).

## Go modules

Dependencies and versions are listed in `go.mod`. Notable licenses:

- eclipse/paho.golang — EPL-2.0 / EDL-1.0
- fyne.io/systray — Apache-2.0
- ebitengine/purego — Apache-2.0
- zalando/go-keyring — MIT
- golang.org/x/sys — BSD-3-Clause
- mochi-mqtt/server — MIT (tests only)
