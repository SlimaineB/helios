import { useEffect, useState } from 'react'
import Modal from './Modal.jsx'

const emptyRow = () => ({ key: '', value: '', is_secret: false })

// Edits key/value variables (application defaults or per-host overrides).
// Secret values are write-only: the API never returns them, so a secret field
// is shown as a sentinel and left as-is (sentinel/empty) to keep the value.
export default function VariableEditor({ title, subtitle, load, save, onClose }) {
  const [rows, setRows] = useState([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    load()
      .then((data) => {
        if (!alive) return
        setRows(
          (data.items || []).map((v) => ({
            key: v.key,
            value: v.is_secret ? '********' : v.value,
            is_secret: !!v.is_secret,
          }))
        )
      })
      .catch((e) => alive && setError(e.message))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [load])

  const setRow = (i, patch) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)))

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      const items = rows
        .filter((r) => r.key.trim())
        .map((r) => ({ key: r.key.trim(), value: r.value, is_secret: r.is_secret }))
      await save(items)
      onClose()
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <Modal title={title} onClose={onClose}>
        <p className="muted">Loading…</p>
      </Modal>
    )
  }

  return (
    <Modal title={title} onClose={onClose} wide>
      {subtitle && <p className="sub" style={{ marginTop: 0 }}>{subtitle}</p>}
      {error && <div className="error">{error}</div>}
      <form onSubmit={submit}>
        <div className="vars-list">
          {rows.length === 0 && <p className="muted">No variables yet.</p>}
          {rows.map((r, i) => (
            <div className="vars-row" key={i}>
              <input
                className="mono"
                placeholder="variable name"
                value={r.key}
                onChange={(e) => setRow(i, { key: e.target.value })}
              />
              <input
                className="mono"
                type={r.is_secret ? 'password' : 'text'}
                placeholder={r.is_secret ? 'write-only: empty keeps current value' : 'value'}
                value={r.value}
                onChange={(e) => setRow(i, { value: e.target.value })}
              />
              <label className="row small">
                <input
                  type="checkbox"
                  checked={r.is_secret}
                  onChange={(e) => {
                    const sec = e.target.checked
                    setRow(i, { is_secret: sec, value: sec ? '********' : '' })
                  }}
                />
                secret
              </label>
              <button
                type="button"
                className="small danger"
                title="remove variable"
                onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}
              >
                ✕
              </button>
            </div>
          ))}
          <div>
            <button
              type="button"
              className="ghost small"
              onClick={() => setRows((rs) => [...rs, emptyRow()])}
            >
              ＋ Add variable
            </button>
          </div>
        </div>
        <div className="modal-foot">
          <button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>{' '}
          <button type="button" className="ghost" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}