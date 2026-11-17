import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { MoreHorizontal } from 'lucide-react'

const MENU_WIDTH = 224

export default function QuickMenu({ label = 'More actions', items = [] }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState(null)
  const btnRef = useRef(null)
  const popRef = useRef(null)

  useLayoutEffect(() => {
    if (!open) return
    const rect = btnRef.current?.getBoundingClientRect()
    if (!rect) return
    const left = Math.min(window.innerWidth - MENU_WIDTH - 8, Math.max(8, rect.right - MENU_WIDTH))
    const estimated = 44 + items.filter((i) => i && !i.separator).length * 34
    const below = rect.bottom + 6
    const top = below + estimated > window.innerHeight - 8
      ? Math.max(8, rect.top - estimated - 6)
      : below
    setPos({ left, top, width: MENU_WIDTH })
  }, [open, items])

  useEffect(() => {
    if (!open) return
    const onPointer = (e) => {
      if (popRef.current?.contains(e.target) || btnRef.current?.contains(e.target)) return
      setOpen(false)
    }
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false) }
    const onScroll = () => setOpen(false)
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', onScroll)
    window.addEventListener('scroll', onScroll, true)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onScroll)
      window.removeEventListener('scroll', onScroll, true)
    }
  }, [open])

  const close = () => setOpen(false)

  return (
    <span className="quick-menu">
      <button
        ref={btnRef}
        type="button"
        className="quick-menu-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        title={label}
        onClick={() => setOpen((v) => !v)}
      >
        <MoreHorizontal size={18} />
      </button>
      {open && pos && createPortal(
        <div
          ref={popRef}
          className="quick-menu-pop"
          role="menu"
          style={{ left: pos.left, top: pos.top, width: pos.width }}
        >
          {items.filter(Boolean).map((item, i) => {
            if (item.separator) return <div key={`sep-${i}`} className="quick-menu-sep" role="separator" />
            const Icon = item.icon
            const content = (
              <>
                {Icon && <Icon size={14} aria-hidden="true" />}
                <span>{item.label}</span>
                {item.hint && <span className="quick-menu-hint">{item.hint}</span>}
              </>
            )
            if (item.to) {
              return (
                <Link key={item.label} className={`quick-menu-item${item.danger ? ' danger' : ''}`} to={item.to} role="menuitem" onClick={close}>
                  {content}
                </Link>
              )
            }
            return (
              <button
                key={item.label}
                type="button"
                className={`quick-menu-item${item.danger ? ' danger' : ''}`}
                role="menuitem"
                disabled={item.disabled}
                title={item.title}
                onClick={() => { close(); item.onClick?.() }}
              >
                {content}
              </button>
            )
          })}
        </div>,
        document.body,
      )}
    </span>
  )
}
