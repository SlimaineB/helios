import { useEffect, useRef } from 'react'

const MIN_GAP_MS = 1500

export function useLiveRefresh(refresh, { interval = 30000, enabled = true } = {}) {
  const lastRef = useRef(0)
  const pendingRef = useRef(null)
  const refreshRef = useRef(refresh)

  useEffect(() => {
    refreshRef.current = refresh
  }, [refresh])

  useEffect(() => {
    if (!enabled) return undefined

    const run = () => {
      if (document.hidden) return
      lastRef.current = Date.now()
      refreshRef.current?.()
    }

    const onEvent = () => {
      if (document.hidden) return
      const wait = MIN_GAP_MS - (Date.now() - lastRef.current)
      if (wait <= 0) {
        run()
        return
      }
      if (pendingRef.current) return
      pendingRef.current = setTimeout(() => {
        pendingRef.current = null
        run()
      }, wait)
    }

    const onVisible = () => {
      if (!document.hidden) run()
    }

    window.addEventListener('helios:event', onEvent)
    document.addEventListener('visibilitychange', onVisible)
    const timer = interval > 0 ? setInterval(run, interval) : null

    return () => {
      window.removeEventListener('helios:event', onEvent)
      document.removeEventListener('visibilitychange', onVisible)
      if (timer) clearInterval(timer)
      if (pendingRef.current) clearTimeout(pendingRef.current)
      pendingRef.current = null
    }
  }, [interval, enabled])
}
