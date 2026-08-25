// ─── Design tokens ──────────────────────────────────────────────
// fnOS / 飞牛 (Semi Design) 视觉语言。品牌主色 #0066ff，纯白卡片，
// 深灰摄影感桌面背景，12px 圆角，PingFang SC 字体栈。
export const T = {
  // Font — 对齐 fnOS: PingFang SC / SF Pro，等宽保留给终端与数字
  sans: "'PingFang SC', 'SF Pro SC', 'SF Pro Text', 'Helvetica Neue', Helvetica, Arial, sans-serif",
  mono: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace",

  // Surface — Semi bg 层级：bg-0 浅灰底，卡片纯白
  bg:        '#f3f3f3',
  surface:   '#ffffff',
  surfaceAlt:'#fafafa',
  border:    '#e6e6e7',   // rgba(11,11,12,.10) 实色化
  borderSoft:'#f0f0f0',

  // Text — Semi grey-9 (#0b0b0c) 分级
  ink:       '#0b0b0c',
  ink2:      '#3d3d3e',   // ~75%
  ink3:      '#7f7f80',   // ~50%
  ink4:      '#b0b0b1',   // ~30%

  // Brand & semantic — fnOS 品牌蓝 #0066ff
  blue:      '#0066ff',
  blueDeep:  '#005eeb',
  blueSoft:  '#e6f4ff',
  cyan:      '#00b8d4',
  indigo:    '#3d5afe',
  teal:      '#009e8e',
  violet:    '#7c4dff',
  green:     '#009e61',
  greenSoft: '#e6f7ef',
  amber:     '#e06c00',
  amberSoft: '#fff7e8',
  red:       '#db382c',
  redSoft:   '#fdecea',
  slate:     '#4b4b4c',

  // Window chrome
  windowBg:  '#ffffff',
  titleBar:  '#fafafa',

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
