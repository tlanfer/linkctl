# linkctl — Design Language

The web UI is a small instrument panel for one camera. It lives in a narrow
browser window beside a video call as often as in a full desktop tab, so it
has to be dense, calm and readable at a glance.

The language is synthesized from three references:

| Reference | What we take |
|---|---|
| **Lunor** (dark dashboard) | Graphite surface ramp, 1px hairline borders, muted text hierarchy, big plain-sans stat numbers, quiet sidebar-style navigation, ghost buttons. |
| **DeerFlow** (AIoT cards) | Numbered card headers in small-caps mono (`01  AGENT STATUS`), dot-matrix numerals and dotted rings, a single orange-red accent used sparingly for *live / active* state, status pills (`STABLE ●`), corner-bracket viewfinder. |
| **Preferences** (soft settings panel) | Soft raised cards with a faint inner top highlight, list-style setting rows (icon · label · control on the right), light-knob toggles, the filled "Brightness 80%" bar slider, segmented choices. |

## 1. Principles

1. **Instrument, not website.** Every element shows a camera value or changes
   one. No decoration without data.
2. **One accent, earned.** Orange-red means *live, active or moving*: the
   active preset, a moving joystick, a pressed control, the focus ring. Everything
   else is graphite and white.
3. **Numbers are the hero.** Values are set in monospace with tabular figures
   so they don't jitter while dragging; headline readouts use the dot-matrix
   display face.
4. **Calm depth.** Cards are raised just enough to separate (hairline border +
   inner highlight + soft shadow). Never glossy, never gradient-heavy.
5. **Narrow first.** Designed at 380px, then expanded to two columns.
6. **Offline.** Every asset (fonts, icons, Preact) is embedded in the binary.
   No CDN at runtime.

## 2. Color tokens

Dark-only for v1 (see §11). All colors are CSS custom properties on `:root`.

### Surfaces (graphite ramp)

