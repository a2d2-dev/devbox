// ─── Design tokens ──────────────────────────────────────────────
export const T = {
  // Font
  mono: "'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
  // Surface
  bg:        '#eef3fa',
  surface:   '#ffffff',
  surfaceAlt:'#f8fafc',
  border:    '#e2e8f0',
  borderSoft:'#eef1f5',

  // Text
  ink:       '#0f172a',
  ink2:      '#334155',
  ink3:      '#64748b',
  ink4:      '#94a3b8',

  // Brand & semantic
  blue:      '#2563eb',
  blueDeep:  '#1d4ed8',
  blueSoft:  '#eff4ff',
  cyan:      '#06b6d4',
  indigo:    '#6366f1',
  teal:      '#14b8a6',
  violet:    '#8b5cf6',
  green:     '#10b981',
  greenSoft: '#ecfdf5',
  amber:     '#f59e0b',
  amberSoft: '#fffbeb',
  red:       '#ef4444',
  redSoft:   '#fef2f2',
  slate:     '#475569',

  // Window chrome
  windowBg:  '#ffffff',
  titleBar:  '#f8fafc',

  // Semantic tokens only; components should reference T.* keys so dark mode can swap values later.
  space: [0, 4, 8, 12, 16, 20, 24, 32, 40, 48],
  radius: {
    xs: 4,
    sm: 6,
    md: 8,
    lg: 10,
    xl: 14,
    pill: 999,
  },
  shadow: {
    sm: '0 1px 2px rgba(15,23,42,0.08), 0 0 0 1px rgba(15,23,42,0.04)',
    md: '0 8px 24px -12px rgba(15,23,42,0.22), 0 0 0 1px rgba(15,23,42,0.06)',
    lg: '0 16px 42px -18px rgba(15,23,42,0.28), 0 0 0 1px rgba(15,23,42,0.07)',
    xl: '0 24px 60px -12px rgba(15,23,42,0.32), 0 0 0 1px rgba(15,23,42,0.08)',
  },
  type: {
    display: { fontSize: 28, lineHeight: 1.12, fontWeight: 800, letterSpacing: '-0.015em' },
    title: { fontSize: 22, lineHeight: 1.18, fontWeight: 750, letterSpacing: '-0.015em' },
    heading: { fontSize: 17, lineHeight: 1.25, fontWeight: 700, letterSpacing: '-0.01em' },
    body: { fontSize: 13, lineHeight: 1.5, fontWeight: 400, letterSpacing: 0 },
    caption: { fontSize: 11.5, lineHeight: 1.35, fontWeight: 500, letterSpacing: 0 },
    label: { fontSize: 10.5, lineHeight: 1.25, fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase' },
  },
  ease: 'cubic-bezier(0.2,0.7,0.2,1)',
  duration: {
    press: '0.1s',
    hover: '0.15s',
    fade: '0.2s',
  },
  viewport: {
    phoneMax: 639,
    tabletMin: 640,
    desktopMin: 1024,
    touchTarget: 44,
  },
};

export const BREAKPOINTS = {
  phone: 640,
  tablet: 1024,
};

/** Semantic color for resource usage: green < 70%, amber >= 70% */
export const statusColor = (pct) => pct >= 70 ? T.amber : T.green;
/** Soft variant for status badges */
export const statusColorSoft = (pct) => pct >= 70 ? T.amberSoft : T.greenSoft;
