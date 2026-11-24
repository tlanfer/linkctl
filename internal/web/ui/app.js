// linkctl web UI (prototype == structure of the production UI).
import { html, render, useState, useEffect, useRef, useCallback, useMemo } from './preact.js';
import { Icon, Logo } from './icons.js';
import { createApi } from './api.js';
import { createMockApi } from './mock.js';

const useMock = window.LINKCTL_MOCK === true || new URLSearchParams(location.search).has('mock') || location.protocol === 'file:';
const api = useMock ? createMockApi() : createApi();

const MOVE_REPEAT_MS = 200;
const MOVE_TTL_MS = 500;
const HOLD_MS = 280;
const NUDGE_DEG = 3; // arrow click / Shift+arrow step

// ---------------------------------------------------------------- helpers
const store = {
  get(k, d) { try { const v = localStorage.getItem('linkctl.' + k); return v === null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem('linkctl.' + k, JSON.stringify(v)); } catch { /* private mode */ } },
};
const isCentered = (s) => s.min < 0 && s.max > 0;
function fmt(s, v) {
  if (v === null || v === undefined) return '—';
  const sign = isCentered(s) && v > 0 ? '+' : '';
  return sign + v;
}
const typingTarget = (el) => el && (el.closest('input:not([type=range]), textarea, select, [contenteditable]') || el.closest('dialog[open]'));

// ---------------------------------------------------------------- toasts
let pushToast = () => {};
function Toasts() {
  const [items, setItems] = useState([]);
  useEffect(() => {
    pushToast = (msg, kind = 'error') => {
      const id = Math.random();
      setItems((xs) => [...xs.slice(-3), { id, msg, kind }]);
      setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), kind === 'error' ? 8000 : 5000);
    };
  }, []);
  return html`<div class="toasts">${items.map((t) => html`
    <div class=${'toast ' + t.kind} role=${t.kind === 'error' ? 'alert' : 'status'} key=${t.id}>
      <span class="msg">${t.msg}</span>
      <button class="icon-btn ghost sm" aria-label="Dismiss" onClick=${() => setItems((xs) => xs.filter((x) => x.id !== t.id))}><${Icon} name="x" /></button>
    </div>`)}</div>`;
}
const run = (p, ok) => Promise.resolve(p).then((r) => { if (ok) pushToast(ok, 'ok'); return r; }, (e) => { pushToast(e.message || String(e)); throw e; }).catch(() => {});

// ---------------------------------------------------------------- primitives
function Card({ num, title, actions, live, children, class: cls }) {
  return html`<section class=${'card ' + (cls || '')} aria-labelledby=${'card-' + num}>
    <header class="card-head">
      <span class="card-num">${num}</span><h2 class="card-title" id=${'card-' + num} style="margin:0">${title}</h2>
      ${live && html`<span class="live" style="margin-left:10px">${live}</span>`}
      <div class="card-actions">${actions}</div>
    </header>
    ${children}
  </section>`;
}

function Toggle({ checked, onChange, label, disabled }) {
  return html`<button class="toggle" role="switch" aria-checked=${checked ? 'true' : 'false'} aria-label=${label}
    disabled=${disabled} onClick=${() => onChange(!checked)} />`;
}

function Seg({ options, value, onChange, label, small, fill }) {
  return html`<div class=${'seg' + (small ? ' small' : '') + (fill ? ' fill' : '')} role="group" aria-label=${label}>
    ${options.map((o, i) => html`<button aria-pressed=${value === (o.value ?? i) ? 'true' : 'false'}
      onClick=${() => onChange(o.value ?? i)}>${o.icon && html`<${Icon} name=${o.icon} size="15" />`}${o.label ?? o}</button>`)}
  </div>`;
}

// Filled bar slider (DESIGN.md §5). Throttles writes to one per 80ms.
// Shared range-input behaviour: throttles writes to one per 80ms while
// dragging, and keeps incoming state from overwriting the value mid-drag.
function useRangeInput(spec, value, onChange) {
  const [drag, setDrag] = useState(null);
  const last = useRef(0), timer = useRef(null), pend = useRef(null), release = useRef(null), sent = useRef(null);
  const v = drag ?? value ?? spec.default;
  const flush = () => {
    clearTimeout(timer.current); timer.current = null;
    if (pend.current !== null && pend.current !== sent.current) { sent.current = pend.current; last.current = Date.now(); onChange(pend.current); }
    pend.current = null;
  };
  const send = (x) => {
    pend.current = x;
    const wait = 80 - (Date.now() - last.current);
    if (wait <= 0) flush(); else if (!timer.current) timer.current = setTimeout(flush, wait);
  };
  const onInput = (e) => { const x = +e.target.value; clearTimeout(release.current); setDrag(x); send(x); };
  const onDone = (e) => { send(+e.target.value); flush(); release.current = setTimeout(() => setDrag(null), 500); };
  useEffect(() => () => { clearTimeout(timer.current); clearTimeout(release.current); }, []);
  return { v, dragging: drag !== null, onInput, onDone };
}

// Geometry of a value on a track (percent), filled from zero for centred ranges.
function trackGeom(spec, v, reverse) {
  const range = spec.max - spec.min || 1;
  let pct = ((v - spec.min) / range) * 100;
  let zero = isCentered(spec) ? ((0 - spec.min) / range) * 100 : null;
  if (reverse) { pct = 100 - pct; if (zero !== null) zero = 100 - zero; }
  const from = zero ?? (reverse ? 100 : 0);
  return { pct, zero, left: Math.min(from, pct), width: Math.abs(pct - from) };
}

// Two-line slider: caption + large value, thin track with a light knob
// (Preferences-style). Used for pan / tilt / zoom.
function LineSlider({ spec, value, onChange, caption, reverse, extra }) {
  const { v, dragging, onInput, onDone } = useRangeInput(spec, value, onChange);
  const g = trackGeom(spec, v, reverse);
  return html`<div class=${'lslider' + (dragging ? ' dragging' : '')}>
    <div class="ls-head">
      <span class="label">${caption}</span>
      ${extra}
      <span class="ls-value">${fmt(spec, v)}${spec.unit && html`<span class="unit">${spec.unit}</span>`}</span>
    </div>
    <div class="ls-track">
      ${g.zero !== null && html`<span class="ls-zero" style=${`left:${g.zero}%`} />`}
      <span class="ls-fill" style=${`left:${g.left}%;width:${g.width}%`} />
      <span class="ls-knob" style=${`left:${g.pct}%`} />
      <input type="range" dir=${reverse ? 'rtl' : 'ltr'} min=${spec.min} max=${spec.max} step=${spec.step || 1} value=${v}
        aria-label=${caption} aria-valuetext=${fmt(spec, v) + (spec.unit || '')} onInput=${onInput} onChange=${onDone} />
    </div>
  </div>`;
}

