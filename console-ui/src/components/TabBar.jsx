import { createElement, useId } from 'react'
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

  return (
    <div style={{ display: 'flex', gap: 2, ...style }}>
      {tabs.map((tab) => {
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

        return createElement(itemAs, {
          key: id,
          onClick: () => onChange(id, tab),
          type: itemAs === 'button' ? 'button' : undefined,
          ...extraProps,
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
