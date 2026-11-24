// In-memory mock of the linkctl API (same interface as api.js).
// Mirrors internal/camera/specs.go DefaultSpecs and the service semantics.

const SPECS = [
  { key: 'brightness', label: 'Brightness', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50 },
  { key: 'contrast', label: 'Contrast', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50 },
  { key: 'saturation', label: 'Saturation', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50 },
  { key: 'sharpness', label: 'Sharpness', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50 },
  { key: 'hue', label: 'Hue', group: 'picture', kind: 'range', min: -15, max: 15, step: 1, default: 0 },
  { key: 'exposure_compensation', label: 'Exposure compensation', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50, vendor: true },
  { key: 'white_balance_auto', label: 'Auto white balance', group: 'picture', kind: 'bool', min: 0, max: 1, step: 1, default: 1 },
  { key: 'white_balance_temperature', label: 'Color temperature', group: 'picture', kind: 'range', min: 2000, max: 10000, step: 100, default: 6400, unit: 'K', auto_key: 'white_balance_auto' },
  { key: 'anti_flicker', label: 'Anti-flicker', group: 'picture', kind: 'enum', min: 0, max: 3, step: 1, default: 3, options: ['Off', '50 Hz', '60 Hz', 'Auto'] },
  { key: 'hdr', label: 'HDR', group: 'picture', kind: 'bool', min: 0, max: 1, step: 1, default: 0, vendor: true },
  { key: 'mirror', label: 'Mirror', group: 'picture', kind: 'bool', min: 0, max: 1, step: 1, default: 0, vendor: true },
  { key: 'focus_auto', label: 'Autofocus', group: 'picture', kind: 'bool', min: 0, max: 1, step: 1, default: 1 },
  { key: 'focus', label: 'Focus', group: 'picture', kind: 'range', min: 0, max: 100, step: 1, default: 50, auto_key: 'focus_auto' },
  { key: 'ai_mode', label: 'Mode', group: 'ai', kind: 'enum', min: 0, max: 2, step: 1, default: 0, options: ['Normal', 'Tracking', 'DeskView'], vendor: true },
  { key: 'composition', label: 'Composition', group: 'ai', kind: 'enum', min: 0, max: 3, step: 1, default: 0, options: ['Off', 'Head', 'Upper body', 'Full body'], vendor: true },
  { key: 'tracking_speed', label: 'Tracking speed', group: 'ai', kind: 'enum', min: 0, max: 2, step: 1, default: 1, options: ['Slow', 'Normal', 'Fast'], vendor: true },
  { key: 'pan', label: 'Pan', group: 'position', kind: 'range', min: -145, max: 145, step: 1, default: 0, unit: '°' },
  { key: 'tilt', label: 'Tilt', group: 'position', kind: 'range', min: -90, max: 100, step: 1, default: 0, unit: '°' },
  { key: 'zoom', label: 'Zoom', group: 'position', kind: 'range', min: 100, max: 400, step: 1, default: 100, unit: '%' },
];
const RATES = { pan: 60, tilt: 45, zoom: 150 };
const spec = (k) => SPECS.find((s) => s.key === k);
const clamp = (s, v) => Math.max(s.min, Math.min(s.max, Math.round(v)));
const delay = (ms = 60) => new Promise((r) => setTimeout(r, ms));
const fail = (msg) => { throw new Error(msg); };
let idSeq = 100;