function Bar({ spec, value, onChange, auto, label, reverse, big, caption }) {
  const { v, dragging, onInput, onDone } = useRangeInput(spec, value, onChange);
  const drag = dragging ? v : null;
  const range = spec.max - spec.min || 1;
  const pct = ((v - spec.min) / range) * 100;
  let left = 0, width = pct, neg = false, zero = null;
  if (isCentered(spec)) {
    zero = ((0 - spec.min) / range) * 100;
    left = Math.min(zero, pct); width = Math.abs(pct - zero); neg = v < 0;
  }
  if (reverse) { // mirrored: max on the left
    left = 100 - left - width;
    if (zero !== null) zero = 100 - zero;
  }
  return html`<div class=${'bar' + (big ? ' big' : '') + (drag !== null ? ' dragging' : '') + (auto ? ' auto' : '')}>
    ${caption && html`<span class="bar-caption label">${caption}</span>`}
    ${zero !== null && html`<span class="bar-zero" style=${`left:${zero}%`} />`}
    <span class=${'bar-fill' + (neg ? ' neg' : '')} style=${`left:${left}%;width:${Math.max(width, 0.0001)}%`} />
    <span class="bar-value">${fmt(spec, v)}${spec.unit && html`<span class="unit">${spec.unit}</span>`}</span>
    ${auto && html`<span class="bar-label"><span class="chip">Auto</span></span>`}
    <input type="range" dir=${reverse ? 'rtl' : 'ltr'} min=${spec.min} max=${spec.max} step=${spec.step || 1} value=${v}
      aria-label=${label || spec.label} aria-valuetext=${fmt(spec, v) + (spec.unit || '')}
      onInput=${onInput} onChange=${onDone} />
  </div>`;
}

const CONTROL_ICONS = {
  brightness: 'sun', contrast: 'contrast', saturation: 'drop', sharpness: 'sharp', hue: 'hue',
  exposure_compensation: 'aperture', hdr: 'hdr', anti_flicker: 'wave', white_balance_auto: 'wbAuto',
  white_balance_temperature: 'temp', focus_auto: 'focus', focus: 'eye', mirror: 'mirror',
  ai_mode: 'robot', composition: 'frame', tracking_speed: 'speed',
  people: 'people', target_locked: 'target',
};

function ControlRow({ c, values, set }) {
  const v = values[c.key];
  const icon = html`<span class="row-icon"><${Icon} name=${CONTROL_ICONS[c.key] || 'sliders'} /></span>`;
  const err = c.error && html`<small class="err">${c.error}</small>`;
  if (c.read_only) { // camera status, not a setting
    const text = v == null ? '—' : c.kind === 'bool' ? (v ? 'Yes' : 'No') : String(v);
    return html`<div class="row">${icon}<span class="row-label">${c.label}${err}</span>
      <span class="row-control"><span class=${'readonly-value mono' + (v ? ' on' : '')}>${text}</span></span></div>`;
  }
  if (c.kind === 'bool') {
    return html`<div class="row">${icon}<span class="row-label">${c.label}${err}</span>
      <span class="row-control"><${Toggle} checked=${!!v} label=${c.label} onChange=${(x) => set({ [c.key]: x ? 1 : 0 })} /></span></div>`;
  }
  if (c.kind === 'enum') { // inline next to the label; wraps only if it does not fit
    return html`<div class="row enum">${icon}<span class="row-label">${c.label}${err}</span>
      <span class="row-control"><${Seg} small options=${c.options} value=${v} label=${c.label} onChange=${(x) => set({ [c.key]: x })} /></span></div>`;
  }
  const auto = c.auto_key && values[c.auto_key] === 1;
  return html`<div class="row wrap-narrow">${icon}<span class="row-label">${c.label}${auto ? html`<small>Drag to switch to manual</small>` : err}</span>
    <span class="row-control grow"><${Bar} spec=${c} value=${v} auto=${auto} onChange=${(x) => set({ [c.key]: x })} /></span></div>`;
}

// ---------------------------------------------------------------- header
function mqttPill(m) {
  switch (m?.state) {
    case 'connected': return html`<span class="pill ok" title=${m.broker}><span class="dot" />MQTT</span>`;
    case 'connecting': return html`<span class="pill warn" title=${m.broker}><span class="dot" />MQTT</span>`;
    case 'error': return html`<span class="pill danger" title=${m.error}><span class="dot" />MQTT</span>`;
    default: return html`<span class="pill off" title="MQTT disabled"><span class="dot" />MQTT</span>`;
  }
}

function Header({ st, online, view, setView }) {
  const d = st?.device;
  return html`<header class="header"><div class="header-inner">
    <div class="brand">
      <span class="logo"><${Logo} /></span>
      <span style="min-width:0">
        <div class="brand-name">linkctl</div>
        <div class="device-line">${d ? d.model : online ? 'waiting for camera' : 'service unreachable'}</div>
      </span>
    </div>
    <span class="header-spacer" />
    <div class="pills">
      ${!online ? html`<span class="pill danger"><span class="dot" />Offline</span>`
        : st?.connected ? html`<span class="pill ok"><span class="dot" />Camera</span>`
        : html`<span class="pill warn"><span class="dot" />No camera</span>`}
      ${mqttPill(st?.mqtt)}
    </div>
    <${Seg} label="View" value=${view} onChange=${setView}
      options=${[{ value: 'control', label: 'Control', icon: 'sliders' }, { value: 'settings', label: 'Settings', icon: 'gear' }]} />
  </div></header>`;
}

