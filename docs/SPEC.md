# linkctl — Iteration 1 Spec

Self-contained Go binary that controls an Insta360 Link 2 webcam from a localhost
web UI and over MQTT (with Home Assistant discovery). Runs in the user session with
a tray icon. Reference implementation for the camera protocol:
[jweaver60/link-studio](https://github.com/jweaver60/link-studio) (MIT, Linux/GTK).

## Platforms & build

- Targets: Windows 11 (amd64), macOS (amd64 + arm64). Linux is out of scope.
- Pure Go, `CGO_ENABLED=0`, cross-compiled from Windows.
- Windows: DirectShow COM via `syscall` — `IAMVideoProcAmp`, `IAMCameraControl`
  for standard controls; `IKsControl` (`KSPROPERTY_EXTENSION_UNIT`) for vendor XU.
- macOS: IOKit USB control requests to the VideoControl interface + `NSStatusItem`
  tray, both via `github.com/ebitengine/purego`. Fallback if blocked: cgo on macOS only.
- Module `github.com/tlanfer/linkctl`, binary `linkctl`.

## Camera

- Match USB VID `0x2E1A`; PIDs 0x4C01 Link, 0x4C02 Link 2C, 0x4C03 Link 2 Pro,
  0x4C04 Link 2 (officially supported; others best-effort).
- Hot-plug: poll enumeration every 2 s. One camera active; internals keyed by camera id.
- Control polling every 1 s (configurable); only changes are emitted.
- Ranges read from the device where possible; baseline table as fallback.
- Identity: serial string from XU unit 9 selector 0x03 (device info);
  fallback: configured "device name".

### Controls (iteration 1)

| Key | Kind | Source |
|---|---|---|
| brightness, contrast, saturation, sharpness, hue | range | UVC processing unit |
| white_balance_auto | bool | UVC PU |
| white_balance_temperature | range (K) | UVC PU |
| anti_flicker | enum off/50/60/auto | UVC PU (power line frequency) |
| focus_auto, focus | bool, range | UVC camera terminal |
| pan, tilt (degrees), zoom (%) | range | UVC CT |
| hdr, mirror | bool | XU 9 / 0x1B feature mask bits 2, 3 |
| exposure_compensation | range | XU 9 / 0x09 |

## PTZ & presets

- Web UI: absolute sliders, nudge buttons, continuous (joystick + keyboard).
- MQTT/HA: absolute + nudge; continuous commands auto-stop after ~500 ms unless repeated.
- Presets: user picks groups (position, picture) to capture; apply only touches
  captured groups. Triggered from UI, MQTT/HA `select`, and an optional default
  preset applied automatically when a video app starts using the camera (see
  hardware findings). Stored per machine in config.

## Web UI / API

- Bound to `127.0.0.1` only; reject foreign `Host` / `Origin`, require JSON
  content type for mutations.
- Internal JSON API, metadata-driven (`GET /api/v1/state`, `PATCH /api/v1/controls`),
  SSE at `/api/v1/events`. Full contract: `docs/design/prototype/api.js`.
- Preact + htm vendored, no build step, embedded via `go:embed`.
- Control panel with optional local preview (`getUserMedia`), settings page.
  Visual language: `docs/DESIGN.md`.

## MQTT / Home Assistant

- MQTT v5 (`eclipse/paho.golang`, autopaho), target broker EMQX.
- `<base>/<id>/availability` online/offline, retained, LWT.
- `<base>/<id>/state` single retained JSON document.
- `<base>/<id>/set` JSON commands: `{"brightness":60}`, `{"preset":"Desk"}`,
  `{"action":"center"}`, `{"action":"reset"}`, `{"nudge":{"pan":-5}}`,
  `{"move":{"pan":1},"ttl_ms":500}`.
- Device-based discovery at `<discovery_prefix>/device/<id>/config`, retained:
  numbers (ranges), switches (bools), selects (anti-flicker, preset), buttons
  (center, nudges, reset picture), binary sensor "In use", sensor "Connected to".
- Settings: enabled, host, port, TLS (off/on/insecure), username, password,
  device name, base topic (`insta360link`), discovery prefix (`homeassistant`),
  test connection, live status.
- Password in OS keychain (`zalando/go-keyring`), fallback to config with UI warning.

## Tray

Open control panel (also left click on Windows) · Start on login (Windows
`HKCU\...\Run`, macOS LaunchAgent; started with `--background`) · Quit.

## Testing / done criteria

1. `go test ./...` green (fake camera, embedded `mochi-mqtt` broker, HA
   discovery checks); `GOOS=darwin` amd64/arm64 builds succeed.
2. On Windows with a real Link 2: all controls, motion types and preset functions
   work via the web UI, also while another app streams the camera.
3. Tray, start-on-login and settings page work on Windows.
4. `linkctl diag` ready for macOS verification.
5. Home Assistant verified manually by the user.

Hardware tests behind the `hardware` build tag.

## Hardware findings (2026-11-24, Link 2 firmware v3.3.0.8, Windows 11)

- DirectShow ranges match link-studio; pan/tilt are reported in degrees.
  Anti-flicker is VideoProcAmp property 13; auto WB / autofocus are the Auto
  flag of the WhiteBalance / Focus properties.
- XU unit 9 = topology node with GUID `FAF1672D-B71B-4793-8C91-7B1C9B7F95F8`,
  unit 10 = `E307E649-4618-A3FF-82FC-2D8B5F216773`.
- Device info contains the serial (22 chars, `17B0…`) and firmware version →
  the serial is the device id.
- **The gimbal parks while idle:** without an active video stream, pan/tilt/zoom
  revert to a rest pose ~2 s after a write; picture settings persist. On stream
  start the camera moves to a wake pose and restores its last zoom.
- No camera-side stream indicator was found (XU 9/0x02 offset 50 turned out to
  be the zoom value; KS pin instance counts are per handle). Camera use is
  detected by the OS instead: Windows ConsentStore (`LastUsedTimeStop == 0`,
  validated against running processes because killed apps leave stale
  entries — per app, not per device); macOS CoreMediaIO
  `kCMIODevicePropertyDeviceIsRunningSomewhere` (per device).
- Consequence: the default preset is applied when a stream starts (after a
  1.5 s settle), not on USB connect. Exposed as `streaming` in the API/MQTT and
  as HA binary sensor "In use".

## Gimbal motion (2026-11-24, learned from the official Insta360 app)

- Windows' standard pan/tilt API is whole degrees only, and its reads refresh
  only about once a second.
- The official app's joystick calls `movePanTilt(speedPan, speedTilt)`. Watching
  the camera while it was used showed it writes **XU 9 / 0x16**,
  `[panDir, panSpeed, tiltDir, tiltSpeed]` (dir as int8 −1/0/+1, idle
  `00 01 00 01`), ramping speed 4 → 8. **Tilt dir +1 moves down.**
- Measured speed is about 2°/s per step and nearly linear (1 → 0.9°/s,
  8 → 14°/s, 32 → 66°/s, 64 → 129°/s). The camera keeps moving until a stop is
  written.
- **XU 9 / 0x1A** = actual position: int32 tilt, int32 pan, in arc-seconds.
  It's live during motion. linkctl uses it for pan/tilt readouts.
- linkctl drives pan/tilt continuously through 0x16:
  - eased velocity (τ 120 ms up, 60 ms down)
  - RC-style expo `0.75·x³ + 0.25·x`
  - speed limited to 24 (~47°/s)
  - updates rate-limited to 40 ms
  - a stop on release, on TTL expiry, before absolute moves, and on shutdown
- Zoom (no speed control) and cameras without 0x16 use stepped absolute writes.
- Measured through the API: deflection 0.15 ≈ 3°/s, 0.5 ≈ 10°/s, 1.0 ≈ 49°/s.

## AI features (2026-11-24, recorded from the official app, verified by writing)

| Feature | Register | Encoding | Status |
|---|---|---|---|
| Mode | XU 9/0x02 (61 B) | [0] id: 0 normal, 1 tracking, 6 DeskView (also 7 group tracking, 4 whiteboard: **not offered**, untested); 0xFF = switching. [1] flag (DeskView 0x10, otherwise 0). Write: reset to 0, wait 600 ms, write the target, keeping bytes 52+. linkctl tilts to −42° about 1.5 s after switching to DeskView, because the firmware doesn't tilt down by itself | verified |
| **Composition** (one control: `composition`) | XU 9/0x1B bit 0 (on/off) + XU 9/0x13 (framing) | 0 off (bit cleared); 1 head, 2 upper body, 3 full body (framing written first, then the bit set) | verified (zoom 180 / 110 / 100 %) |
| Tracking speed | XU 9/0x12 | **0 fast, 1 normal, 2 slow** (inverted, confirmed by feel) | verified |
| ~~People detected~~ | XU 10/0x01 byte 4 | increments each time tracking is enabled and ignores people leaving the frame: a counter, not presence | **removed** |
| ~~Target locked~~ | XU 9/0x02 byte 54, bit 0x20 | stays set regardless | **removed** |
| Gesture event | XU 9/0x05 | ~1 s pulse: 0x02, 0x04, 0x08 and combinations (zoom gesture) | decoded, not built yet |
| Gesture enable | XU 9/0x1B bit 14? | | unverified |
| Track-Stopp-Bereich | unknown (only sent while tracking is active?) | | unmapped |

Modes only take effect while a video client streams. Home Assistant gets mode,
composition and tracking-speed selects. Presets saved with the earlier
`smart_composition` + `composition_framing` pair are migrated to `composition`
when linkctl starts (`camera.MigrateValues`).
