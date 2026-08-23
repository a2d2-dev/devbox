import { useMemo, useRef } from 'react'
import { Icon } from '../icons'
import { useMotionPref } from '../motion'
import { useOverlayLayer } from '../overlays/OverlayProvider'
import { formatBinding, getPlatform, shortcutCategories, shortcutRegistry } from '../shortcuts/shortcuts'
import styles from './ShortcutHelpDialog.module.css'

export function ShortcutHelpDialog({ onClose, context = {} }) {
  const dialogRef = useRef(null)
  const closeRef = useRef(null)
  const pref = useMotionPref()
  const platform = useMemo(() => getPlatform(), [])
  const { backdropProps, layerProps } = useOverlayLayer({
    id: 'shortcut-help',
    onDismiss: onClose,
    layerRef: dialogRef,
    initialFocusRef: closeRef,
    modal: true,
  })

  const groups = shortcutCategories.map(category => ({
    ...category,
    shortcuts: shortcutRegistry.filter(shortcut => shortcut.category === category.id).map(shortcut => ({
      ...shortcut,
      unavailable: shortcut.enabled && !shortcut.bindings.some(binding => shortcut.enabled(context, binding)),
    })),
  }))

  return (
    <div className={styles.backdrop} {...backdropProps} data-reduced-motion={pref.reduced ? 'true' : 'false'}>
      <section
        ref={dialogRef}
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby="shortcut-help-title"
        aria-describedby="shortcut-help-description"
        tabIndex={-1}
        {...layerProps}
      >
        <header className={styles.header}>
          <div>
            <h2 id="shortcut-help-title" className={styles.title}>键盘快捷键</h2>
            <p id="shortcut-help-description" className={styles.subtitle}>在 devbox Console UI 中快速导航和管理窗口</p>
          </div>
          <button ref={closeRef} type="button" className={styles.close} onClick={onClose} aria-label="关闭快捷键帮助">
            <Icon name="x" size={16} stroke={2}/>
          </button>
        </header>
        <div className={styles.body}>
          {groups.map(group => (
            <section className={styles.section} key={group.id} aria-labelledby={`shortcut-group-${group.id}`}>
              <h3 className={styles.sectionTitle} id={`shortcut-group-${group.id}`}>{group.label}</h3>
              {group.shortcuts.map(shortcut => (
                <div className={styles.row} key={shortcut.id} aria-disabled={shortcut.unavailable || undefined}>
                  <div>
                    <div className={styles.name}>{shortcut.name}{shortcut.unavailable ? '（当前不可用）' : ''}</div>
                    <div className={styles.description}>{shortcut.description}</div>
                  </div>
                  <div className={styles.keys} aria-label={shortcut.bindings.map(binding => formatBinding(binding, platform)).join(' 或 ')}>
                    {shortcut.bindings.map((binding, index) => (
                      <kbd className={styles.key} key={`${binding.code}-${index}`}>{formatBinding(binding, platform)}</kbd>
                    ))}
                  </div>
                </div>
              ))}
            </section>
          ))}
          <div className={styles.notice}>
            快捷键仅在焦点位于 devbox Console UI 时生效。跨域 iframe、终端、代码编辑器和输入区域会接收自己的键盘事件，父页面无法也不会拦截这些按键。
          </div>
        </div>
      </section>
    </div>
  )
}
