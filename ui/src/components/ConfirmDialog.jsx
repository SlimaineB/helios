import { useEffect, useRef } from 'react'

export default function ConfirmDialog({ open, title, message, onConfirm, onCancel, variant = 'warn' }) {
  const overlayRef = useRef(null)
  const firstFocusRef = useRef(null)

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
      setTimeout(() => firstFocusRef.current?.focus(), 50)
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  const handleKey = (e) => {
    if (!open) return
    if (e.key === 'Escape') onCancel()
    if (e.key === 'Enter' && e.target.tagName !== 'TEXTAREA') onConfirm()
  }
  useEffect(() => {
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [open, onConfirm, onCancel])

  if (!open) return null

  return (
    <div className="modal-backdrop" onClick={onCancel} ref={overlayRef}>
      <div className="modal confirm-modal" role="dialog" aria-modal="true" aria-labelledby="confirm-title" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span className={`confirm-icon ${variant}`}>
            {variant === 'danger' && '⚠'}
            {variant === 'warn' && '⚠'}
            {variant === 'info' && 'ℹ'}
          </span>
          <h3 id="confirm-title">{title}</h3>
        </div>
        <div className="modal-body" style={{ padding: '16px 20px 8px' }}>
          <p>{message}</p>
        </div>
        <div className="modal-foot" style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <button className="btn ghost" onClick={onCancel}>Cancel</button>
          <button className={`btn ${variant === 'danger' ? 'error' : variant === 'warn' ? 'warn' : ''}`} ref={firstFocusRef} onClick={onConfirm}>
            Confirm
          </button>
        </div>
      </div>
    </div>
  )
}