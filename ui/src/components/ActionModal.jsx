import { useEffect, useState } from 'react'
import { api } from '../api/client.js'

// ActionModal sends a Sync/Check command and shows a simple "waiting" overlay
// while the agent picks it up on its next pass.
export default function ActionModal({ instId, instName, appId, action, onClose }) {
  const [seconds, setSeconds] = useState(0)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        if (action === 'apply') await api.sync(instId, appId)
        else await api.check(instId, appId)
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    })()
    const t = setInterval(() => setSeconds((s) => s + 1), 1000)
    return () => {
      cancelled = true
      clearInterval(t)
    }
  }, [instId, appId, action])

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal action-modal"
        role="dialog"
        aria-modal="true"
        aria-label={`${instName} ${action}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head">
          <strong>
            {action === 'apply' ? 'Sync' : 'Check'} · {instName}
          </strong>
          <button className="ghost small" onClick={onClose} style={{ marginLeft: 'auto' }}>
            ✕
          </button>
        </div>
        <div className="action-body">
          <div className="action-wait">
            <span className="spinner" aria-hidden="true" />
            <div>
              <strong>{error ? 'Command failed' : 'Waiting…'}</strong>
              <div className="sub">
                {error || `Command sent — the agent will run it on its next pass (~5s). ${seconds}s elapsed.`}
              </div>
              {!error && <div className="pulse-note">The buttons stay blocked until you close this.</div>}
            </div>
          </div>
          <button className="small" onClick={onClose} style={{ marginTop: 4 }}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}