// ---------------------------------------------------------------- motion
// Repeats /ptz/move while a direction is held; sends zeros on stop.
function useMover() {
  const vec = useRef(null), timer = useRef(null);
  const tick = () => { if (vec.current) api.move({ ...vec.current, ttl_ms: MOVE_TTL_MS }).catch((e) => { stop(); pushToast(e.message); }); };
  const start = useCallback((v) => {
    const changed = JSON.stringify(v) !== JSON.stringify(vec.current);
    vec.current = v;
    if (!timer.current) { timer.current = setInterval(tick, MOVE_REPEAT_MS); tick(); } else if (changed) tick();
  }, []);
  const stop = useCallback(() => {
    if (!timer.current && !vec.current) return;
    clearInterval(timer.current); timer.current = null; vec.current = null;
    api.move({ pan: 0, tilt: 0, zoom: 0 }).catch(() => {});
  }, []);
  useEffect(() => stop, []);
  return { start, stop };
}

// Click = nudge, press-and-hold = continuous move.
function HoldButton({ cls, label, icon, nudge, move, mover }) {
  const t = useRef(null), held = useRef(false);
  const down = (e) => {
    e.currentTarget.setPointerCapture?.(e.pointerId);
    held.current = false;
    t.current = setTimeout(() => { held.current = true; mover.start(move); }, HOLD_MS);
  };
  const up = () => {
    clearTimeout(t.current);
    if (held.current) mover.stop(); else run(api.nudge(nudge));
    held.current = false;
  };
  const cancel = () => { clearTimeout(t.current); if (held.current) mover.stop(); held.current = false; };
  return html`<button class=${'icon-btn ' + cls} aria-label=${label} title=${label + ' (hold to move)'}
    onPointerDown=${down} onPointerUp=${up} onPointerCancel=${cancel}
    onKeyDown=${(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); run(api.nudge(nudge)); } }}
    onClick=${(e) => e.preventDefault()}><${Icon} name=${icon} /></button>`;
}

function Ring({ angle, active }) {
  const n = 56, dots = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2;
    let lit = false;
    if (active && angle !== null) {
      let diff = Math.abs(a - angle); diff = Math.min(diff, Math.PI * 2 - diff);
      lit = diff < 0.45;
    }
    dots.push(html`<circle cx=${50 + Math.cos(a) * 46} cy=${50 + Math.sin(a) * 46} r=${lit ? 1.25 : 0.8}
      fill=${lit ? 'var(--accent)' : 'var(--text-3)'} opacity=${lit ? 1 : 0.7} />`);
  }
  for (let i = -3; i <= 3; i++) {
    if (i === 0) continue;
    dots.push(html`<circle cx=${50 + i * 9} cy="50" r=".6" fill="var(--text-3)" opacity=".5" />`);
    dots.push(html`<circle cx="50" cy=${50 + i * 9} r=".6" fill="var(--text-3)" opacity=".5" />`);
  }
  return html`<svg class="ring" viewBox="0 0 100 100" aria-hidden="true">${dots}</svg>`;
}

function Joystick({ mover, children, flip }) {
  const ref = useRef(null);
  const [knob, setKnob] = useState(null); // {x,y} px offset while dragging
  const R = 62;
  const update = (e) => {
    const r = ref.current.getBoundingClientRect();
    let x = e.clientX - (r.left + r.width / 2), y = e.clientY - (r.top + r.height / 2);
    const len = Math.hypot(x, y);
    if (len > R) { x = (x / len) * R; y = (y / len) * R; }
    setKnob({ x, y });
    // Dead zone, then rescale so speed starts from 0 at its edge. The service
    // applies an expo curve, so small deflections give fine, slow control.
    const dz = 0.12;
    const axis = (n) => (Math.abs(n) < dz ? 0 : +(Math.sign(n) * (Math.abs(n) - dz) / (1 - dz)).toFixed(3));
    // Mirrored image: screen-right is camera pan-left.
    const pan = axis((flip ? -x : x) / R) || 0, tilt = axis(-y / R);
    if (pan || tilt) mover.start({ pan, tilt }); else mover.stop();
  };
  const end = () => { setKnob(null); mover.stop(); };
  const angle = knob && (knob.x || knob.y) ? Math.atan2(knob.y, knob.x) : null;
  return html`<div class="pad-wrap">
    <div ref=${ref} class=${'pad' + (knob ? ' active' : '')} role="application"
      aria-label="Joystick: drag to pan and tilt. Arrow keys move, 0 centers."
      onPointerDown=${(e) => { e.currentTarget.setPointerCapture(e.pointerId); update(e); }}
      onPointerMove=${(e) => knob && update(e)} onPointerUp=${end} onPointerCancel=${end}
      onDblClick=${() => run(api.center())}>
      <${Ring} angle=${angle} active=${!!knob} />
      <span class="pad-knob" style=${knob ? `transform:translate(${knob.x}px,${knob.y}px)` : ''} />
    </div>
    ${children}
  </div>`;
}

// Zoom rocker: a one-axis joystick. Drag up to zoom in, down to zoom out;
// speed follows the displacement (the service applies easing and expo),
// and the knob springs back on release. The ends are step/hold buttons.
function ZoomRocker({ mover, spec, value }) {
  const ref = useRef(null);
  const [off, setOff] = useState(null); // px offset while dragging
  const R = 46;
  const update = (e) => {
    const r = ref.current.getBoundingClientRect();
    const y = Math.max(-R, Math.min(R, e.clientY - (r.top + r.height / 2)));
    setOff(y);
    const n = -y / R, dz = 0.12;
    const z = Math.abs(n) < dz ? 0 : +(Math.sign(n) * (Math.abs(n) - dz) / (1 - dz)).toFixed(3);
    if (z) mover.start({ zoom: z }); else mover.stop();
  };
  const end = () => { setOff(null); mover.stop(); };
  const step = (d) => run(api.nudge({ zoom: d }));
  return html`<div class="zoom-col">
    <div class="zoom-val"><span class="label">Zoom</span><span class="v">${fmt(spec, value)}<span class="unit">%</span></span></div>
    <${HoldButton} cls="sm ghost" label="Zoom in" icon="zoomIn" nudge=${{ zoom: 25 }} move=${{ zoom: 1 }} mover=${mover} />
    <div ref=${ref} class=${'rocker' + (off !== null ? ' active' : '')} tabindex="0" role="slider"
      aria-label="Zoom: drag up to zoom in, down to zoom out" aria-valuemin=${spec.min} aria-valuemax=${spec.max} aria-valuenow=${value}
      onPointerDown=${(e) => { e.currentTarget.setPointerCapture(e.pointerId); update(e); }}
      onPointerMove=${(e) => off !== null && update(e)} onPointerUp=${end} onPointerCancel=${end}
      onKeyDown=${(e) => { if (e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); step(25); } else if (e.key === 'ArrowDown') { e.preventDefault(); e.stopPropagation(); step(-25); } }}>
      <span class="rocker-track" />
      <span class="rocker-knob" style=${off !== null ? `transform:translateY(${off}px)` : ''} />
    </div>
    <${HoldButton} cls="sm ghost" label="Zoom out" icon="zoomOut" nudge=${{ zoom: -25 }} move=${{ zoom: -1 }} mover=${mover} />
  </div>`;
}

