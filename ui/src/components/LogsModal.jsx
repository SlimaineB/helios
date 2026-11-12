import { useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Badge from './Badge.jsx'
import { useDialog } from '../hooks/useDialog.js'

export default function LogsModal({ runId, onClose, initialView = 'text' }) {
  const [state, setState] = useState({ loading: true, output: '', error: '', meta: null })
  const [fmt, setFmt] = useState(initialView)
  const dialogRef = useDialog(true, onClose)

  useEffect(() => {
    setFmt(initialView)
    let alive = true
    api
      .runLog(runId)
      .then((d) => {
        if (!alive) return
        const meta = d || {}
        const views = availableViews(meta)
        setState({ loading: false, output: meta.output || '', error: '', meta })
        setFmt((f) => (views.includes(f) ? f : 'text'))
      })
      .catch((e) => alive && setState({ loading: false, output: '', error: e.message, meta: null }))
    return () => {
      alive = false
    }
  }, [runId, initialView])

  const meta = state.meta
  const views = state.loading ? [] : availableViews(state.meta)
  const body =
    fmt === 'text' && meta?.output_text
      ? meta.output_text
      : fmt === 'diff'
        ? meta?.diff
        : fmt === 'resources'
          ? null
          : state.output

  const renderResources = () => (
    <table className="grid res-table">
      <thead>
        <tr>
          <th>Module</th>
          <th>Target</th>
        </tr>
      </thead>
      <tbody>
        {meta?.resources?.length ? (
          meta.resources.map((rr, i) => (
            <tr key={i}>
              <td className="mono">{rr.module}</td>
              <td className="mono">{rr.target || <span className="muted">(instance)</span>}</td>
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan={2} className="muted">
              No resources detected.
            </td>
          </tr>
        )}
      </tbody>
    </table>
  )

  const content =
    fmt === 'resources' ? (
      renderResources()
    ) : fmt === 'diff' && !(body && body.trim()) ? (
      <div className="muted pad">No diff available for this run.</div>
    ) : (
      <pre className="runlog">{body || '(empty output)'}</pre>
    )

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        ref={dialogRef}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={`Run log #${runId}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head">
          <strong>Run log #{runId}</strong>
          {meta && (
            <span>
              <Badge state={meta.status} label={meta.status} small />
              <span className="muted">
                {' '}
                · {meta.mode} · {new Date(meta.finished_at).toLocaleString()}
              </span>
            </span>
          )}
          {!state.loading && (
            <span className="seg" role="tablist">
              {['text', 'diff', 'json', 'resources']
                .filter((v) => views.includes(v))
                .map((v) => (
                  <button
                    key={v}
                    className={'seg-btn' + (fmt === v ? ' active' : '')}
                    onClick={() => setFmt(v)}
                    role="tab"
                    aria-selected={fmt === v}
                  >
                    {v}
                  </button>
                ))}
            </span>
          )}
          <button
            type="button"
            className="ghost small"
            onClick={onClose}
            style={{ marginLeft: 'auto' }}
            aria-label="Close run log"
          >
            ✕
          </button>
        </div>
        <div className="modal-body">
          {state.loading && <div className="muted pad">Loading…</div>}
          {state.error && <div className="error pad">{state.error}</div>}
          {!state.loading && !state.error && content}
        </div>
      </div>
    </div>
  )
}

function availableViews(meta) {
  if (!meta) return ['text']
  const v = ['text']
  if (meta.diff && meta.diff.trim()) v.push('diff')
  if (meta.output && meta.output.trim()) v.push('json')
  if (Array.isArray(meta.resources) && meta.resources.length) v.push('resources')
  return v
}