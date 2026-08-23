// Runs immediately while visible, pauses the timer in a background tab, and
// refreshes immediately when visibility returns.
export function startVisiblePolling(run, interval, doc = document) {
  let timer = null
  let stopped = false

  const stopTimer = () => {
    if (timer != null) {
      clearInterval(timer)
      timer = null
    }
  }
  const startTimer = () => {
    if (stopped || doc.hidden) return
    run()
    if (interval > 0) timer = setInterval(run, interval)
  }
  const onVisibility = () => {
    stopTimer()
    startTimer()
  }

  startTimer()
  doc.addEventListener('visibilitychange', onVisibility)
  return () => {
    stopped = true
    stopTimer()
    doc.removeEventListener('visibilitychange', onVisibility)
  }
}