export function createMockApi() {
  const values = Object.fromEntries(SPECS.map((s) => [s.key, s.default]));
  Object.assign(values, { white_balance_temperature: 3600, focus: 94, exposure_compensation: 55, anti_flicker: 1 });
  let connected = true;
  let streaming = false;
  let parkTimer = null;
  const REST = { pan: 0, tilt: 0, zoom: 100 };
  let presets = [
    { id: 'p1', name: 'Desk', groups: ['position'], values: { pan: 0, tilt: -10, zoom: 130 } },
    { id: 'p2', name: 'Whiteboard', groups: ['position'], values: { pan: 62, tilt: 4, zoom: 180 } },
    { id: 'p3', name: 'Evening', groups: ['picture', 'position'], values: { ...pick(['brightness', 'contrast', 'saturation', 'sharpness', 'hue', 'exposure_compensation', 'white_balance_auto', 'white_balance_temperature', 'anti_flicker', 'hdr', 'mirror', 'focus_auto', 'focus']), brightness: 62, white_balance_auto: 0, white_balance_temperature: 3200, pan: 0, tilt: -6, zoom: 110 } },
  ];
  let defaultPreset = 'p1';
  let active = '';
  let settings = {
    http_port: 7360, poll_interval_ms: 1000, start_on_login: true,
    config_path: 'C:\\Users\\you\\AppData\\Roaming\\linkctl\\config.json',
    mqtt: {
      enabled: true, host: 'emqx.local', port: 1883, tls: 'off', username: 'linkctl',
      password_set: true, password_in_file: false, device_name: 'Insta360 Link 2',
      base_topic: 'insta360link', discovery_prefix: 'homeassistant',
    },
  };
  let mqtt = { state: 'connected', broker: 'mqtt://emqx.local:1883', device_id: '17b0fake0000000000001', topic: 'insta360link/17b0fake0000000000001' };
  let motion = null; // {vel, pos, deadline}
  const listeners = new Set();

  function pick(keys) { return Object.fromEntries(keys.map((k) => [k, values?.[k] ?? spec(k).default])); }

  function matches(p) {
    return Object.entries(p.values).every(([k, v]) => {
      const s = spec(k);
      if (s.auto_key && p.values[s.auto_key] === 1) return true;
      const tol = k === 'pan' || k === 'tilt' ? 1 : 0;
      return Math.abs(values[k] - v) <= tol;
    });
  }

  function state() {
    if (active && !matches(presets.find((p) => p.id === active) || { values: { x: 0 } })) active = '';
    return JSON.parse(JSON.stringify({
      connected,
      device: connected ? { model: 'Insta360 Link 2', name: 'Insta360 Link 2', serial: '17B0FAKE0000000000001', firmware: 'v3.3.0.8_build3', product_id: 0x4c04 } : null,
      device_id: '17b0fake0000000000001',
      host: 'DESKTOP-STUDIO',
      controls: SPECS.map((s) => ({ ...s, value: connected ? values[s.key] : null })),
      values: connected ? values : {},
      presets, default_preset: defaultPreset, active_preset: active,
      mqtt: settings.mqtt.enabled ? mqtt : { state: 'disabled' },
      moving: !!motion,
      streaming: connected && streaming,
    }));
  }
  let pending = false;
  function emit() {
    if (pending) return;
    pending = true;
    queueMicrotask(() => { pending = false; const s = state(); listeners.forEach((fn) => fn(s)); });
  }
  const needCam = () => { if (!connected) fail('no camera connected'); };

  function set(obj) {
    needCam();
    const keys = Object.keys(obj).sort((a, b) => (SPECS.some((s) => s.auto_key === a) ? -1 : 0) - (SPECS.some((s) => s.auto_key === b) ? -1 : 0));
    for (const k of keys) {
      const s = spec(k);
      if (!s) fail(`unknown control "${k}"`);
      values[k] = clamp(s, obj[k]);
      if (s.auto_key) values[s.auto_key] = 0;
    }
    if (keys.some((k) => RATES[k])) schedulePark();
    emit();
  }

  // The Link 2 parks its gimbal ~2s after a position write while no app streams video.
  function schedulePark() {
    clearTimeout(parkTimer);
    if (streaming) return;
    parkTimer = setTimeout(() => { if (!streaming && !motion && connected) { Object.assign(values, REST); emit(); } }, 2000);
  }
  function setStreaming(v) {
    if (streaming === v) return;
    streaming = v;
    if (v && defaultPreset) {
      const p = presets.find((x) => x.id === defaultPreset);
      if (p) { const want = { ...p.values }; SPECS.forEach((s) => { if (s.auto_key && want[s.auto_key] === 1) delete want[s.key]; }); set(want); active = p.id; }
    }
    if (!v) schedulePark();
    emit();
  }

  // Simulated auto white balance / autofocus drift, like the real camera.
  setInterval(() => {
    if (!connected) return;
    let ch = false;
    if (values.white_balance_auto) { values.white_balance_temperature = clamp(spec('white_balance_temperature'), 3500 + Math.round(Math.random() * 3) * 50); ch = true; }
    if (values.focus_auto) { values.focus = clamp(spec('focus'), 92 + Math.round(Math.random() * 4)); ch = true; }
    if (ch) emit();
  }, 1500);

  setInterval(() => {
    if (!motion) return;
    if (Date.now() > motion.deadline || !connected) { motion = null; schedulePark(); emit(); return; }
    for (const [k, v] of Object.entries(motion.vel)) {
      const s = spec(k);
      motion.pos[k] = Math.max(s.min, Math.min(s.max, motion.pos[k] + v * RATES[k] * 0.05));
      values[k] = Math.round(motion.pos[k]);
    }
    emit();
  }, 50);

  return {
    mock: true,
    async getState() { await delay(); return state(); },
    subscribe(onState, onConn) {
      listeners.add(onState);
      onConn && onConn(true);
      onState(state());
      return () => listeners.delete(onState);
    },
    async setControls(obj) { await delay(20); set(obj); },
    async resetControls(keys) {
      await delay(); needCam();
      const want = {};
      SPECS.forEach((s) => { if (!keys || keys.includes(s.key)) want[s.key] = s.default; });
      SPECS.forEach((s) => { if (s.auto_key && want[s.auto_key] === 1) delete want[s.key]; });
      set(want);
    },
    async center() { await delay(); motion = null; set({ pan: 0, tilt: 0, zoom: 100 }); },
    async nudge(d) {
      await delay(); needCam();
      const w = {};
      for (const [k, v] of Object.entries(d)) { if (!RATES[k]) fail(`nudge supports pan, tilt and zoom, not "${k}"`); w[k] = values[k] + v; }
      set(w);
    },
    async move(v) {
      needCam();
      const vel = {};
      for (const [k, x] of Object.entries(v)) if (RATES[k] && x) vel[k] = Math.max(-1, Math.min(1, x));
      if (!Object.keys(vel).length) { motion = null; emit(); return; }
      const pos = motion ? motion.pos : {};
      for (const k of Object.keys(vel)) if (pos[k] === undefined) pos[k] = values[k];
      motion = { vel, pos, deadline: Date.now() + (v.ttl_ms || 500) };
      emit();
    },
    async savePreset(name, groups) {
      await delay(); needCam();
      name = (name || '').trim();
      if (!name) fail('preset name cannot be empty');
      if (!groups || !groups.length) fail('select at least one group (picture, position)');
      if (presets.some((p) => p.name.toLowerCase() === name.toLowerCase())) fail(`a preset named "${name}" already exists`);
      const p = { id: 'p' + idSeq++, name, groups, values: pick(SPECS.filter((s) => groups.includes(s.group)).map((s) => s.key)) };
      presets = [...presets, p]; active = p.id; emit(); return p;
    },
    async updatePreset(id, patch) {
      await delay();
      const p = presets.find((x) => x.id === id) || fail('preset not found');
      if (patch.name !== undefined) {
        const n = patch.name.trim();
        if (!n) fail('preset name cannot be empty');
        if (presets.some((x) => x.id !== id && x.name.toLowerCase() === n.toLowerCase())) fail(`a preset named "${n}" already exists`);
        p.name = n;
      }
      if (patch.groups) { p.groups = patch.groups; patch.recapture = true; }
      if (patch.recapture) { needCam(); p.values = pick(SPECS.filter((s) => p.groups.includes(s.group)).map((s) => s.key)); }
      presets = [...presets]; emit();
    },
    async deletePreset(id) {
      await delay();
      presets = presets.filter((p) => p.id !== id);
      if (defaultPreset === id) defaultPreset = '';
      if (active === id) active = '';
      emit();
    },
    async applyPreset(id) {
      await delay();
      const p = presets.find((x) => x.id === id) || fail('preset not found');
      const want = { ...p.values };
      SPECS.forEach((s) => { if (s.auto_key && want[s.auto_key] === 1) delete want[s.key]; });
      motion = null; set(want); active = p.id; emit();
    },
    async reorderPresets(ids) { await delay(); presets = ids.map((id) => presets.find((p) => p.id === id)); emit(); },
    async setDefaultPreset(id) { await delay(); defaultPreset = id || ''; emit(); },
    async getSettings() { await delay(); return JSON.parse(JSON.stringify(settings)); },
    async putSettings(s) {
      await delay(200);
      const m = { ...settings.mqtt, ...(s.mqtt || {}) };
      if (m.password !== undefined) { m.password_set = m.password !== ''; delete m.password; }
      settings = { ...settings, ...s, mqtt: m };
      if (m.enabled) {
        mqtt = { state: 'connecting', broker: `${m.tls === 'off' ? 'mqtt' : 'tls'}://${m.host}:${m.port}` };
        emit();
        setTimeout(() => {
          mqtt = /fail|bad/.test(m.host)
            ? { ...mqtt, state: 'error', error: `dial tcp ${m.host}:${m.port}: connection refused` }
            : { ...mqtt, state: 'connected', device_id: '17b0fake0000000000001', topic: `${m.base_topic}/17b0fake0000000000001` };
          emit();
        }, 1200);
      }
      emit();
      return JSON.parse(JSON.stringify(settings));
    },
    async testMqtt(m) {
      await delay(900);
      if (!m.host) return { ok: false, error: 'broker host is empty' };
      if (/fail|bad/.test(m.host)) return { ok: false, error: `dial tcp ${m.host}:${m.port}: connection refused` };
      return { ok: true };
    },
    // Prototype-only switches.
    demo: {
      setConnected(v) { connected = v; if (!v) { motion = null; active = ''; } emit(); },
      setKeychainFallback(v) { settings.mqtt.password_in_file = v; emit(); },
      isConnected: () => connected,
      setStreaming,
      isStreaming: () => streaming,
    },
  };
}