| Token | Value | Use |
|---|---|---|
| `--bg` | `#0f0f11` | Page background |
| `--surface-0` | `#141416` | App shell / header band |
| `--surface-1` | `#1a1a1d` | Cards |
| `--surface-2` | `#222226` | Insets: slider tracks, inputs, setting-row groups, tiles |
| `--surface-3` | `#2b2b30` | Hover, pressed, raised controls (buttons, knobs' shadow side) |
| `--surface-4` | `#35353b` | Active segment, selected tile |

### Lines and depth

| Token | Value | Use |
|---|---|---|
| `--line` | `rgba(255,255,255,0.06)` | Hairline borders, row dividers |
| `--line-strong` | `rgba(255,255,255,0.11)` | Input borders, hover borders |
| `--highlight` | `inset 0 1px 0 rgba(255,255,255,0.045)` | Inner top highlight on cards and buttons |
| `--shadow-1` | `0 1px 2px rgba(0,0,0,.4), 0 8px 24px -12px rgba(0,0,0,.6)` | Cards |
| `--shadow-2` | `0 12px 40px -8px rgba(0,0,0,.7)` | Dialogs, toasts, menus |

### Text

| Token | Value | Contrast on `--surface-1` | Use |
|---|---|---|---|
| `--text-1` | `#ededef` | 15.4:1 | Values, headings, primary labels |
| `--text-2` | `#a3a3ab` | 7.1:1 | Secondary labels, body |
| `--text-3` | `#71717a` | 3.9:1 | Captions, units, card numbers, disabled (large/UI text only) |

### Accent and status

| Token | Value | Use |
|---|---|---|
| `--accent` | `#ff5a1f` | Live / active / moving, focus ring, primary button text |
| `--accent-soft` | `rgba(255,90,31,0.14)` | Accent fills (active tile, toggle-on track) |
| `--accent-line` | `rgba(255,90,31,0.45)` | Accent borders |
| `--ok` | `#5fd18b` | Connected |
| `--warn` | `#f2c14e` | Connecting, keychain fallback warning |
| `--danger` | `#ff4d5e` | Errors, destructive actions |

Rules: accent and status colors appear as **dots, text, 1px lines or 14% fills**,
never as large solid blocks. The only solid accent shape is the moving
joystick knob.

## 3. Typography

| Role | Token | Stack | Notes |
|---|---|---|---|
| UI | `--font-ui` | `"Inter", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif` | System fonts (Segoe UI Variable / SF Pro) render well. Inter is **not** vendored in v1. |
| Mono | `--font-mono` | `"JetBrains Mono", ui-monospace, "SF Mono", "Cascadia Mono", Consolas, monospace` | Vendored: `JetBrains Mono` 400/500, Latin subset woff2 (~35 kB each, OFL). Values, units, card labels, pills. |

All numeric text uses `font-variant-numeric: tabular-nums`.

| Token | Size / line | Weight | Use |
|---|---|---|---|
| `--fs-display` | 34px / 1 | UI sans 600, tabular figures | Big readouts (pan/tilt/zoom) |
| `--fs-title` | 18px / 1.3 | 600 | Page title, dialog title |
| `--fs-body` | 14px / 1.45 | 400–500 | Labels, rows |
| `--fs-small` | 12.5px / 1.4 | 400 | Captions, helper text |
| `--fs-label` | 11px / 1, `letter-spacing: .08em`, uppercase | mono 500 | Card headers `01  POSITION`, pills, units |

The pan/tilt/zoom readouts use the regular UI font with tabular figures. The
dot-matrix display font was dropped after review. The prototype loads
JetBrains Mono from Google Fonts. The production build serves the vendored
woff2 files from `internal/web/ui/fonts/` through `@font-face` with
`font-display: swap`.

## 4. Space, radius, elevation

4px base grid.

| Token | Value |
|---|---|
| `--sp-1` … `--sp-8` | 4, 8, 12, 16, 20, 24, 32, 40 px |
| `--r-xs` | 6px: chips, small tags |
| `--r-sm` | 10px: inputs, slider bars, icon buttons |
| `--r-md` | 14px: tiles, setting-row groups |
| `--r-lg` | 20px: cards |
| `--r-pill` | 999px: pills, pill buttons, toggles |

Card padding is `--sp-5` (20px), or `--sp-4` under 480px. The gap between
cards is `--sp-4`.

**Elevation:** cards have `--surface-1`, a `1px solid var(--line)` border,
`box-shadow: var(--highlight), var(--shadow-1)`. Inset groups (Preferences
style) have `--surface-2` with no shadow. Floating layers (dialog, menu,
toast) have `--surface-2`, `--line-strong` and `--shadow-2`.

## 5. Components

Class names below are the ones used in `docs/design/prototype/styles.css`;
the production UI uses the same ones.

### Card: `.card`
Header row: `.card-num` (`01`, mono, `--text-3`), two spaces, then
`.card-title` (mono label, `--text-2`, uppercase). Optional right-aligned
`.card-actions` hold ghost icon buttons (reset, more). A live indicator
`● LIVE` / `● MOVING` in accent goes in the header when relevant.

Numbering is fixed per card: `01 PREVIEW`, `02 POSITION`, `03 PRESETS`,
`04 IMAGE`, `05 EXPOSURE`, `06 WHITE BALANCE`, `07 FOCUS`. In settings:
`01 MQTT`, `02 GENERAL`, `03 ABOUT`.

### Setting row: `.row`
Taken from Preferences: `[icon 18px] label …………… control`, 52px min height,
divided by `--line`, grouped in an inset `.rows` block (`--surface-2`,
`--r-md`). Helper or error text sits under the label in `--fs-small`.

### Bar slider: `.bar`
The filled bar from Preferences ("Brightness 80%"):
- A 36px-high `--surface-2` track with `--r-sm` corners. The fill is
  `--surface-4`, with a 2px `--text-1` edge line at the fill end.
- The value is printed **inside** the bar, left, in mono (`80`, with the unit
  in `--text-3`). The label is either in the row (Preferences layout) or above
  the bar.
- Drag anywhere. It's a native `<input type=range>` stretched transparent over
  the visuals, so keyboard, screen readers and step semantics stay native.
- Centered ranges (hue, pan, tilt) fill from zero outward, not from min.
- While dragging, the edge line and value turn `--accent`.
- When auto-driven (`auto_key` on), the bar is dimmed to 45% with an `AUTO`
  chip, and it's still draggable: dragging switches the control to manual,
  matching the camera's behavior.

### Toggle: `.toggle`
A 44×26 pill. The track is `--surface-3`. The knob is a 20px light disc
(`linear-gradient(#f4f4f5,#c9c9ce)` with a small shadow), as in Preferences.
On: the knob moves right and the track becomes `--accent-soft` with an
`--accent-line` border. It's a `<button role=switch aria-checked>`.

### Segmented: `.seg`
For enums (anti-flicker) and the Control / Settings tabs: an inset
`--surface-2` pill containing buttons. The selected segment is `--surface-4`
with `--highlight`, the rest `--text-2`.

### Buttons
- `.btn`: pill, 36px, `--surface-3`, `--line` border, `--highlight`. The
  default.
- `.btn.primary`: same surface with accent text and accent border. Used for
  one call to action per view (Save preset, Save settings).
- `.btn.ghost`: no surface until hover (Lunor's "Edit").
- `.btn.danger`: danger text.
- `.icon-btn`: 36px square with `--r-sm` corners, a 18px icon. Ghost
  variant in card headers.

### Joystick: `.pad`
A circular field (max 220px) drawn as a **dotted ring** (DeerFlow), with a
crosshair of dots and a 44px knob. Idle knob: light disc like the toggle.
While dragging, the knob turns solid `--accent` and the ring dots on the drag
side light up. Release springs back to center (`--ease-out`, 180ms).
**The four nudge buttons sit on the dotted ring** at N/E/S/W: 36px round
buttons centered on the ring's radius. A click nudges 3° (`NUDGE_DEG`), and
press-and-hold (280ms) moves continuously. They sit next to the drag area in
the DOM, so pressing one never starts a drag. There's no separate d-pad:
Center is the card-header button, a double-click on the pad, or the `0` key.
Big readouts (regular UI font, tabular figures) for PAN, TILT and ZOOM stack
vertically beside the joystick, and sit in a row of three when the card is a
single column.

### Streaming / idle state (Position card)
The Link 2 only holds a pan/tilt/zoom position **while some app is
streaming video**. When idle it parks the gimbal and reverts to its rest pose
about 2 s after a write. Picture settings persist either way. The state comes
from `streaming` in `GET /api/v1/state`.
- `streaming: true`: accent pill `● IN USE` in the Position card header
  (DeerFlow "LIVE/REC" style).
- `streaming: false`: hollow pill `○ IDLE`, plus a `.notice` strip under the
  header: *"Position holds only while a video app uses the camera…"*.
  The controls stay **enabled**, because a nudge is still useful to aim
  before a call.
- The default preset (★) is applied automatically when a stream starts. The
  Presets card says so in its footnote and in the star's tooltip.

### Preset tile: `.tile`
`--surface-2`, `--r-md`, 1px `--line`, min 140×88. Content: mono index
`01`, name (`--text-1`, 500), group chips `POS` / `PIC` (`.chip`, mono 10px).
The default preset (applied when a stream starts) gets a small star. The active tile has an accent ring
(`--accent-line`), `--accent-soft` wash and `● ACTIVE` label. Click applies;
the `⋯` menu holds Rename, Recapture, Set as default, Move left/right, Delete.
The last tile is a dashed "+ Save current" tile.

### Status pill: `.pill`
Mono label, `--r-pill`, `--surface-2`, a 6px dot before the text:
`● CONNECTED` (ok), `● CONNECTING` (warn, pulsing), `● ERROR` (danger),
`○ DISABLED` (`--text-3`, hollow dot).

### Toast: `.toast`
Bottom-center stack, `--surface-2`, `--shadow-2`, 3px left rule in the status
color, auto-dismiss after 5s (errors 8s), with a close button.

### Dialog: `.dialog`
A native `<dialog>`, `--r-lg`, max 420px, title plus body plus right-aligned
actions. The backdrop is `rgba(0,0,0,.55)` with a 4px blur.

### Viewfinder (preview): `.viewfinder`
16:9 black box with four corner brackets (DeerFlow "Vision AI"). Contains
the `<video>`, or an empty state when it's off.

### Empty state: "no camera"
A large dotted ring with a slowly rotating accent arc (DeerFlow "device
overview") and the mono text `NO CAMERA`. A caption explains that linkctl is
waiting for an Insta360 Link to be plugged in.

## 6. Motion

| Token | Value |
|---|---|
| `--ease-out` | `cubic-bezier(.2,.8,.2,1)` |
| `--dur-1` | 120ms: hover, press |
| `--dur-2` | 180ms: toggles, knob return, tile state |
| `--dur-3` | 260ms: dialogs, toasts |

Only animate `transform`, `opacity`, `background-color`, `border-color`.
Honor `prefers-reduced-motion: reduce`: disable the rotating ring, pulsing
dots and knob spring.

## 7. Iconography

Inline SVG, 24-unit viewBox, `stroke="currentColor"`, `stroke-width=1.5`,
round caps and joins, no fills (the Lunor and Preferences icon style). Rendered at
18px in rows and 16px in buttons. The set lives in `icons.js`; no icon font.

## 8. Layout

- **Header** (no bar: no background or border, about 50px tall): logo mark
  plus `linkctl` (18px/600), then the camera model on a mono line under it.
  Firmware and hostname are not shown here; they're on the Settings › About
  card and in the MQTT state. Then the camera and MQTT pills, and the
  Control / Settings segmented tabs. The header scrolls away with the page on
  narrow screens; there is no page scroll on wide screens.
- **Control view uses the full screen.** The preview must never scroll out of
  view, and nothing ever scrolls horizontally. There are three panes:
  **stage** (Preview + Presets), **mid** (Position + AI), and **side** (one
  Image card).
  - **1300px and wider:** three full-height columns,
    `minmax(440px, 1.4fr) minmax(400px, 1fr) minmax(380px, 1fr)`. The page
    never scrolls (`html, body { overflow: hidden }`).
    - Stage is a flex column. Presets keep their natural height, up to 45%,
      scrolling inside beyond that. An active preview takes all remaining
      height and fits its aspect ratio with container query units:
      `width: min(100cqw, 100cqh × ar)`.
    - Mid and side scroll vertically on their own if they don't fit.
    - Measured with realistic browser viewports: **nothing scrolls** at
      3440×1300, 2560×1300 and 1920×930. Below that, columns scroll
      vertically only.
  - **900–1299px:** two columns with grid areas `"stage mid" "stage side"`.
    The page scrolls, and an active preview is pinned under the header.
  - **Below 900px:** one column (stage, mid, side), max width 560px.
- **Flat cards:** rows sit directly on the card, with no inner panel (`.rows` has no background or border; this also applies on
  the settings page).
  - **Image card:** Image, Exposure, White balance and Focus in one card. Every
    section except the first gets a small mono `.section-label`. Sliders whose
    auto flag is on (color temperature, focus) are hidden.
  - **Notes moved to tooltips:** the explanatory notes on the Position and AI
    cards live in their **Idle** pill's tooltip.
- **Position card:** the joystick (nudge buttons on its ring) and, to its
  right, the **zoom rocker** (`.rocker`), with full-width **line sliders** for
  Pan and Tilt (`.lslider`) below them.
  - **The zoom rocker is a one-axis joystick, not an absolute slider.** Drag
    up to zoom in, down to zoom out. Speed follows the displacement (12% dead
    zone, rescaled; the service applies easing and expo), and the knob springs
    back on release. Above it is the current zoom value; at its ends are −/+
    step-and-hold buttons. With keyboard focus, ↑/↓ steps 25%.
  - **Line sliders:** caption with a large value, a thin 2px track with a fill
    from zero, and the toggle-style light knob. While dragging, the fill turns
    accent.
  - The "position holds only while streaming" explanation lives in the
    **Idle** pill's tooltip. Keyboard shortcuts are behind the **ⓘ** button.
- **Card numbers follow reading order** (left to right, top to bottom):
  01 Preview, 02 Presets, 03 Position, 04 AI, 05 Image. Settings cards are
  numbered separately.
- **Choice rows** (segmented controls): the control sits next to the label and
  wraps onto its own line only when there isn't enough room (`.row.enum`).
- **Rows have no dividers.** Spacing alone separates them; section labels act
  as headings.
- **Settings view:** one column, max 640px, rows grouped like Preferences. It
  scrolls inside the main area.
- Page gutter: 16px narrow, 20–24px wide. There's no horizontal scroll at 360px.

- **Mirrored image:** when the camera's `mirror` control is on, every
  horizontal pan control follows the picture rather than the gimbal: the
  joystick x-axis, the ring's left/right buttons, the arrow keys and Shift+arrow
  nudges, and the pan slider (drawn reversed, with `dir="rtl"` on the range
  input). The pan value itself (readouts, presets, API, MQTT) is unchanged.
- **Preview sizing:** the viewfinder takes the stream's real aspect ratio
  (`videoWidth / videoHeight`, stored in `--ar`). Its height is capped per
  layout with `--vf-max-h` (34vh narrow, 40vh two panes, 52vh three panes). When
  the cap applies, the box gets narrower and stays centered, so the image never
  has black bars.

## 9. Accessibility

- Text contrast is at least 4.5:1 for all text except `--text-3`, which is
  used only for 11px uppercase labels and units next to a higher-contrast value.
- Focus: `outline: 2px solid var(--accent); outline-offset: 2px` on
  `:focus-visible`, everywhere.
- Every control is a native element (`input[type=range]`, `button`,
  `role=switch`, `<dialog>`, `<select>` fallback).
- **Keyboard joystick** (when focus isn't in a text field):
  - Arrow keys: continuous pan and tilt while held
  - `Shift` + arrow keys: nudge by 3°
  - `+` / `-`: zoom while held
  - `0`: center
  - `1`–`9`: apply preset *n*
- Live regions: toasts are `role=status` (errors use `role=alert`).
- Hit targets are at least 36px (including the ring nudge buttons), and 44px
  for the joystick knob.

## 10. Implementation notes

- The prototype in `docs/design/prototype/` is structurally the real UI:
  - `index.html`, `styles.css`, `app.js` (Preact + htm components) and `icons.js`.
  - `api.js` is the real `fetch` / `EventSource` client. `mock.js` implements
    the same interface in memory and is chosen with `?mock` (the prototype
    defaults to the mock when it isn't served by linkctl).
- Preact + htm: the prototype imports
  `https://unpkg.com/htm@3.1.1/preact/standalone.module.js`. **The
  production build vendors that file** as
  `internal/web/ui/vendor/preact-htm.js` (about 13 kB) and imports it
  relatively, with no CDN.
- Fonts are vendored under `internal/web/ui/fonts/` (see §3).
- Slider writes are throttled to one `PATCH` every 80ms while dragging, plus
  a final write on release. Incoming SSE state does not overwrite a slider
  while it's being dragged.
- Joystick: 12% dead zone, then the deflection is rescaled to 0..1, so speed
  starts from zero at the edge of the dead zone. The service applies an RC-style
  expo curve (`e·x³ + (1−e)·x`, e = 0.75) and drives the gimbal's own speed
  control, giving about 1°/s at a light touch and about 47°/s at full tilt.
- Joystick: it sends `POST /api/v1/ptz/move {pan,tilt,ttl_ms:500}` every 200ms
  while held, and `{pan:0,tilt:0,zoom:0}` on release.

## 11. Light theme

**Dark-only for v1.** All three references are dark, the UI sits next to
video calls where a bright panel is distracting, and every value is already
a token, so a light theme later means adding one `[data-theme=light]` block.
