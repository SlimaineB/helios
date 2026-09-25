import { useEffect, useState } from 'react'

// ActionModal shows a simple "waiting" overlay while a Sync/Check command is
// sent to an agent. The command is async (executed on the agent's next pass),
// so this is just a spinner + message: it gives feedback and blocks further
// clicks. No run tracking — close it whenever you want; the page refreshes.
export default function ActionModal({ instName, action, onClose }) {
  const [seconds, setSeconds] = useState(0)

  useEffect(() => {
    const t = setInterval(() => setSeconds((s) => s + 1), 1000)
    return () => clearInterval(t)
  }, [])

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
              <strong>Waiting…</strong>
              <div className="sub">
                Command sent — the agent will run it on its next pass (~5s).{' '}
                {seconds}s elapsed.
              </div>
              <div className="pulse-note">The buttons stay blocked until you close this.</div>
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