import { createElement, useEffect, useId } from 'react'
import { T } from '../tokens'
import { motion, springs, useMotionPref } from '../motion'

const defaultItemStyle = {
  position: 'relative',
  border: 'none',
  background: 'transparent',
  cursor: 'pointer',
  display: 'flex',
  alignItems: 'center',
  gap: 6,
  boxSizing: 'border-box',
  whiteSpace: 'nowrap',
  fontFamily: 'inherit',
  flexShrink: 0,
}

function resolveStyle(style, tab, active) {
  return typeof style === 'function' ? style(tab, active) : style
}

export default function TabBar({
  tabs,
  active,
  onChange,
  getId = (tab) => tab.id,
  renderLabel,
  itemAs = 'div',
  style,
  itemStyle,
  activeItemStyle,
  inactiveItemStyle,
  activeColor = T.blue,
  activeTextColor = T.blueDeep,
  inactiveTextColor = T.ink3,
  activeWeight = 600,
  inactiveWeight = 500,
  indicatorPosition = 'bottom',
  indicatorInset = 0,
  indicatorRadius = 0,
  reserveIndicatorSpace = true,
  getItemProps,
}) {
  const layoutId = `tabbar-${useId()}-indicator`
  const pref = useMotionPref()
  const tablistId = `tabbar-${useId()}`

  useEffect(() => {
    document.getElementById(tablistId)
      ?.querySelector('[role="tab"][aria-selected="true"]')
      ?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }, [active, tablistId])

  const selectAt = (index) => {
    const tab = tabs[index]
    if (!tab) return
    const id = getId(tab)
    onChange(id, tab)
    requestAnimationFrame(() => {
      document.getElementById(tablistId)?.querySelectorAll('[role="tab"]')?.[index]?.focus?.()
    })
  }

  return (
    <div
      id={tablistId}
      className="edge-tabbar"
      role="tablist"
      aria-orientation="horizontal"
      style={{ display: 'flex', gap: 2, ...style }}
    >
      {tabs.map((tab, index) => {
        const id = getId(tab)
        const selected = active === id
        const extraProps = getItemProps ? getItemProps(tab, selected) : null
        const indicatorStyle = {
          position: 'absolute',
          left: indicatorInset,
          right: indicatorInset,
          height: 2,
          background: activeColor,
          borderRadius: indicatorRadius,
          pointerEvents: 'none',
          [indicatorPosition]: 0,
        }
        const onKeyDown = (event) => {
          extraProps?.onKeyDown?.(event)
          if (event.defaultPrevented) return
          let next = null
          if (event.key === 'ArrowRight') next = (index + 1) % tabs.length
          if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length
          if (event.key === 'Home') next = 0
          if (event.key === 'End') next = tabs.length - 1
          if (next == null) return
          event.preventDefault()
          selectAt(next)
        }

        return createElement(itemAs, {
          ...extraProps,
          key: id,
          ref: extraProps?.ref,
          role: extraProps?.role || 'tab',
          'aria-selected': selected,
          tabIndex: selected ? 0 : -1,
          onClick: (event) => {
            extraProps?.onClick?.(event)
            if (!event.defaultPrevented) onChange(id, tab)
          },
          onKeyDown,
          type: itemAs === 'button' ? 'button' : undefined,
          style: {
            ...defaultItemStyle,
            ...(reserveIndicatorSpace ? { [indicatorPosition === 'top' ? 'borderTop' : 'borderBottom']: '2px solid transparent' } : null),
            color: selected ? activeTextColor : inactiveTextColor,
            fontWeight: selected ? activeWeight : inactiveWeight,
            ...resolveStyle(itemStyle, tab, selected),
            ...(selected ? resolveStyle(activeItemStyle, tab, selected) : resolveStyle(inactiveItemStyle, tab, selected)),
            ...extraProps?.style,
          },
        },
          renderLabel ? renderLabel(tab, selected) : tab.label,
          selected && (
            <motion.div
              layoutId={layoutId}
              style={indicatorStyle}
              transition={pref.reduced ? { duration: 0 } : springs.snappy}
            />
          ),
        )
      })}
    </div>
  )
}
