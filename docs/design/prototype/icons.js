// Thin line icons: 24-unit viewBox, stroke 1.5, currentColor (docs/DESIGN.md §7).
import { html } from './preact.js';

const paths = {
  camera: 'M3 8.5A2.5 2.5 0 0 1 5.5 6h8A2.5 2.5 0 0 1 16 8.5v7a2.5 2.5 0 0 1-2.5 2.5h-8A2.5 2.5 0 0 1 3 15.5zM16 10.5l4.2-2.4a.5.5 0 0 1 .8.4v7a.5.5 0 0 1-.8.4L16 13.5',
  sliders: 'M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1M15 4v4M9 10v4M17 16v4',
  gear: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
  up: 'M12 19V5M6 11l6-6 6 6',
  down: 'M12 5v14M6 13l6 6 6-6',
  left: 'M19 12H5M11 6l-6 6 6 6',
  right: 'M5 12h14M13 6l6 6-6 6',
  center: 'M12 3v3M12 18v3M3 12h3M18 12h3M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  plus: 'M12 5v14M5 12h14',
  minus: 'M5 12h14',
  zoomIn: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M11 8v6M8 11h6',
  zoomOut: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4M8 11h6',
  reset: 'M4 12a8 8 0 1 0 2.4-5.7M4 4v4h4',
  more: 'M5 12h.01M12 12h.01M19 12h.01',
  star: 'M12 3.5l2.6 5.3 5.9.9-4.2 4.1 1 5.8-5.3-2.8-5.3 2.8 1-5.8-4.2-4.1 5.9-.9z',
  pencil: 'M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16zM13.5 6.5l4 4',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3',
  capture: 'M4 8V6a2 2 0 0 1 2-2h2M16 4h2a2 2 0 0 1 2 2v2M20 16v2a2 2 0 0 1-2 2h-2M8 20H6a2 2 0 0 1-2-2v-2M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  x: 'M6 6l12 12M18 6L6 18',
  check: 'M5 12.5l4.5 4.5L19 7.5',
  sun: 'M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4',
  contrast: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 3v18',
  drop: 'M12 3.5s6 6.3 6 10.5a6 6 0 0 1-12 0c0-4.2 6-10.5 6-10.5z',
  sharp: 'M4 20L20 4M4 14l10-10M10 20L20 10',
  hue: 'M12 21a9 9 0 1 1 9-9c0 2.5-2 3-3.5 3H15a2 2 0 0 0-1.5 3.3c.4.5.5 1 .5 1.4 0 .8-.9 1.3-2 1.3zM7.5 10.5h.01M10.5 7h.01M15 7.5h.01',
  aperture: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM14.3 3.3L9 12M20.7 9.5H9.8M17.7 18.9L12.2 9.5M9.7 20.7l5.5-9.5M3.3 14.5h10.9M6.3 5.1l5.5 9.4',
  hdr: 'M3 12h3M18 12h3M12 3v3M12 18v3M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM6 6l1.5 1.5M16.5 16.5L18 18',
  wave: 'M2 12c2-5 4-5 6 0s4 5 6 0 4-5 6 0 2 3 2 3',
  temp: 'M14 14.8V5a2 2 0 1 0-4 0v9.8a4 4 0 1 0 4 0zM12 9v7',
  wbAuto: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM8.5 15l3.5-7 3.5 7M9.7 12.6h4.6',
  focus: 'M3 8V5a2 2 0 0 1 2-2h3M16 3h3a2 2 0 0 1 2 2v3M21 16v3a2 2 0 0 1-2 2h-3M8 21H5a2 2 0 0 1-2-2v-3M12 14a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
  robot: 'M7 9h10a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-6a2 2 0 0 1 2-2zM12 5v4M12 4h.01M9.5 13.5h.01M14.5 13.5h.01M10 16.5h4M3 13v2M21 13v2',
  frame: 'M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3M12 11a2 2 0 1 0 0-4 2 2 0 0 0 0 4zM8.5 17a3.5 3.5 0 0 1 7 0',
  people: 'M9 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM3.5 19a5.5 5.5 0 0 1 11 0M16 5.5a3 3 0 0 1 0 5.5M17.5 14a5.5 5.5 0 0 1 3 5',
  target: 'M12 20a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM12 12h.01',
  speed: 'M4.5 16a8 8 0 1 1 15 0M12 13l4-4M12 13h.01',
  mirror: 'M12 3v18M8 7L3 12l5 5zM16 7l5 5-5 5z',
  broker: 'M5 12a7 7 0 0 1 14 0M8.5 12a3.5 3.5 0 0 1 7 0M12 12v8M9 20h6',
  server: 'M4 5h16v6H4zM4 13h16v6H4zM8 8h.01M8 16h.01',
  lock: 'M6 11h12v9H6zM8.5 11V8a3.5 3.5 0 0 1 7 0v3',
  user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4.5 20a7.5 7.5 0 0 1 15 0',
  tag: 'M3 12V4h8l9 9-8 8zM7.5 7.5h.01',
  topic: 'M4 7h16M4 12h10M4 17h7',
  home: 'M4 11l8-7 8 7M6 9.5V20h12V9.5',
  clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2',
  power: 'M12 3v8M6.4 6.4a8 8 0 1 0 11.2 0',
  info: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5M12 8h.01',
  file: 'M6 3h8l4 4v14H6zM14 3v4h4',
  eye: 'M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  bolt: 'M13 3L5 13.5h6L10 21l8-10.5h-6z',
  arrowsH: 'M4 12h16M8 8l-4 4 4 4M16 8l4 4-4 4',
};

export function Icon({ name, size }) {
  const d = paths[name];
  const s = size || undefined;
  return html`<svg viewBox="0 0 24 24" width=${s} height=${s} fill="none" stroke="currentColor"
    stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d=${d} /></svg>`;
}

// Brand mark: lens inside a dotted ring.
export function Logo() {
  const dots = [];
  for (let i = 0; i < 16; i++) {
    const a = (i / 16) * Math.PI * 2;
    dots.push(html`<circle cx=${12 + Math.cos(a) * 9} cy=${12 + Math.sin(a) * 9} r=${i === 12 ? 1.3 : 0.75}
      fill=${i === 12 ? 'var(--accent)' : 'var(--text-2)'} />`);
  }
  return html`<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">${dots}
    <circle cx="12" cy="12" r="4" fill="none" stroke="var(--text-1)" stroke-width="1.5" />
    <circle cx="12" cy="12" r="1.2" fill="var(--text-1)" /></svg>`;
}
