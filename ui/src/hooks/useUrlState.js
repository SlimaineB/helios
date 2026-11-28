import { useCallback, useMemo, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'

function coerceNumber(raw, fallback, range) {
  let value = raw == null || raw === '' ? fallback : Number(raw)
  if (!Number.isFinite(value)) value = fallback
  value = Math.round(value)
  if (range) value = Math.min(range[1], Math.max(range[0], value))
  return value
}

export function useUrlState(defaults, options = {}) {
  const [params, setParams] = useSearchParams()
  const initialDefaults = useRef(undefined)
  if (initialDefaults.current === undefined) {
    initialDefaults.current = typeof defaults === 'function' ? defaults() : defaults
  }
  const defaultsRef = useRef(initialDefaults.current)
  const optionsRef = useRef(options)
  defaultsRef.current = initialDefaults.current
  optionsRef.current = options

  const defaultsKey = JSON.stringify(defaultsRef.current)
  const state = useMemo(() => {
    const out = {}
    const defs = defaultsRef.current
    const clamp = optionsRef.current.clamp || {}
    for (const key of Object.keys(defs)) {
      const fallback = defs[key]
      const raw = params.get(key)
      if (typeof fallback === 'number') out[key] = coerceNumber(raw, fallback, clamp[key])
      else if (typeof fallback === 'boolean') out[key] = raw == null ? fallback : raw === '1' || raw === 'true'
      else out[key] = raw == null ? fallback : raw
    }
    return out
  }, [params, defaultsKey])

  const set = useCallback((patch, opts = {}) => {
    const defs = defaultsRef.current
    const clamp = optionsRef.current.clamp || {}
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      for (const [key, raw] of Object.entries(patch)) {
        let value = raw
        if (typeof defs[key] === 'number' && value !== '' && value != null) {
          value = coerceNumber(value, defs[key], clamp[key])
        }
        if (value === '' || value == null || value === false || value === defs[key]) next.delete(key)
        else if (value === true) next.set(key, '1')
        else next.set(key, String(value))
      }
      return next
    }, { replace: !opts.push })
  }, [setParams])

  const reset = useCallback((keys) => {
    const defs = defaultsRef.current
    set(Object.fromEntries(keys.map((key) => [key, defs[key]])))
  }, [set])

  return [state, set, reset]
}
