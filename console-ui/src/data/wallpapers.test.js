import { describe, it, expect } from 'vitest'
import { WALLPAPERS, WALLPAPER_BY_ID, resolveWallpaper, photoWallpaperStyle } from './wallpapers'

const TOKENS = { desktopTopoBg: '#topo', desktopTopoImage: 'topo-img', desktopPlainBg: '#plain' }

describe('wallpapers registry', () => {
  it('内置 4 张 NASA 照片壁纸 + 4 个程序化背景', () => {
    const photos = WALLPAPERS.filter((w) => w.kind === 'photo')
    expect(photos.map((w) => w.id)).toEqual([
      'aurora-earth-limb', 'milky-way-dust-core', 'night-ocean-earth', 'orbital-airglow-milkyway',
    ])
    photos.forEach((w) => expect(w.src).toMatch(/^\/wallpapers\/.+\.webp$/))
    expect(WALLPAPERS.filter((w) => w.kind === 'programmatic').map((w) => w.id))
      .toEqual(['fnos', 'grid', 'topo', 'plain'])
  })

  it('resolveWallpaper 对照片壁纸返回 cover 图样式（无 className）', () => {
    const r = resolveWallpaper('night-ocean-earth', TOKENS)
    expect(r.className).toBe('')
    expect(r.style.backgroundImage).toBe('url(/wallpapers/night-ocean-earth.webp)')
    expect(r.style.backgroundSize).toBe('cover')
    expect(r.style.backgroundColor).toBe('#1a1f27') // 兜底色，加载前不白屏
  })

  it('resolveWallpaper 对程序化背景返回对应 className/style', () => {
    expect(resolveWallpaper('fnos', TOKENS)).toEqual({ className: 'fnos-desktop-bg', style: {} })
    expect(resolveWallpaper('grid', TOKENS)).toEqual({ className: 'edge-bg', style: {} })
    expect(resolveWallpaper('topo', TOKENS).style.background).toBe('#topo')
    expect(resolveWallpaper('plain', TOKENS).style.background).toBe('#plain')
  })

  it('未知 id 回退到 fnos，不抛错', () => {
    expect(resolveWallpaper('nope', TOKENS)).toEqual({ className: 'fnos-desktop-bg', style: {} })
    expect(resolveWallpaper(undefined, TOKENS).className).toBe('fnos-desktop-bg')
  })

  it('WALLPAPER_BY_ID 覆盖全部条目', () => {
    expect(Object.keys(WALLPAPER_BY_ID).sort()).toEqual(WALLPAPERS.map((w) => w.id).sort())
  })

  it('photoWallpaperStyle 生成 cover 背景', () => {
    expect(photoWallpaperStyle('/x.webp')).toMatchObject({
      backgroundImage: 'url(/x.webp)', backgroundSize: 'cover', backgroundPosition: 'center',
    })
  })
})
