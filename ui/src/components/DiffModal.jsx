import { useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Badge from './Badge.jsx'

export default function DiffModal({ runId, onClose }) {
  const [state, setState] = useState({ loading: true, sections: [], error: '', meta: null })

  useEffect(() => {
    let alive = true
    api
      .runLog(runId)
      .then((d) => {
        if (!alive) return
        const meta = d || {}
        setState({ loading: false, sections: parseDiff(meta.diff || ''), meta, error: '' })
      })
      .catch((e) => alive && setState({ loading: false, sections: [], error: e.message, meta: null }))
    return () => {
      alive = false
    }
  }, [runId])

  const { sections, meta } = state

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <strong>Diff · run #{runId}</strong>
          {meta && (
            <span>
              <Badge state={meta.status} label={meta.status} small />
              <span className="muted">
                {' '}
                · {meta.mode} · {new Date(meta.finished_at).toLocaleString()}
              </span>
            </span>
          )}
          <button className="ghost small" onClick={onClose} style={{ marginLeft: 'auto' }}>
            ✕
          </button>
        </div>
        <div className="modal-body">
          {state.loading && <div className="muted pad diff-loading"><span className="spinner" aria-hidden="true" /> Loading diff…</div>}
          {state.error && <div className="error pad">{state.error}</div>}
          {!state.loading && !state.error && sections.length === 0 && (
            <div className="muted pad">No diff — this run would change nothing.</div>
          )}
          {!state.loading &&
            !state.error &&
            sections.length > 0 &&
            sections.map((s, i) => (
              <div key={i} className="diff-card">
                <div className="diff-title mono">
                  {s.title || s.task || 'Change'}
                  {s.title && s.task && <span className="muted"> — {s.task}</span>}
                </div>
                <table className="grid diff-table">
                  <thead>
                    <tr>
                      <th className="diff-cur">Current</th>
                      <th className="diff-des">Desired</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr>
                      <td className="diff-cell diff-cur">
                        <pre>{s.current.join('\n')}</pre>
                      </td>
                      <td className="diff-cell diff-des">
                        <pre>{s.desired.join('\n')}</pre>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            ))}
        </div>
      </div>
    </div>
  )
}

// parseDiff turns the stored diff text (task blocks + "-"/"+" file lines) into
// sections with a Current (before) column and a Desired (after) column.
function parseDiff(text) {
  const sections = []
  let cur = null
  for (const raw of text.split('\n')) {
    const line = raw.replace(/\r$/, '')
    if (line.startsWith('--- ')) {
      const rest = line.slice(4)
      const task = rest.match(/^(.*?)\s+\[([^\]]+)\]$/)
      if (task) {
        cur = { task: `${task[1]} [${task[2]}]`, title: '', current: [], desired: [] }
        sections.push(cur)
        continue
      }
      if (rest.startsWith('before:')) {
        if (!cur) {
          cur = { task: '', title: '', current: [], desired: [] }
          sections.push(cur)
        }
        if (!cur.title) cur.title = rest.replace(/^before:\s*/, '')
        continue
      }
      continue
    }
    if (line.startsWith('+++ ')) {
      const rest = line.slice(4)
      if (rest.startsWith('after:')) {
        if (!cur) {
          cur = { task: '', title: '', current: [], desired: [] }
          sections.push(cur)
        }
        if (!cur.title) cur.title = rest.replace(/^after:\s*/, '')
      }
      continue
    }
    if (!cur) continue
    if (line.startsWith('- ')) cur.current.push(line.slice(2))
    else if (line.startsWith('+ ')) cur.desired.push(line.slice(2))
  }
  return sections
}