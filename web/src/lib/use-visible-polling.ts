import { useEffect, useSyncExternalStore } from 'react'

function subscribeVisibility(callback: () => void) {
  document.addEventListener('visibilitychange', callback)
  return () => document.removeEventListener('visibilitychange', callback)
}

const isVisible = () => !document.hidden

// Serial requests, cancelled when the view is hidden or unmounted.
export function useVisiblePolling(load: (signal: AbortSignal) => Promise<void>, enabled = true, intervalMs = 0) {
  const visible = useSyncExternalStore(subscribeVisibility, isVisible, () => true)
  useEffect(() => {
    if (!enabled || !visible) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      try { await load(controller.signal) }
      catch { /* The loader displays errors; cancellation is silent. */ }
      if (!controller.signal.aborted && intervalMs > 0) timer = setTimeout(() => { void poll() }, intervalMs)
    }
    void poll()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [load, enabled, intervalMs, visible])
}