// Global keyboard joystick (DESIGN.md §9).
function useKeyboard(mover, presets, flip) {
  // Listeners are registered once. Current values are read through a ref:
  // re-registering on every state update (presets is a new array per SSE
  // event) would reset `held` and lose the key-up -> motion would never stop.
  const ctx = useRef(null);
  ctx.current = { mover, presets, flip };
  useEffect(() => {
    const mv = {
      start: (v) => ctx.current.mover.start(v),
      stop: () => ctx.current.mover.stop(),
    };
    const held = new Set();
    const vec = () => {
      const v = {};
      const ps = ctx.current.flip ? -1 : 1; // mirrored image: left/right swap
      if (held.has('ArrowLeft')) v.pan = -ps; if (held.has('ArrowRight')) v.pan = (v.pan || 0) + ps;
      if (held.has('ArrowUp')) v.tilt = 1; if (held.has('ArrowDown')) v.tilt = (v.tilt || 0) - 1;
      if (held.has('+')) v.zoom = 1; if (held.has('-')) v.zoom = (v.zoom || 0) - 1;
      Object.keys(v).forEach((k) => { if (!v[k]) delete v[k]; });
      return v;
    };
    const norm = (k) => (k === '=' ? '+' : k === '_' ? '-' : k);
    const down = (e) => {
      if (typingTarget(e.target) || e.ctrlKey || e.metaKey || e.altKey) return;
      const k = norm(e.key);
      if (k.startsWith('Arrow') && e.shiftKey) {
        e.preventDefault();
        if (e.repeat) return;
        const ps = ctx.current.flip ? -1 : 1;
        const d = { ArrowLeft: { pan: -ps * NUDGE_DEG }, ArrowRight: { pan: ps * NUDGE_DEG }, ArrowUp: { tilt: NUDGE_DEG }, ArrowDown: { tilt: -NUDGE_DEG } }[k];
        run(api.nudge(d)); return;
      }
      if (k.startsWith('Arrow') || k === '+' || k === '-') {
        if (e.target.matches?.('input[type=range]')) return; // let sliders use arrows
        e.preventDefault(); held.add(k); const v = vec();
        Object.keys(v).length ? mv.start(v) : mv.stop(); return;
      }
      if (k === '0') { e.preventDefault(); run(api.center()); return; }
      const presets = ctx.current.presets;
      if (/^[1-9]$/.test(k) && presets[+k - 1]) { e.preventDefault(); run(api.applyPreset(presets[+k - 1].id)); }
    };
    const up = (e) => {
      const k = norm(e.key);
      if (!held.delete(k)) return;
      const v = vec(); Object.keys(v).length ? mv.start(v) : mv.stop();
    };
    const blur = () => { if (held.size) { held.clear(); mv.stop(); } };
    addEventListener('keydown', down); addEventListener('keyup', up); addEventListener('blur', blur);
    return () => { removeEventListener('keydown', down); removeEventListener('keyup', up); removeEventListener('blur', blur); mv.stop(); };
  }, []);
}

// ---------------------------------------------------------------- cards
function PositionCard({ st, mover }) {
  const c = Object.fromEntries(st.controls.map((x) => [x.key, x]));
  const v = st.values;
  const set = (o) => run(api.setControls(o));
  // With image mirroring on, horizontal controls follow the picture, not the
  // gimbal: screen-right turns the camera left.
  const flip = v.mirror === 1, ps = flip ? -1 : 1;
  return html`<${Card} num="03" title="Position" class="pos-card" live=${st.moving && 'Moving'}
    actions=${html`${st.streaming
      ? html`<span class="pill accent" title="A video app is using the camera"><span class="dot" />In use</span>`
      : html`<span class="pill off" title=${'No app is streaming video. Position holds only while a video app uses the camera; when idle, the gimbal returns to its rest pose after ~2 s.' + (st.default_preset ? ' The default preset is applied when a stream starts.' : '')}><span class="dot" />Idle</span>`}
      <button class="btn ghost sm" onClick=${() => run(api.center())}><${Icon} name="center" />Center</button>
      <button class="icon-btn ghost sm" aria-label="Keyboard shortcuts"
        title=${'Keyboard: arrows move · Shift+arrow nudge ' + NUDGE_DEG + '° · + / − zoom · 0 center · 1–9 apply preset · double-click the pad to center'}><${Icon} name="info" /></button>`}>
    <div class="ptz">
      <${Joystick} mover=${mover} flip=${flip}>
        <${HoldButton} cls="nudge n" label="Tilt up" icon="up" nudge=${{ tilt: NUDGE_DEG }} move=${{ tilt: 1 }} mover=${mover} />
        <${HoldButton} cls="nudge e" label="Pan right" icon="right" nudge=${{ pan: ps * NUDGE_DEG }} move=${{ pan: ps }} mover=${mover} />
        <${HoldButton} cls="nudge s" label="Tilt down" icon="down" nudge=${{ tilt: -NUDGE_DEG }} move=${{ tilt: -1 }} mover=${mover} />
        <${HoldButton} cls="nudge w" label="Pan left" icon="left" nudge=${{ pan: -ps * NUDGE_DEG }} move=${{ pan: -ps }} mover=${mover} />
      <//>
      <${ZoomRocker} mover=${mover} spec=${c.zoom} value=${v.zoom} />
    </div>
    <div class="ptz-sliders">
      <${LineSlider} caption="Pan" spec=${c.pan} value=${v.pan} reverse=${flip} onChange=${(x) => set({ pan: x })} />
      <${LineSlider} caption="Tilt" spec=${c.tilt} value=${v.tilt} onChange=${(x) => set({ tilt: x })} />
    </div>
  <//>`;
}

