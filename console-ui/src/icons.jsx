import React from 'react';

// ─── Inline SVG icons (line, 1.7 stroke) ────────────────────────
export const Svg = ({ children, size = 18, stroke = 1.7, style, fill = 'none' }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill={fill} stroke="currentColor"
       strokeWidth={stroke} strokeLinecap="round" strokeLinejoin="round" style={style}>
    {children}
  </svg>
);

export const ICONS = {
  // System
  dashboard:   (p) => <Svg {...p}><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></Svg>,
  bell:        (p) => <Svg {...p}><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10 21a2 2 0 0 0 4 0"/></Svg>,
  wrench:      (p) => <Svg {...p}><path d="M14.7 6.3a4 4 0 0 0 5 5L21 13l-8 8a3 3 0 0 1-4.2-4.2l8-8z"/><path d="m6 14-3 3a2 2 0 1 0 2.8 2.8L9 17"/></Svg>,
  gear:        (p) => <Svg {...p}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1A1.7 1.7 0 0 0 4.6 9a1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9c.3.6.9 1 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"/></Svg>,
  lock:        (p) => <Svg {...p}><rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/></Svg>,
  shield:      (p) => <Svg {...p}><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z"/></Svg>,
  apps:        (p) => <Svg {...p}><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></Svg>,
  home:        (p) => <Svg {...p}><path d="m3 11 9-8 9 8v9a2 2 0 0 1-2 2h-4v-7h-6v7H5a2 2 0 0 1-2-2Z"/></Svg>,

  // App glyphs
  eye:         (p) => <Svg {...p}><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></Svg>,
  ruler:       (p) => <Svg {...p}><path d="M21.3 7.9 16.1 2.7a1.4 1.4 0 0 0-2 0L2.7 14.1a1.4 1.4 0 0 0 0 2l5.2 5.2a1.4 1.4 0 0 0 2 0L21.3 9.9a1.4 1.4 0 0 0 0-2Z"/><path d="m7.5 10.5 2 2"/><path d="m10.5 7.5 2 2"/><path d="m13.5 4.5 2 2"/><path d="m4.5 13.5 2 2"/></Svg>,
  database:    (p) => <Svg {...p}><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v6c0 1.7 4 3 9 3s9-1.3 9-3V5"/><path d="M3 11v6c0 1.7 4 3 9 3s9-1.3 9-3v-6"/></Svg>,
  zap:         (p) => <Svg {...p}><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8Z"/></Svg>,

  // Dev environment glyphs
  code:        (p) => <Svg {...p}><path d="m8 6-6 6 6 6"/><path d="m16 6 6 6-6 6"/><path d="m14 4-4 16"/></Svg>,
  jupyter:     (p) => <Svg {...p}><circle cx="8" cy="6" r="1.4" fill="currentColor"/><circle cx="16" cy="18" r="1.4" fill="currentColor"/><path d="M5 9c2 4 6 7 11 7"/><path d="M19 15c-2-4-6-7-11-7"/></Svg>,
  vllm:        (p) => <Svg {...p}><path d="M5 4v8a7 7 0 1 0 14 0V4"/><path d="M9 4v6a3 3 0 0 0 6 0V4"/></Svg>,
  comfy:       (p) => <Svg {...p}><rect x="3" y="3" width="6" height="5" rx="1"/><rect x="15" y="3" width="6" height="5" rx="1"/><rect x="9" y="16" width="6" height="5" rx="1"/><path d="M9 5.5h6"/><path d="M6 8v5l6 3"/><path d="M18 8v5l-6 3"/></Svg>,
  palette:     (p) => <Svg {...p}><circle cx="12" cy="12" r="9"/><circle cx="7"  cy="11" r="1.2" fill="currentColor"/><circle cx="11" cy="7"  r="1.2" fill="currentColor"/><circle cx="16" cy="9"  r="1.2" fill="currentColor"/><path d="M12 21a3 3 0 0 1 0-6 2 2 0 0 0 2-2 3 3 0 0 1 3-3"/></Svg>,
  folder:      (p) => <Svg {...p}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></Svg>,
  port:        (p) => <Svg {...p}><rect x="3" y="9" width="18" height="10" rx="2"/><circle cx="7"  cy="14" r="0.8" fill="currentColor"/><circle cx="11" cy="14" r="0.8" fill="currentColor"/><circle cx="15" cy="14" r="0.8" fill="currentColor"/><path d="M8 9V5h8v4"/></Svg>,
  brain:       (p) => <Svg {...p}><path d="M8 4a3 3 0 0 0-3 3 3 3 0 0 0-2 3 3 3 0 0 0 1 2 3 3 0 0 0 1 4 3 3 0 0 0 5 2c1 0 2-1 2-2"/><path d="M16 4a3 3 0 0 1 3 3 3 3 0 0 1 2 3 3 3 0 0 1-1 2 3 3 0 0 1-1 4 3 3 0 0 1-5 2c-1 0-2-1-2-2"/><path d="M12 4v14"/></Svg>,

  // AI app glyphs
  maxkb:       (p) => <Svg {...p}><path d="M4 4v15a2 2 0 0 0 2 2h13"/><path d="M8 7h10"/><path d="M8 11h6"/><rect x="13" y="13" width="8" height="6" rx="1.5"/><path d="m15.5 15.5 1.5 1.5 2-2"/></Svg>,
  ollama:      (p) => <Svg {...p}><path d="M12 3a6 6 0 0 0-6 6c0 2 1 3 1 5v3a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2v-3c0-2 1-3 1-5a6 6 0 0 0-6-6Z"/><circle cx="10" cy="10" r="0.6" fill="currentColor"/><circle cx="14" cy="10" r="0.6" fill="currentColor"/><path d="M10 16h4"/></Svg>,
  openclaw:    (p) => <Svg {...p}><circle cx="12" cy="9" r="4"/><path d="M8 9c-2 0-4 1.5-4 4M16 9c2 0 4 1.5 4 4"/><path d="M9 13v3a3 3 0 0 0 6 0v-3"/><path d="m9 6 1 1M14 7l1-1"/></Svg>,
  openwebui:   (p) => <Svg {...p}><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 11h2M14 11h2"/><path d="m12 17-3 4h6Z"/></Svg>,
  hermes:      (p) => <Svg {...p}><path d="M12 3 4 19h16Z"/><circle cx="12" cy="13" r="2.5"/></Svg>,
  sparkle:     (p) => <Svg {...p}><path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M6 18l2.5-2.5M15.5 8.5 18 6"/></Svg>,
  store:       (p) => <Svg {...p}><path d="M3 8 5 4h14l2 4"/><path d="M3 8v11a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1V8"/><path d="M3 8c0 2 1.5 3 3 3s3-1 3-3c0 2 1.5 3 3 3s3-1 3-3c0 2 1.5 3 3 3s3-1 3-3"/><path d="M9 20v-6h6v6"/></Svg>,
  tag:         (p) => <Svg {...p}><path d="M20 12 12 20l-9-9V3h8Z"/><circle cx="7.5" cy="7.5" r="1.2" fill="currentColor"/></Svg>,
  rocket:      (p) => <Svg {...p}><path d="M14 4.5C18 5 19 9 19 13l-4 1-4 4-3-3 4-4 1-4c0-2 1-2 2-2.5Z"/><path d="m12 14-3 3a3 3 0 1 0 4 3"/><path d="M9 11 5 9l2-2 4 1"/></Svg>,
  filter:      (p) => <Svg {...p}><path d="M3 5h18l-7 9v6l-4-2v-4Z"/></Svg>,
  send:        (p) => <Svg {...p}><path d="m3 11 18-8-8 18-2-8Z"/></Svg>,
  plus:        (p) => <Svg {...p}><path d="M12 5v14M5 12h14"/></Svg>,
  copy:        (p) => <Svg {...p}><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a1 1 0 0 1 1-1h10"/></Svg>,
  book:        (p) => <Svg {...p}><path d="M4 4v15a2 2 0 0 0 2 2h13V3H6a2 2 0 0 0-2 2Z"/><path d="M19 17H6a2 2 0 0 0-2 2"/></Svg>,
  message:     (p) => <Svg {...p}><path d="M21 12a8 8 0 0 1-13 6.3L3 20l1.7-5A8 8 0 1 1 21 12Z"/></Svg>,
  mic:         (p) => <Svg {...p}><rect x="9" y="3" width="6" height="13" rx="3"/><path d="M5 11a7 7 0 0 0 14 0"/><path d="M12 18v3"/></Svg>,
  attach:      (p) => <Svg {...p}><path d="m21 11-9 9a5 5 0 0 1-7-7l9-9a3.5 3.5 0 0 1 5 5l-9 9a2 2 0 0 1-3-3l8-8"/></Svg>,
  chevLeft:    (p) => <Svg {...p}><path d="m15 6-6 6 6 6"/></Svg>,
  star:        (p) => <Svg {...p}><path d="M12 2 15 9l7 .6-5.3 4.6 1.6 6.8L12 17.3 5.7 21l1.6-6.8L2 9.6 9 9z"/></Svg>,
  external:    (p) => <Svg {...p}><path d="M15 3h6v6"/><path d="M21 3 10 14"/><path d="M21 14v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h6"/></Svg>,
  thumbs:      (p) => <Svg {...p}><path d="M7 22V10l5-7a2 2 0 0 1 2 2v6h6a2 2 0 0 1 2 2l-2 7a2 2 0 0 1-2 1H7Z"/><path d="M7 22H3V10h4"/></Svg>,
  globe:       (p) => <Svg {...p}><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></Svg>,
  flame:       (p) => <Svg {...p}><path d="M14 4c0 4-4 5-4 9a2 2 0 1 0 4 0 4 4 0 0 1 4-4c0 4-4 6-4 9a5 5 0 1 1-10 0c0-6 6-7 4-14 4 0 6 4 6 0Z"/></Svg>,

  // UI
  x:           (p) => <Svg {...p}><path d="M18 6 6 18M6 6l12 12"/></Svg>,
  minus:       (p) => <Svg {...p}><path d="M5 12h14"/></Svg>,
  maximize:    (p) => <Svg {...p}><rect x="4" y="4" width="16" height="16" rx="2"/></Svg>,
  restore:     (p) => <Svg {...p}><rect x="7" y="7" width="13" height="13" rx="2"/><path d="M4 16V5a1 1 0 0 1 1-1h11"/></Svg>,
  cpu:         (p) => <Svg {...p}><rect x="5" y="5" width="14" height="14" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M1 9h3M1 15h3M20 9h3M20 15h3"/></Svg>,
  hardDrive:   (p) => <Svg {...p}><rect x="3" y="13" width="18" height="8" rx="2"/><path d="M3 13 6 4a2 2 0 0 1 2-1h8a2 2 0 0 1 2 1l3 9"/><circle cx="7" cy="17" r="0.6" fill="currentColor"/></Svg>,
  memory:      (p) => <Svg {...p}><rect x="2" y="7" width="20" height="10" rx="2"/><path d="M6 11v2M10 11v2M14 11v2M18 11v2"/><path d="M2 17v2M22 17v2"/></Svg>,
  thermo:      (p) => <Svg {...p}><path d="M14 14.8V4a2 2 0 1 0-4 0v10.8a4 4 0 1 0 4 0Z"/></Svg>,
  bolt:        (p) => <Svg {...p}><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8Z"/></Svg>,
  refresh:     (p) => <Svg {...p}><path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15 6.7L3 16"/><path d="M3 21v-5h5"/></Svg>,
  stop:        (p) => <Svg {...p}><rect x="6" y="6" width="12" height="12" rx="1.5"/></Svg>,
  pause:       (p) => <Svg {...p}><path d="M8 5v14M16 5v14"/></Svg>,
  play:        (p) => <Svg {...p}><path d="M6 4v16l14-8Z"/></Svg>,
  chevDown:    (p) => <Svg {...p}><path d="m6 9 6 6 6-6"/></Svg>,
  chevRight:   (p) => <Svg {...p}><path d="m9 6 6 6-6 6"/></Svg>,
  cloud:       (p) => <Svg {...p}><path d="M17.5 19a4.5 4.5 0 0 0 .5-9 6 6 0 0 0-11.6 1.4A4 4 0 0 0 6.5 19Z"/></Svg>,
  cloudOff:    (p) => <Svg {...p}><path d="M2 2 22 22"/><path d="M17.5 19a4.5 4.5 0 0 0 .5-9 6 6 0 0 0-9.6-2.4"/><path d="M6.5 19a4 4 0 0 1-.6-7.6"/></Svg>,
  check:       (p) => <Svg {...p}><path d="m5 12 5 5 9-11"/></Svg>,
  alertTri:    (p) => <Svg {...p}><path d="m10.3 3.9-8 14a2 2 0 0 0 1.7 3h16a2 2 0 0 0 1.7-3l-8-14a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4"/><circle cx="12" cy="17" r="0.6" fill="currentColor"/></Svg>,
  info:        (p) => <Svg {...p}><circle cx="12" cy="12" r="9"/><path d="M12 16v-4"/><circle cx="12" cy="8.5" r="0.6" fill="currentColor"/></Svg>,
  search:      (p) => <Svg {...p}><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></Svg>,
  user:        (p) => <Svg {...p}><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></Svg>,
  terminal:    (p) => <Svg {...p}><path d="m4 7 5 5-5 5"/><path d="M12 19h8"/></Svg>,
  network:     (p) => <Svg {...p}><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></Svg>,
  download:    (p) => <Svg {...p}><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></Svg>,
  expand:      (p) => <Svg {...p}><path d="M3 9V3h6M21 9V3h-6M3 15v6h6M21 15v6h-6"/></Svg>,
  dot:         (p) => <Svg {...p}><circle cx="12" cy="12" r="4" fill="currentColor"/></Svg>,
  arrowUp:     (p) => <Svg {...p}><path d="M12 19V5M5 12l7-7 7 7"/></Svg>,
  arrowDown:   (p) => <Svg {...p}><path d="M12 5v14M19 12l-7 7-7-7"/></Svg>,
  badge:       (p) => <Svg {...p}><path d="M12 2 4 6v6c0 5 3.5 8.5 8 10 4.5-1.5 8-5 8-10V6Z"/></Svg>,
  history:     (p) => <Svg {...p}><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/></Svg>,
  power:       (p) => <Svg {...p}><path d="M12 3v9"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/></Svg>,
  qrcode:      (p) => <Svg {...p}><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3M20 14v3M14 20h3M20 20h1"/></Svg>,

  // Additional icons
  calendar:    (p) => <Svg {...p}><rect x="3" y="4" width="18" height="17" rx="2"/><path d="M16 2v4M8 2v4M3 9h18"/></Svg>,
  clock:       (p) => <Svg {...p}><circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/></Svg>,
  upload:      (p) => <Svg {...p}><path d="M12 15V3"/><path d="m7 8 5-5 5 5"/><path d="M5 21h14"/></Svg>,
  trash:       (p) => <Svg {...p}><path d="M4 7h16"/><path d="M6 7v12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V7"/><path d="M9 7V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v3"/></Svg>,
  edit:        (p) => <Svg {...p}><path d="M17 3a2.8 2.8 0 1 1 4 4L8 20l-5 1 1-5Z"/></Svg>,
  file:        (p) => <Svg {...p}><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6"/></Svg>,
  image:       (p) => <Svg {...p}><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="m21 15-5-5L5 21"/></Svg>,
  wifi:        (p) => <Svg {...p}><path d="M5 12.6a14 14 0 0 1 14 0"/><path d="M8.5 16.1a7 7 0 0 1 7 0"/><circle cx="12" cy="20" r="0.6" fill="currentColor"/></Svg>,
  log:         (p) => <Svg {...p}><path d="M4 6h16M4 10h16M4 14h10M4 18h6"/></Svg>,
  key:         (p) => <Svg {...p}><circle cx="8" cy="15" r="4"/><path d="m14.7 7.3 4-4 2 2-4 4M14.7 7.3l-3.4 3.4"/></Svg>,
  chart:       (p) => <Svg {...p}><path d="M3 21 9 9l4 6 4-8 4 6"/></Svg>,
  gpu:         (p) => <Svg {...p}><rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10h2v4H6zM10 10h2v4h-2zM14 10h2v4h-2z"/><path d="M6 6V4M10 6V4M14 6V4M18 6V4M6 18v2M10 18v2M14 18v2M18 18v2"/></Svg>,
  layers:      (p) => <Svg {...p}><path d="m12 2-9 5 9 5 9-5Z"/><path d="m3 12 9 5 9-5"/><path d="m3 17 9 5 9-5"/></Svg>,
  server:      (p) => <Svg {...p}><rect x="3" y="3" width="18" height="6" rx="2"/><rect x="3" y="15" width="18" height="6" rx="2"/><circle cx="7" cy="6" r="0.6" fill="currentColor"/><circle cx="7" cy="18" r="0.6" fill="currentColor"/></Svg>,
  link:        (p) => <Svg {...p}><path d="M10 14a3.5 3.5 0 0 0 5 0l3-3a3.5 3.5 0 0 0-5-5l-1 1"/><path d="M14 10a3.5 3.5 0 0 0-5 0l-3 3a3.5 3.5 0 0 0 5 5l1-1"/></Svg>,
  pin:         (p) => <Svg {...p}><path d="m15 4.5-4 7.5-1 1L4.5 9.5l1-1L13 4.5a2 2 0 0 1 2 0Z"/><path d="M9 15 4 20"/></Svg>,
  minio:       (p) => <Svg {...p}><ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6"/><path d="M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/></Svg>,
  swap:        (p) => <Svg {...p}><path d="m7 3-4 4 4 4"/><path d="M3 7h14M17 21l4-4-4-4"/><path d="M21 17H7"/></Svg>,
  trendUp:     (p) => <Svg {...p}><path d="m23 6-9.5 9.5-5-5L1 18"/><path d="M17 6h6v6"/></Svg>,
  logout:      (p) => <Svg {...p}><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/></Svg>,
  sidebar:     (p) => <Svg {...p}><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/></Svg>,
};

// ─── Atoms ──────────────────────────────────────────────────────
export const Icon = ({ name, size = 16, stroke = 1.7, style }) => {
  const C = ICONS[name];
  if (!C) return null;
  return <C size={size} stroke={stroke} style={style}/>;
};
