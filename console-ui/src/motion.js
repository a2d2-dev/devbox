import { createElement } from 'react'
import { motion, AnimatePresence, useReducedMotion } from 'motion/react'

export { motion, AnimatePresence, useReducedMotion }

export const springs = {
  default: { type: 'spring', bounce: 0, duration: 0.35 },
  gentle: { type: 'spring', bounce: 0, duration: 0.45 },
  momentum: { type: 'spring', bounce: 0.2, duration: 0.4 },
  snappy: { type: 'spring', bounce: 0, duration: 0.25 },
}

export function useMotionPref() {
  const reduced = useReducedMotion()
  return {
    reduced,
    fadeTransition: { duration: reduced ? 0.2 : 0.2 },
    spring: (name = 'default') => (reduced ? { duration: 0.2 } : springs[name]),
    transform: (value) => (reduced ? {} : value),
  }
}

export function PressScale({ children, as = motion.div, ...props }) {
  const pref = useMotionPref()
  return createElement(as, {
    whileTap: pref.reduced ? undefined : { scale: 0.97 },
    transition: pref.spring('snappy'),
    ...props,
  }, children)
}

export function PopScale({ children, origin = 'top right', ...props }) {
  const pref = useMotionPref()
  return createElement(motion.div, {
    ...props,
    initial: pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.9 },
    animate: pref.reduced ? { opacity: 1 } : { opacity: 1, scale: 1 },
    exit: pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.9 },
    transition: pref.reduced ? pref.fadeTransition : springs.snappy,
    style: { transformOrigin: origin, ...props.style },
  }, children)
}

export function Fade({ children, ...props }) {
  const pref = useMotionPref()
  return createElement(AnimatePresence, null,
    createElement(motion.div, {
      initial: { opacity: 0 },
      animate: { opacity: 1 },
      exit: { opacity: 0 },
      transition: pref.fadeTransition,
      ...props,
    }, children),
  )
}

export function rubberband(distance, dimension, constant = 0.55) {
  if (!dimension || !Number.isFinite(distance)) return 0
  const sign = Math.sign(distance)
  const abs = Math.abs(distance)
  return sign * ((1 - (1 / ((abs * constant / dimension) + 1))) * dimension)
}

export function project(velocity, decelerationRate = 0.998) {
  if (!Number.isFinite(velocity)) return 0
  return (velocity / 1000) * decelerationRate / (1 - decelerationRate)
}