function PreviewCard() {
  const [on, setOn] = useState(() => store.get('preview', false));
  const [msg, setMsg] = useState('');
  const [label, setLabel] = useState('');
  const [ar, setAr] = useState(null); // stream aspect ratio, e.g. 16/9
  const vid = useRef(null);
  useEffect(() => {
    store.set('preview', on);
    api.demo?.setStreaming(on);
    if (!on) return;
    let stream, cancelled = false;
    (async () => {
      setMsg('Opening camera…');
      try {
        if (!navigator.mediaDevices?.getUserMedia) throw new Error('This browser cannot open cameras here.');
        let s = await navigator.mediaDevices.getUserMedia({ video: true });
        const devs = await navigator.mediaDevices.enumerateDevices();
        const link = devs.find((d) => d.kind === 'videoinput' && /insta360/i.test(d.label) && !/virtual/i.test(d.label));
        if (link && s.getVideoTracks()[0]?.getSettings().deviceId !== link.deviceId) {
          s.getTracks().forEach((t) => t.stop());
          s = await navigator.mediaDevices.getUserMedia({ video: { deviceId: { exact: link.deviceId }, width: 1280, height: 720 } });
        }
        if (cancelled) { s.getTracks().forEach((t) => t.stop()); return; }
        stream = s;
        setLabel(s.getVideoTracks()[0]?.label || '');
        if (!link) setMsg('');
        if (vid.current) vid.current.srcObject = s;
        setMsg(link ? '' : 'Insta360 not found — showing default camera');
      } catch (e) {
        setMsg(e.name === 'NotAllowedError' ? 'Camera permission denied.' : e.name === 'NotReadableError' ? 'Camera is busy in another app.' : e.message);
      }
    })();
    return () => { cancelled = true; stream?.getTracks().forEach((t) => t.stop()); if (vid.current) vid.current.srcObject = null; setLabel(''); setAr(null); };
  }, [on]);
  return html`<${Card} num="01" title="Preview" class=${'preview-card' + (on ? ' on' : '')} live=${on && label && 'Live'}
    actions=${html`<${Toggle} checked=${on} label="Local preview" onChange=${setOn} />`}>
    <div class="vf-wrap"><div class=${"viewfinder" + (on ? "" : " off")}
      style=${on && ar ? `--ar:${ar.toFixed(4)}` : ''}>
      ${on && html`<video ref=${vid} autoplay muted playsinline
        onLoadedMetadata=${(e) => { const v = e.currentTarget; if (v.videoWidth && v.videoHeight) setAr(v.videoWidth / v.videoHeight); }}
        onResize=${(e) => { const v = e.currentTarget; if (v.videoWidth && v.videoHeight) setAr(v.videoWidth / v.videoHeight); }} />`}
      <span class="corner tl" /><span class="corner tr" /><span class="corner bl" /><span class="corner br" />
      ${(!on || msg) && html`<div class="vf-empty"><div>
        ${!on ? html`<div class="label" style="margin-bottom:8px">Preview off</div>Shows the camera from this browser. Some apps can’t share the camera while it’s open here.` : msg}
      </div></div>`}
    </div></div>
  <//>`;
}

function PresetMenu({ p, idx, count, isDefault, onClose, onRename }) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current?.querySelector('button')?.focus();
    const h = (e) => { if (!ref.current?.contains(e.target)) onClose(); };
    const k = (e) => { if (e.key === 'Escape') onClose(); };
    setTimeout(() => addEventListener('pointerdown', h)); addEventListener('keydown', k);
    return () => { removeEventListener('pointerdown', h); removeEventListener('keydown', k); };
  }, []);
  const act = (fn) => () => { onClose(); fn(); };
  return html`<div class="menu" ref=${ref} role="menu" onClick=${(e) => e.stopPropagation()}>
    <button role="menuitem" onClick=${act(onRename)}><${Icon} name="pencil" />Rename</button>
    <button role="menuitem" onClick=${act(() => run(api.updatePreset(p.id, { recapture: true }), `Updated “${p.name}” from camera`))}><${Icon} name="capture" />Recapture current</button>
    <button role="menuitem" onClick=${act(() => run(api.setDefaultPreset(isDefault ? '' : p.id)))}><${Icon} name="star" />${isDefault ? 'Unset default' : 'Make default'}</button>
    ${idx > 0 && html`<button role="menuitem" onClick=${act(() => onRename('left'))}><${Icon} name="left" />Move left</button>`}
    ${idx < count - 1 && html`<button role="menuitem" onClick=${act(() => onRename('right'))}><${Icon} name="right" />Move right</button>`}
    <hr />
    <button role="menuitem" class="danger" onClick=${act(() => run(api.deletePreset(p.id), `Deleted “${p.name}”`))}><${Icon} name="trash" />Delete</button>
  </div>`;
}

function Dialog({ open, onClose, title, children, actions }) {
  const ref = useRef(null);
  useEffect(() => {
    const d = ref.current; if (!d) return;
    if (open && !d.open) d.showModal(); else if (!open && d.open) d.close();
  }, [open]);
  return html`<dialog class="dialog" ref=${ref} onClose=${onClose} onCancel=${onClose}>
    <form method="dialog" onSubmit=${(e) => e.preventDefault()}>
      <h2>${title}</h2>${children}<div class="dialog-actions">${actions}</div>
    </form>
  </dialog>`;
}

