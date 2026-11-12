import { useEffect, useRef } from 'react'

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function useDialog(open, onClose) {
  const ref = useRef(null)
  const closeRef = useRef(onClose)
  const restoreRef = useRef(null)
  const wasOpenRef = useRef(false)

  if (open && !wasOpenRef.current) {
    wasOpenRef.current = true
    restoreRef.current = typeof document !== 'undefined' ? document.activeElement : null
  } else if (!open && wasOpenRef.current) {
    wasOpenRef.current = false
  }

  useEffect(() => {
    closeRef.current = onClose
  }, [onClose])

  useEffect(() => {
    if (!open) return

    const node = ref.current
    const focusable = () =>
      Array.from(node?.querySelectorAll(FOCUSABLE) || []).filter(
        (el) => !el.hasAttribute('disabled') && el.getClientRects().length > 0
      )

    const raf = requestAnimationFrame(() => {
      const active = document.activeElement
      if (node?.contains(active) && active !== document.body && active.tagName !== 'BUTTON') return
      const preferred = node?.querySelector(
        'input:not([type="hidden"]):not([disabled]), textarea:not([disabled]), select:not([disabled])'
      )
      const target = preferred || focusable()[0]
      if (target) target.focus()
    })

    const onKey = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        closeRef.current?.()
        return
      }
      if (e.key !== 'Tab') return

      const items = focusable()
      if (!items.length) return
      const first = items[0]
      const last = items[items.length - 1]

      if (!node?.contains(document.activeElement)) {
        e.preventDefault()
        first.focus()
        return
      }
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }

    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    document.addEventListener('keydown', onKey)

    return () => {
      cancelAnimationFrame(raf)
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = previousOverflow
      const previous = restoreRef.current
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [open])

  return ref
}
