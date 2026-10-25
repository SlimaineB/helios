import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api/client.js'
import { toast } from '../components/Toast.jsx'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const TIMEOUT_MS = 120000
const POLL_MS = 2500

// useCommandRunner sends Sync/Check commands and follows them in the background.
// It dedupes concurrent clicks per assignment and exposes a pending map so the
// UI can show an inline spinner and disable the action buttons.
export function useCommandRunner(onDone) {
  const [pending, setPending] = useState({})
  const inFlight = useRef(new Set())
  const alive = useRef(true)

  useEffect(() => () => { alive.current = false }, [])

  const keyFor = (instId, appId) => `${instId}:${appId}`

  const run = useCallback(async (instId, appId, action) => {
    const key = keyFor(instId, appId)
    if (inFlight.current.has(key)) return false
    inFlight.current.add(key)
    setPending((p) => ({ ...p, [key]: { action, startedAt: Date.now() } }))

    const mode = action === 'apply' ? 'apply' : 'check'
    const label = action === 'apply' ? 'Sync' : 'Check'
    try {
      const before = await api.runs(instId, `?app_id=${appId}&mode=${mode}&limit=1`)
      const baseline = before.items?.[0]?.id || 0

      if (action === 'apply') await api.sync(instId, appId)
      else await api.check(instId, appId)

      const deadline = Date.now() + TIMEOUT_MS
      while (alive.current && Date.now() < deadline) {
        await sleep(POLL_MS)
        if (!alive.current) break
        const d = await api.runs(instId, `?app_id=${appId}&mode=${mode}&limit=1`)
        const latest = d.items?.[0]
        if (latest && latest.id > baseline) {
          if (latest.status === 'failed') toast.error(`${label} failed`)
          else toast.success(`${label} finished · ${latest.status}`)
          onDone?.()
          return true
        }
      }
      if (alive.current) {
        toast.warn(`${label} still pending — the agent may be offline`)
        onDone?.()
      }
      return false
    } catch (e) {
      if (alive.current) toast.error(e.message)
      return false
    } finally {
      inFlight.current.delete(key)
      if (alive.current) {
        setPending((p) => {
          const next = { ...p }
          delete next[key]
          return next
        })
      }
    }
  }, [onDone])

  return { pending, run }
}