function PresetsCard({ st }) {
  const [menu, setMenu] = useState(null);
  const [dlg, setDlg] = useState(null); // {mode:'save'|'rename', id?, name, groups}
  const presets = st.presets;
  const move = (i, dir) => {
    const ids = presets.map((p) => p.id); const j = i + dir;
    [ids[i], ids[j]] = [ids[j], ids[i]]; run(api.reorderPresets(ids));
  };
  const submit = () => {
    const d = dlg;
    const p = d.mode === 'save' ? api.savePreset(d.name, d.groups) : api.updatePreset(d.id, { name: d.name });
    run(p.then(() => setDlg(null)), d.mode === 'save' ? `Saved “${d.name.trim()}”` : null);
  };
  const toggleGroup = (g) => setDlg({ ...dlg, groups: dlg.groups.includes(g) ? dlg.groups.filter((x) => x !== g) : [...dlg.groups, g] });
  return html`<${Card} num="02" title="Presets">
    <div class="tiles">
      ${presets.map((p, i) => {
        const active = st.active_preset === p.id, isDef = st.default_preset === p.id;
        return html`<div style="position:relative" key=${p.id}>
          <button class=${'tile' + (active ? ' active' : '')} style="width:100%" disabled=${!st.connected}
            aria-current=${active ? 'true' : null}
            onClick=${() => run(api.applyPreset(p.id))} title=${`Apply ${p.name} (${i + 1})`}>
            <span class="tile-top"><span class="card-num">${String(i + 1).padStart(2, '0')}</span>
              ${isDef && html`<span class="star" title="Default: applied when a video app starts using the camera"><${Icon} name="star" /></span>`}</span>
            <span class="tile-name">${p.name}</span>
            <span class="tile-chips">
              ${p.groups.includes('position') && html`<span class="chip">Pos</span>`}
              ${p.groups.includes('picture') && html`<span class="chip">Pic</span>`}
              ${p.groups.includes('ai') && html`<span class="chip">AI</span>`}
            </span>
          </button>
          <button class="icon-btn ghost sm more" style="position:absolute;top:6px;right:6px" aria-label=${'More for ' + p.name}
            aria-haspopup="menu" aria-expanded=${menu === p.id} onClick=${() => setMenu(menu === p.id ? null : p.id)}><${Icon} name="more" /></button>
          ${menu === p.id && html`<${PresetMenu} p=${p} idx=${i} count=${presets.length} isDefault=${isDef}
            onClose=${() => setMenu(null)}
            onRename=${(dir) => dir === 'left' ? move(i, -1) : dir === 'right' ? move(i, 1) : setDlg({ mode: 'rename', id: p.id, name: p.name, groups: p.groups })} />`}
        </div>`;
      })}
      <button class="tile add" disabled=${!st.connected} onClick=${() => setDlg({ mode: 'save', name: '', groups: ['position'] })}>
        <${Icon} name="plus" size="18" />Save current
      </button>
    </div>
    <div class="hint" style="margin-top:12px"><span class="star" style="color:var(--warn);display:inline-grid"><${Icon} name="star" size="13" /></span>
      Default preset — applied automatically whenever a video app starts streaming. Keys <span class="kbd">1</span>–<span class="kbd">9</span> apply presets.</div>
    <${Dialog} open=${!!dlg} onClose=${() => setDlg(null)} title=${dlg?.mode === 'rename' ? 'Rename preset' : 'Save preset'}
      actions=${html`<button type="button" class="btn ghost" onClick=${() => setDlg(null)}>Cancel</button>
        <button type="submit" class="btn primary" onClick=${submit} disabled=${!dlg?.name.trim() || !dlg?.groups.length}>${dlg?.mode === 'rename' ? 'Rename' : 'Save'}</button>`}>
      ${dlg && html`
        <label class="form-label" for="preset-name">Name</label>
        <input id="preset-name" class="input" maxlength="60" placeholder="e.g. Desk" value=${dlg.name} autofocus
          onInput=${(e) => setDlg({ ...dlg, name: e.target.value })} onKeyDown=${(e) => e.key === 'Enter' && dlg.name.trim() && dlg.groups.length && submit()} />
        ${dlg.mode === 'save' && html`<div class="checks">
          <label class="check"><input type="checkbox" checked=${dlg.groups.includes('position')} onChange=${() => toggleGroup('position')} />
            <span>Position<small>Pan, tilt, zoom</small></span></label>
          <label class="check"><input type="checkbox" checked=${dlg.groups.includes('picture')} onChange=${() => toggleGroup('picture')} />
            <span>Picture<small>Image, exposure, white balance, focus</small></span></label>
          <label class="check"><input type="checkbox" checked=${dlg.groups.includes('ai')} onChange=${() => toggleGroup('ai')} />
            <span>AI<small>Mode (fixed / tracking / DeskView), composition, tracking speed. A tracking preset leaves position to the camera.</small></span></label>
        </div>`}`}
    <//>
  <//>`;
}

// AI features of the camera firmware (modes need an active video stream).
const AI_CARD = { num: '04', title: 'AI', keys: ['ai_mode', 'composition', 'tracking_speed'] };

function AICard({ st }) {
  const byKey = Object.fromEntries(st.controls.map((c) => [c.key, c]));
  const set = (o) => run(api.setControls(o));
  const keys = AI_CARD.keys;
  return html`<${Card} num=${AI_CARD.num} title=${AI_CARD.title}
    actions=${st.streaming ? null : html`<span class="pill off" title="Modes and tracking work only while a video app uses the camera. DeskView tilts the camera down to the desk."><span class="dot" />Idle</span>`}>
    <div class="rows">${keys.map((k) => byKey[k] && html`<${ControlRow} key=${k} c=${byKey[k]} values=${st.values} set=${set} />`)}</div>
  <//>`;
}

// All picture settings in one card, with section labels. Manual sliders
// driven by an enabled auto flag (white balance, focus) are hidden.
const IMAGE_SECTIONS = [
  { title: null, keys: ['brightness', 'contrast', 'saturation', 'sharpness', 'hue', 'mirror'] }, // no label: the card title says it
  { title: 'Exposure', keys: ['exposure_compensation', 'hdr', 'anti_flicker'] },
  { title: 'White balance', keys: ['white_balance_auto', 'white_balance_temperature'] },
  { title: 'Focus', keys: ['focus_auto', 'focus'] },
];
const IMAGE_KEYS = IMAGE_SECTIONS.flatMap((x) => x.keys);

function ImageCard({ st }) {
  const byKey = Object.fromEntries(st.controls.map((c) => [c.key, c]));
  const set = (o) => run(api.setControls(o));
  const shown = (k) => byKey[k] && !(byKey[k].auto_key && st.values[byKey[k].auto_key] === 1);
  return html`<${Card} num="05" title="Image"
    actions=${html`<button class="icon-btn ghost sm" aria-label="Reset image settings" title="Reset all image settings to defaults"
      onClick=${() => run(api.resetControls(IMAGE_KEYS), 'Image settings reset')}><${Icon} name="reset" /></button>`}>
    ${IMAGE_SECTIONS.map((sec) => html`<div class="section" key=${sec.title || 'image'}>
      ${sec.title && html`<div class="section-label label">${sec.title}</div>`}
      <div class="rows">${sec.keys.filter(shown).map((k) => html`<${ControlRow} key=${k} c=${byKey[k]} values=${st.values} set=${set} />`)}</div>
    </div>`)}
  <//>`;
}

