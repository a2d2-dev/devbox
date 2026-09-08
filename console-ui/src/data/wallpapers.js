// 壁纸注册表 —— App.jsx（桌面背景解析）与 AppearanceSettings.jsx（设置页缩略选择）
// 的单一事实源。新增内置壁纸只需在此加一条，两处消费自动同步。
//
// 两类壁纸：
//   - 程序化背景（CSS 类 / 内联样式）：fnos / grid / topo / plain（历史保留）
//   - 照片壁纸（photo）：NASA 公有素材 WebP，放 public/wallpapers/，全部深色调，
//     顶部 200px 亮度 ≤ 37.9/255，适配浅色玻璃顶栏 + 白色图标。
//
// photo 壁纸的桌面背景与缩略预览都用同一 CSS：
//   background: #1a1f27 兜底色 + url(基准图) center/cover。

// 照片壁纸公共背景构造：底色兜底 + cover 图，保证加载前/失败不白屏。
export function photoWallpaperStyle(src) {
  return {
    backgroundColor: '#1a1f27',
    backgroundImage: `url(${src})`,
    backgroundSize: 'cover',
    backgroundPosition: 'center',
    backgroundRepeat: 'no-repeat',
  };
}

// 内置壁纸清单。id 即持久化到偏好的 t.wallpaper 值。
// kind: 'programmatic' 走 className/style；'photo' 走 photoWallpaperStyle(src)。
export const WALLPAPERS = [
  { id: 'fnos',  kind: 'programmatic', label: '默认', desc: '深灰渐变 + 柔光斑', className: 'fnos-desktop-bg' },
  { id: 'aurora-earth-limb',      kind: 'photo', label: '极光地平线', desc: '南极光掠过地球边缘（NASA）', src: '/wallpapers/aurora-earth-limb.webp' },
  { id: 'milky-way-dust-core',    kind: 'photo', label: '银河核心',   desc: '银河中心尘埃辉光（NASA）',   src: '/wallpapers/milky-way-dust-core.webp' },
  { id: 'night-ocean-earth',      kind: 'photo', label: '夜海',       desc: '夜间地球暗面海洋（NASA）',   src: '/wallpapers/night-ocean-earth.webp' },
  { id: 'orbital-airglow-milkyway', kind: 'photo', label: '轨道气辉', desc: '气辉带 + 银河横贯（NASA）',  src: '/wallpapers/orbital-airglow-milkyway.webp' },
  { id: 'grid',  kind: 'programmatic', label: '网格', desc: '浅色网格纹理', className: 'edge-bg' },
  { id: 'topo',  kind: 'programmatic', label: '光晕', desc: '柔和光斑渐变' /* style 由消费方注入 token */ },
  { id: 'plain', kind: 'programmatic', label: '纯色', desc: '极简纯色底' /* style 由消费方注入 token */ },
];

export const WALLPAPER_BY_ID = Object.fromEntries(WALLPAPERS.map((w) => [w.id, w]));

// 解析桌面背景：返回 { className, style }。photo → cover 图；programmatic → 各自 CSS。
// topo/plain 的颜色来自 tokens（T.desktopTopoBg 等），由 App.jsx 传入 tokens 注入。
export function resolveWallpaper(id, tokens) {
  const wp = WALLPAPER_BY_ID[id] || WALLPAPER_BY_ID.fnos;
  if (wp.kind === 'photo') return { className: '', style: photoWallpaperStyle(wp.src) };
  if (wp.id === 'grid') return { className: 'edge-bg', style: {} };
  if (wp.id === 'topo') return { className: '', style: { background: tokens.desktopTopoBg, backgroundImage: tokens.desktopTopoImage } };
  if (wp.id === 'plain') return { className: '', style: { background: tokens.desktopPlainBg } };
  return { className: 'fnos-desktop-bg', style: {} };
}
