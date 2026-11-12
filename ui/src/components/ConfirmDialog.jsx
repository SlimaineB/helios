import { useDialog } from '../hooks/useDialog.js'

export default function ConfirmDialog({ open, title, message, onConfirm, onCancel, variant = 'warn' }) {
  const dialogRef = useDialog(open, onCancel)

  if (!open) return null

  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <div
        ref={dialogRef}
        className="modal confirm-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        onClick={(e) => e.stopPropagation()}
      >
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
          <button type="button" className="btn ghost" onClick={onCancel}>Cancel</button>
          <button type="button" className={`btn ${variant === 'danger' ? 'error' : variant === 'warn' ? 'warn' : ''}`} onClick={onConfirm}>
            Confirm
          </button>
        </div>
      </div>
    </div>
  )
}