function NoCamera({ online }) {
  const dots = [];
  for (let i = 0; i < 60; i++) {
    const a = (i / 60) * Math.PI * 2;
    dots.push(html`<circle cx=${50 + Math.cos(a) * 46} cy=${50 + Math.sin(a) * 46} r=".9" fill="var(--text-3)" />`);
  }
  const arc = [];
  for (let i = 0; i < 8; i++) {
    const a = (i / 60) * Math.PI * 2;
    arc.push(html`<circle cx=${50 + Math.cos(a) * 46} cy=${50 + Math.sin(a) * 46} r="1.3" fill="var(--accent)" opacity=${0.3 + i / 10} />`);
  }
  return html`<${Card} num="00" title="Camera">
    <div class="empty">
      <div class="ring">
        <svg viewBox="0 0 100 100">${dots}</svg>
        <svg viewBox="0 0 100 100" class="spin">${arc}</svg>
        <div class="txt">${online ? 'NO\nCAMERA' : 'OFF\nLINE'}</div>
      </div>
      <p>${online ? 'Waiting for an Insta360 Link to be plugged in. linkctl checks every two seconds and connects automatically.'
        : 'The linkctl service is not reachable. Is it running? It reconnects automatically.'}</p>
    </div>
  <//>`;
}

function ControlView({ st, online }) {
  const mover = useMover();
  useKeyboard(mover, st?.connected ? st.presets : [], st?.values?.mirror === 1);
  // Wide screens (>= 1300px): three full-height columns, the page never
  // scrolls. Left: preview (fills the height) + presets. Middle: position +
  // AI. Right: image settings.
  if (!st?.connected) {
    return html`<div class="dash">
      <div class="pane stage"><${PreviewCard} />${st && html`<${PresetsCard} st=${st} />`}</div>
      <div class="pane mid"><${NoCamera} online=${online} /></div>
    </div>`;
  }
  return html`<div class="dash">
    <div class="pane stage"><${PreviewCard} /><${PresetsCard} st=${st} /></div>
    <div class="pane mid"><${PositionCard} st=${st} mover=${mover} /><${AICard} st=${st} /></div>
    <div class="pane side"><${ImageCard} st=${st} /></div>
  </div>`;
}

