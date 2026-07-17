import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { AnimatePresence, motion, springs, useMotionPref } from '../motion'

const ToastContext = createContext(null)

const TONES = {
  ok: { bg: '#f0fdf4', border: '#bbf7d0', text: '#166534', dot: '#22c55e' },
  warn: { bg: '#fffbeb', border: '#fde68a', text: '#92400e', dot: '#f59e0b' },
  err: { bg: '#fef2f2', border: '#fecaca', text: '#991b1b', dot: '#ef4444' },
}

export function ToastProvider({ children }) {
  const [items, setItems] = useState([])
  const timers = useRef(new Map())
  const seq = useRef(0)

  const remove = useCallback((id) => {
    const timer = timers.current.get(id)
    if (timer) clearTimeout(timer)
    timers.current.delete(id)
    setItems(current => current.filter(item => item.id !== id))
  }, [])

  const push = useCallback((kind, text) => {
    const safeKind = TONES[kind] ? kind : 'ok'
    const id = `${Date.now()}-${seq.current++}`
    setItems(current => [...current, { id, kind: safeKind, text }])
    timers.current.set(id, setTimeout(() => remove(id), 3000))
    return id
  }, [remove])

  useEffect(() => () => {
    timers.current.forEach(timer => clearTimeout(timer))
    timers.current.clear()
  }, [])

  const api = useMemo(() => ({
    ok: (text) => push('ok', text),
    warn: (text) => push('warn', text),
    err: (text) => push('err', text),
    dismiss: remove,
  }), [push, remove])

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastStack items={items}/>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const toast = useContext(ToastContext)
  if (!toast) throw new Error('useToast must be used inside ToastProvider')
  return toast
}

function ToastStack({ items }) {
  const pref = useMotionPref()
  return (
    <div style={{
      position: 'fixed', left: '50%', bottom: 28, zIndex: 2000,
      transform: 'translateX(-50%)',
      display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8,
      width: 'min(440px, calc(100vw - 32px))',
      pointerEvents: 'none',
    }}>
      <AnimatePresence mode="popLayout">
        {items.map(item => {
          const tone = TONES[item.kind]
          return (
            <motion.div
              key={item.id}
              layout
              initial={pref.reduced ? { opacity: 0 } : { opacity: 0, y: 16, scale: 0.95 }}
              animate={pref.reduced ? { opacity: 1 } : { opacity: 1, y: 0, scale: 1 }}
              exit={pref.reduced ? { opacity: 0 } : { opacity: 0, y: 16, scale: 0.95 }}
              transition={pref.reduced ? pref.fadeTransition : springs.default}
              className="edge-material-surface"
              role="status"
              style={{
                width: '100%',
                border: `1px solid ${tone.border}`,
                borderRadius: 8,
                boxShadow: '0 12px 28px -12px rgba(15,23,42,0.28), 0 1px 0 rgba(255,255,255,0.72) inset',
                color: tone.text,
                padding: '10px 14px',
                fontSize: 12.5,
                fontWeight: 600,
                display: 'flex',
                alignItems: 'center',
                gap: 9,
              }}
            >
              <span style={{
                width: 8, height: 8, borderRadius: '50%',
                background: tone.dot, flexShrink: 0,
                boxShadow: `0 0 0 3px ${tone.bg}`,
              }}/>
              <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere', lineHeight: 1.4 }}>
                {item.text}
              </span>
            </motion.div>
          )
        })}
      </AnimatePresence>
    </div>
  )
}
