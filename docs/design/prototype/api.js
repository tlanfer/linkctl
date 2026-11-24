// Real linkctl API client (fetch + EventSource). mock.js implements the same interface.
async function call(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* non-JSON */ }
  if (!res.ok) throw new Error((data && data.error) || text || `${res.status} ${res.statusText}`);
  return data;
}

export function createApi() {
  return {
    getState: () => call('GET', '/api/v1/state'),
    subscribe(onState, onConn) {
      const es = new EventSource('/api/v1/events');
      es.addEventListener('state', (e) => onState(JSON.parse(e.data)));
      es.onopen = () => onConn && onConn(true);
      es.onerror = () => onConn && onConn(false);
      return () => es.close();
    },
    setControls: (values) => call('PATCH', '/api/v1/controls', values),
    resetControls: (keys) => call('POST', '/api/v1/controls/reset', keys ? { keys } : {}),
    center: () => call('POST', '/api/v1/ptz/center', {}),
    nudge: (d) => call('POST', '/api/v1/ptz/nudge', d),
    move: (v) => call('POST', '/api/v1/ptz/move', v),
    savePreset: (name, groups) => call('POST', '/api/v1/presets', { name, groups }),
    updatePreset: (id, patch) => call('PUT', `/api/v1/presets/${encodeURIComponent(id)}`, patch),
    deletePreset: (id) => call('DELETE', `/api/v1/presets/${encodeURIComponent(id)}`),
    applyPreset: (id) => call('POST', `/api/v1/presets/${encodeURIComponent(id)}/apply`, {}),
    reorderPresets: (ids) => call('PUT', '/api/v1/presets/order', { ids }),
    setDefaultPreset: (id) => call('PUT', '/api/v1/presets/default', { id }),
    getSettings: () => call('GET', '/api/v1/settings'),
    putSettings: (s) => call('PUT', '/api/v1/settings', s),
    testMqtt: (m) => call('POST', '/api/v1/mqtt/test', m),
  };
}
