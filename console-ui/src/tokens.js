// ─── Design tokens ──────────────────────────────────────────────
// fnOS / 飞牛 (Semi Design) 视觉语言。品牌主色 #0066ff。
// Color tokens resolve through CSS variables so the existing `T.*` imports can
// switch themes without touching hundreds of inline style call sites.
const v = (name) => `var(--${name})`;

export const T = {
  // Font — 对齐 fnOS: PingFang SC / SF Pro，等宽保留给终端与数字
  sans: "'PingFang SC', 'SF Pro SC', 'SF Pro Text', 'Helvetica Neue', Helvetica, Arial, sans-serif",
  mono: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",

  // Surface — Semi bg 层级：bg-0 浅灰底，卡片纯白
  bg:        v('edge-bg'),
  surface:   v('edge-surface'),
  surfaceAlt:v('edge-surface-alt'),
  border:    v('edge-border'),
  borderSoft:v('edge-border-soft'),

  // Text — Semi grey-9 (#0b0b0c) 分级
  ink:       v('edge-ink'),
  ink2:      v('edge-ink-2'),
  ink3:      v('edge-ink-3'),
  ink4:      v('edge-ink-4'),

  // Brand & semantic — fnOS 品牌蓝 #0066ff
  blue:      v('edge-blue'),
  blueDeep:  v('edge-blue-deep'),
  blueSoft:  v('edge-blue-soft'),
  cyan:      v('edge-cyan'),
  indigo:    v('edge-indigo'),
  teal:      v('edge-teal'),
  violet:    v('edge-violet'),
  green:     v('edge-green'),
  greenSoft: v('edge-green-soft'),
  amber:     v('edge-amber'),
  amberSoft: v('edge-amber-soft'),
  red:       v('edge-red'),
  redSoft:   v('edge-red-soft'),
  slate:     v('edge-slate'),

  // Window chrome
  windowBg:  v('edge-window-bg'),
  titleBar:  v('edge-titlebar'),

  // Shared chrome / controls
  controlBg: v('edge-control-bg'),
  controlBgHover: v('edge-control-bg-hover'),
  overlayBg: v('edge-overlay-bg'),
  sidebarBg: v('edge-sidebar-bg'),
  sidebarFg: v('edge-sidebar-fg'),
  sidebarMuted: v('edge-sidebar-muted'),
  sidebarActiveBg: v('edge-sidebar-active-bg'),
  sidebarActiveFg: v('edge-sidebar-active-fg'),
  blueBorder: v('edge-blue-border'),
  redBorder: v('edge-red-border'),
  greenBorder: v('edge-green-border'),
  amberBorder: v('edge-amber-border'),
  codeBg: v('edge-code-bg'),
  desktopTopoBg: v('edge-desktop-topo-bg'),
  desktopTopoImage: v('edge-desktop-topo-image'),
  desktopPlainBg: v('edge-desktop-plain-bg'),

  // Semantic tokens only; components should reference T.* keys so dark mode can swap values later.
  space: [0, 4, 8, 12, 16, 20, 24, 32, 40, 48],
  radius: {
    xs: 3,    // Semi small
    sm: 6,    // Semi medium
    md: 8,    // Semi name
    lg: 12,   // Semi large
    xl: 16,   // fnOS floating panels
    pill: 999,
  },
  shadow: {
    sm: '0 1px 2px rgba(11,11,12,0.06), 0 0 0 1px rgba(11,11,12,0.05)',
    md: '0 8px 24px -12px rgba(11,11,12,0.18), 0 0 0 1px rgba(11,11,12,0.05)',
    lg: '0 16px 42px -18px rgba(11,11,12,0.22), 0 0 0 1px rgba(11,11,12,0.06)',
    xl: '0 24px 60px -12px rgba(11,11,12,0.26), 0 0 0 1px rgba(11,11,12,0.06)',
  },
  type: {
    display: { fontSize: 28, lineHeight: 1.12, fontWeight: 700, letterSpacing: '-0.015em' },
    title: { fontSize: 22, lineHeight: 1.18, fontWeight: 700, letterSpacing: '-0.01em' },
    heading: { fontSize: 17, lineHeight: 1.25, fontWeight: 600, letterSpacing: '-0.005em' },
    body: { fontSize: 14, lineHeight: 1.5, fontWeight: 400, letterSpacing: 0 },
    caption: { fontSize: 12, lineHeight: 1.35, fontWeight: 500, letterSpacing: 0 },
    label: { fontSize: 11, lineHeight: 1.25, fontWeight: 600, letterSpacing: '0.04em', textTransform: 'uppercase' },
  },
  ease: 'cubic-bezier(0.2,0.7,0.2,1)',
  duration: {
    press: '0.1s',
    hover: '0.15s',
    fade: '0.2s',
  },
};

/** Semantic color for resource usage: green < 70%, amber >= 70% */
export const statusColor = (pct) => pct >= 70 ? T.amber : T.green;
/** Soft variant for status badges */
export const statusColorSoft = (pct) => pct >= 70 ? T.amberSoft : T.greenSoft;

export function resolveThemePreference(preference, win = globalThis.window) {
  if (preference === 'dark' || preference === 'light') return preference;
  if (preference === 'system' && win?.matchMedia) {
    return win.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  return 'light';
}

export function applyThemePreference(preference, root = globalThis.document?.documentElement, win = globalThis.window) {
  const resolved = resolveThemePreference(preference, win);
  if (root) {
    root.dataset.theme = resolved;
    root.dataset.themePreference = preference || 'light';
    root.style.colorScheme = resolved;
  }
  return resolved;
}
