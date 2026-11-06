import { useEffect, useRef, useState, useMemo } from 'react'

const SHORTCUTS = [
  { keys: '⌘K', label: 'Command palette' },
  { keys: 'Esc', label: 'Close dialog' },
  { keys: 'Enter', label: 'Confirm action' },
]

const NAV = [
  { id: 'applications', label: 'Applications', path: '/', perm: 'applications.get' },
  { id: 'instances', label: 'Instances', path: '/instances', perm: 'instances.get' },
  { id: 'topology', label: 'Topology', path: '/topology', perm: 'instances.get' },
  { id: 'projects', label: 'Projects', path: '/projects', perm: 'projects.get' },
  { id: 'sync-windows', label: 'Sync windows', path: '/sync-windows', perm: 'windows.get' },
  { id: 'repositories', label: 'Repositories', path: '/repositories', perm: 'repositories.get' },
  { id: 'audit', label: 'Audit log', path: '/audit', perm: 'audit.get' },
]

export default function CommandPalette({ open, onClose, can, navigate }) {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const inputRef = useRef(null)
  const itemsRef = useRef([])

  const filtered = useMemo(() => {
    const q = query.toLowerCase().trim()
    const res = []
    NAV.forEach((n) => {
      if (!n.perm || can(n.perm.split('.')[0], n.perm.split('.')[1])) {
        if (!q || n.label.toLowerCase().includes(q)) res.push({ type: 'nav', ...n })
      }
    })
    SHORTCUTS.forEach((s) => {
      if (!q || s.label.toLowerCase().includes(q) || s.keys.toLowerCase().includes(q)) res.push({ type: 'shortcut', ...s })
    })
    return res
  }, [query, can])

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
      setQuery('')
      setSelected(0)
      setTimeout(() => inputRef.current?.focus(), 10)
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  useEffect(() => {
    const handler = (e) => {
      if (!open) return
      if (e.key === 'Escape') onClose()
      if (e.key === 'ArrowDown') { e.preventDefault(); setSelected((s) => Math.min(s + 1, filtered.length - 1)) }
      if (e.key === 'ArrowUp') { e.preventDefault(); setSelected((s) => Math.max(s - 1, 0)) }
      if (e.key === 'Enter') { e.preventDefault(); handleSelect(filtered[selected]) }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, filtered, selected, onClose])

  const handleSelect = (item) => {
    if (!item) return
    if (item.type === 'nav') {
      if (item.path) navigate(item.path)
      onClose()
    }
  }

  if (!open) return null

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal command-palette" style={{ width: 'min(560px, 90vw)', height: 'auto', maxHeight: '70vh' }} role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
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
            placeholder="Type a command or navigate…"
            style={{
              width: '100%', padding: '10px 12px',
              background: 'var(--panel-3)', border: '1px solid var(--border)',
              borderRadius: 'var(--radius-sm)', color: 'var(--text)',
              fontSize: '15px', outline: 'none', marginBottom: '8px'
            }}
            autoFocus
          />
          <div style={{ maxHeight: '45vh', overflow: 'auto' }}>
            {filtered.length === 0 ? (
              <div className="muted" style={{ padding: '20px', textAlign: 'center' }}>No matches</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                {filtered.map((item, i) => (
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
                    <span style={{ fontSize: '11px', color: 'var(--muted-2)', marginLeft: '12px' }}>{item.keys || (item.path ? `→ ${item.path}` : '')}</span>
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