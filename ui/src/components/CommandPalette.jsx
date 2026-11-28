import { useEffect, useRef, useState, useMemo } from 'react'
import { useDialog } from '../hooks/useDialog.js'
import { NAV_ITEMS } from '../nav.js'
import { api } from '../api/client.js'

const SHORTCUTS = [
  { keys: '⌘K', label: 'Command palette' },
  { keys: '/', label: 'Focus page search' },
  { keys: 'Esc', label: 'Close dialog' },
  { keys: 'Enter', label: 'Confirm action' },
]


export default function CommandPalette({ open, onClose, can, navigate }) {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const [entities, setEntities] = useState([])
  const inputRef = useRef(null)
  const dialogRef = useDialog(open, onClose)
  const entitySeq = useRef(0)

  const filtered = useMemo(() => {
    const q = query.toLowerCase().trim()
    const res = []
    NAV_ITEMS.forEach((n) => {
      if (!n.perm || can(n.perm.split('.')[0], n.perm.split('.')[1])) {
        const hay = `${n.group} ${n.label}`.toLowerCase()
        if (!q || hay.includes(q)) res.push({ type: 'nav', ...n })
      }
    })
    SHORTCUTS.forEach((s) => {
      if (!q || s.label.toLowerCase().includes(q) || s.keys.toLowerCase().includes(q)) res.push({ type: 'shortcut', ...s })
    })
    return res
  }, [query, can])

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(0)
    setEntities([])
  }, [open])

  useEffect(() => {
    if (!open) return
    const q = query.trim()
    if (q.length < 2) {
      setEntities([])
      return
    }
    const seq = ++entitySeq.current
    const timer = setTimeout(async () => {
      try {
        const page = await api.instances(`?q=${encodeURIComponent(q)}&compact=1&limit=8`)
        if (seq === entitySeq.current) setEntities(Array.isArray(page.items) ? page.items : [])
      } catch {
        if (seq === entitySeq.current) setEntities([])
      }
    }, 180)
    return () => clearTimeout(timer)
  }, [open, query])

  const items = useMemo(() => {
    const servers = entities.map((inst) => ({
      type: 'server',
      id: inst.id,
      label: `${inst.name} (id ${inst.id})`,
      path: `/instances/${inst.id}`,
      hint: [inst.region, inst.project, inst.state].filter(Boolean).join(' · '),
    }))
    return [...filtered, ...servers]
  }, [filtered, entities])

  useEffect(() => {
    const handler = (e) => {
      if (!open) return
      if (e.key === 'ArrowDown') { e.preventDefault(); setSelected((s) => Math.min(s + 1, items.length - 1)) }
      if (e.key === 'ArrowUp') { e.preventDefault(); setSelected((s) => Math.max(s - 1, 0)) }
      if (e.key === 'Enter') { e.preventDefault(); handleSelect(items[selected]) }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, items, selected])

  const handleSelect = (item) => {
    if (!item) return
    if ((item.type === 'nav' || item.type === 'server') && item.path) {
      navigate(item.path)
      onClose()
    }
  }

  if (!open) return null

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        ref={dialogRef}
        className="modal command-palette"
        style={{ width: 'min(560px, 90vw)', height: 'auto', maxHeight: '70vh' }}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head" style={{ padding: '10px 14px', justifyContent: 'space-between' }}>
          <div className="seg">
            <span style={{ padding: '3px 10px', color: 'var(--muted)', fontSize: '12px' }}>⌘K</span>
          </div>
        </div>
        <div className="modal-body" style={{ padding: '0 14px 14px' }}>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => { setQuery(e.target.value); setSelected(0) }}
            placeholder="Type a command, page or host…"
            style={{
              width: '100%', padding: '10px 12px',
              background: 'var(--panel-3)', border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)', color: 'var(--text)',
              fontSize: '15px', outline: 'none', marginBottom: '8px'
            }}
            autoFocus
          />
          <div style={{ maxHeight: '45vh', overflow: 'auto' }}>
            {items.length === 0 ? (
              <div className="muted" style={{ padding: '20px', textAlign: 'center' }}>No matches</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                {items.map((item, i) => (
                  <button
                    key={`${item.type}-${item.id || item.keys}`}
                    className={`command-item ${i === selected ? 'selected' : ''}`}
                    style={{
                      display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                      padding: '8px 10px', border: 'none', background: i === selected ? 'rgba(77,166,255,.12)' : 'transparent',
                      color: i === selected ? 'var(--text)' : 'var(--muted)',
                      borderRadius: '6px', fontSize: '13px', textAlign: 'left', width: '100%', cursor: 'pointer'
                    }}
                    onClick={() => handleSelect(item)}
                    onMouseEnter={() => setSelected(i)}
                  >
                    <span>{item.label}</span>
                    <span style={{ fontSize: '11px', color: 'var(--muted-2)', marginLeft: '12px' }}>{item.keys || item.hint || (item.path ? `→ ${item.path}` : '')}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}