// ---------------------------------------------------------------- settings
function SettingsView({ st }) {
  const [s, setS] = useState(null);
  const [saved, setSaved] = useState(null);
  const [pw, setPw] = useState('');
  const [test, setTest] = useState(null); // {busy} | {ok, error}
  const [busy, setBusy] = useState(false);
  useEffect(() => { run(api.getSettings().then((x) => { setS(x); setSaved(x); })); }, []);
  if (!s) return html`<div class="single"><div class="card"><span class="faint">Loading settings…</span></div></div>`;
  const m = s.mqtt;
  const setM = (patch) => setS({ ...s, mqtt: { ...m, ...patch } });
  const dirty = JSON.stringify(s) !== JSON.stringify(saved) || pw !== '';
  const mqttOut = () => { const out = { ...m }; delete out.password_set; delete out.password_in_file; if (pw !== '') out.password = pw; return out; };
  const save = () => {
    setBusy(true);
    run(api.putSettings({ poll_interval_ms: s.poll_interval_ms, mqtt: mqttOut() })
      .then((x) => { setS(x); setSaved(x); setPw(''); }), 'Settings saved').finally(() => setBusy(false));
  };
  const doTest = () => { setTest({ busy: true }); api.testMqtt(mqttOut()).then(setTest, (e) => setTest({ ok: false, error: e.message })); };
  const ms = st?.mqtt || { state: 'disabled' };
  const statusText = { connected: 'Connected', connecting: 'Connecting…', error: 'Error', disabled: 'Disabled' }[ms.state];

  return html`<div class="single">
    <${Card} num="01" title="MQTT · Home Assistant"
      actions=${html`<${Toggle} checked=${m.enabled} label="Enable MQTT" onChange=${(x) => setM({ enabled: x })} />`}>
      <div class="rows" style="margin-bottom:16px">
        <div class="row"><span class="row-icon"><${Icon} name="broker" /></span>
          <span class="row-label">Status<small class=${ms.state === 'error' ? 'err' : ''}>${ms.state === 'error' ? ms.error : ms.state === 'connected' ? ms.topic : ms.broker || 'Not connected'}</small></span>
          <span class="row-control">${mqttPill(ms)}<span class="mono faint" style="font-size:12px">${statusText}</span></span></div>
      </div>
      <fieldset disabled=${!m.enabled} style="border:0;padding:0;margin:0;${m.enabled ? '' : 'opacity:.5'}">
        <div class="form-grid">
          <div><label class="form-label" for="h">Broker host</label>
            <input id="h" class="input mono" placeholder="emqx.local" value=${m.host} onInput=${(e) => setM({ host: e.target.value.trim() })} /></div>
          <div><label class="form-label" for="p">Port</label>
            <input id="p" class="input mono" inputmode="numeric" value=${m.port} onInput=${(e) => setM({ port: +e.target.value || 0 })} /></div>
          <div class="full"><span class="form-label">Transport security</span>
            <${Seg} fill label="TLS" value=${m.tls} onChange=${(x) => setM({ tls: x, port: x === 'off' && m.port === 8883 ? 1883 : x !== 'off' && m.port === 1883 ? 8883 : m.port })}
              options=${[{ value: 'off', label: 'Off' }, { value: 'on', label: 'TLS' }, { value: 'insecure', label: 'TLS, no verify' }]} /></div>
          <div class="full"><label class="form-label" for="u">Username</label>
            <input id="u" class="input" autocomplete="username" value=${m.username} onInput=${(e) => setM({ username: e.target.value })} /></div>
          <div class="full"><label class="form-label" for="pw">Password</label>
            <input id="pw" class="input" type="password" autocomplete="current-password"
              placeholder=${m.password_set ? '•••••••• stored — type to replace' : 'none'} value=${pw} onInput=${(e) => setPw(e.target.value)} />
            ${m.password_in_file
              ? html`<div class="hint" style="margin-top:6px;color:var(--warn)"><${Icon} name="lock" size="14" />No OS keychain available — the password is stored in the config file.</div>`
              : html`<div class="hint" style="margin-top:6px"><${Icon} name="lock" size="14" />Stored in the OS keychain.</div>`}</div>
          <div class="full"><label class="form-label" for="dn">Device name</label>
            <input id="dn" class="input" value=${m.device_name} onInput=${(e) => setM({ device_name: e.target.value })} />
            <div class="hint" style="margin-top:6px">Shown in Home Assistant. Used as id when the camera reports no serial.</div></div>
          <div class="form-pair"><div class="half"><label class="form-label" for="bt">Base topic</label>
            <input id="bt" class="input mono" value=${m.base_topic} onInput=${(e) => setM({ base_topic: e.target.value.trim() })} /></div>
          <div class="half"><label class="form-label" for="dp">HA discovery prefix</label>
            <input id="dp" class="input mono" value=${m.discovery_prefix} onInput=${(e) => setM({ discovery_prefix: e.target.value.trim() })} /></div></div>
        </div>
      </fieldset>
      <div class="split" style="margin-top:20px">
        <span class="status-line">
          <button class="btn" onClick=${doTest} disabled=${!m.host || test?.busy}><${Icon} name="bolt" />${test?.busy ? 'Testing…' : 'Test connection'}</button>
          ${test && !test.busy && (test.ok
            ? html`<span class="pill ok"><span class="dot" />Reachable</span>`
            : html`<span class="err-text">${test.error}</span>`)}
        </span>
        <button class="btn primary" disabled=${!dirty || busy} onClick=${save}>${busy ? 'Saving…' : 'Save'}</button>
      </div>
    <//>

    <${Card} num="02" title="General">
      <div class="rows">
        <div class="row"><span class="row-icon"><${Icon} name="power" /></span>
          <span class="row-label">Start on login<small>Runs linkctl in the tray when you sign in</small></span>
          <span class="row-control"><${Toggle} checked=${s.start_on_login} label="Start on login"
            onChange=${(x) => run(api.putSettings({ start_on_login: x }).then((r) => { setS({ ...s, start_on_login: r.start_on_login }); setSaved({ ...saved, start_on_login: r.start_on_login }); }))} /></span></div>
        <div class="row wrap-narrow"><span class="row-icon"><${Icon} name="clock" /></span>
          <span class="row-label">Poll interval<small>How often changes from other apps are picked up</small></span>
          <span class="row-control"><${Seg} small label="Poll interval" value=${s.poll_interval_ms} onChange=${(x) => setS({ ...s, poll_interval_ms: x })}
            options=${[500, 1000, 2000, 5000].map((x) => ({ value: x, label: x < 1000 ? x + 'ms' : x / 1000 + 's' }))} /></span></div>
        <div class="row"><span class="row-icon"><${Icon} name="server" /></span>
          <span class="row-label">Web UI<small>Only reachable from this computer</small></span>
          <span class="row-control mono faint">127.0.0.1:${s.http_port}</span></div>
        <div class="row"><span class="row-icon"><${Icon} name="file" /></span>
          <span class="row-label">Config file<small class="mono" style="word-break:break-all">${s.config_path}</small></span></div>
      </div>
    <//>

    <${Card} num="03" title="About">
      <dl class="kv">
        <dt>Camera</dt><dd>${st?.device ? st.device.model : '—'}</dd>
        <dt>Serial</dt><dd>${st?.device?.serial || '—'}</dd>
        <dt>Firmware</dt><dd>${st?.device?.firmware || '—'}</dd>
        <dt>Device id</dt><dd>${st?.device_id || '—'}</dd>
        <dt>Host</dt><dd>${st?.host || '—'}</dd>
      </dl>
      <div class="hint" style="margin-top:16px">
        <span class="kbd">←→↑↓</span> move <span class="kbd">⇧</span>+arrow nudge <span class="kbd">+ −</span> zoom
        <span class="kbd">0</span> center <span class="kbd">1–9</span> preset
      </div>
    <//>
  </div>`;
}

// ---------------------------------------------------------------- prototype switches
function DemoBar() {
  if (!api.demo) return null;
  const [cam, setCam] = useState(true);
  const [kc, setKc] = useState(false);
  const [str, setStr] = useState(api.demo.isStreaming());
  useEffect(() => api.subscribe((s) => setStr(!!s.streaming)), []);
  return html`<div class="demo-bar" style="position:fixed;right:12px;bottom:12px;z-index:40;display:flex;gap:10px;align-items:center;padding:8px 12px;
      background:var(--surface-2);border:1px dashed var(--line-strong);border-radius:12px;box-shadow:var(--shadow-2)">
    <span class="label">Prototype</span>
    <label class="hint" style="gap:6px">Camera <${Toggle} checked=${cam} label="Simulate camera" onChange=${(x) => { setCam(x); api.demo.setConnected(x); }} /></label>
    <label class="hint" style="gap:6px">Video app <${Toggle} checked=${str} label="Simulate streaming" onChange=${(x) => api.demo.setStreaming(x)} /></label>
    <label class="hint" style="gap:6px">No keychain <${Toggle} checked=${kc} label="Simulate keychain fallback" onChange=${(x) => { setKc(x); api.demo.setKeychainFallback(x); }} /></label>
  </div>`;
}

// ---------------------------------------------------------------- app
function App() {
  const [st, setSt] = useState(null);
  const [online, setOnline] = useState(true);
  const [view, setView] = useState(() => (location.hash === '#config' ? 'settings' : 'control'));
  useEffect(() => api.subscribe(setSt, setOnline), []);
  useEffect(() => { history.replaceState(null, '', view === 'settings' ? '#config' : '#'); }, [view]);
  return html`
    <${Header} st=${st} online=${online} view=${view} setView=${setView} />
    <main class=${'main view-' + view}>
      ${view === 'control' ? html`<${ControlView} st=${st} online=${online} />` : html`<${SettingsView} st=${st} />`}
    </main>
    <${Toasts} />
    <${DemoBar} />`;
}

render(html`<${App} />`, document.getElementById('app'));
