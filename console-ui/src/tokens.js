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
};

/** Semantic color for resource usage: green < 70%, amber >= 70% */
export const statusColor = (pct) => pct >= 70 ? T.amber : T.green;
/** Soft variant for status badges */
export const statusColorSoft = (pct) => pct >= 70 ? T.amberSoft : T.greenSoft;
