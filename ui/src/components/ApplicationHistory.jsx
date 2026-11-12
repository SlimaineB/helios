import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/client.js'
import Modal from './Modal.jsx'
import Pager from './Pager.jsx'
import Badge from './Badge.jsx'
import { SkeletonTable } from './Skeleton.jsx'

const PAGE = 25

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')
const short = (c) => (c ? c.slice(0, 7) : '—')

export default function ApplicationHistory({ app, instancesById, onClose }) {
  const [rows, setRows] = useState([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    api.appRuns(app.id, `?limit=${PAGE}&offset=${offset}`)
      .then((d) => {
        if (cancelled) return
        setRows(d.items || [])
        setTotal(d.total)
      })
      .catch((e) => { if (!cancelled) setError(e.message) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [app.id, offset])

  return (
    <Modal title={`History · ${app.name}`} onClose={onClose} wide>
      {error && <div className="error">{error}</div>}
      {loading ? (
        <SkeletonTable rows={6} columns={5} />
      ) : rows.length === 0 ? (
        <div className="empty">
          <p className="muted">No runs yet. History appears after the first check or apply.</p>
        </div>
      ) : (
        <>
          <div className="table-wrap history-table">
            <table className="grid">
              <thead>
                <tr>
                  <th style={{ width: '170px' }}>When</th>
                  <th>Revision</th>
                  <th style={{ width: '180px' }}>Instance</th>
                  <th style={{ width: '130px' }}>Action</th>
                  <th style={{ width: '100px' }}>Status</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className="muted">{fmt(r.finished_at || r.started_at)}</td>
                    <td>
                      <div className="rev-cell" title={r.commit_msg || ''}>
                        <span className="mono">{short(r.commit_after || r.commit_before)}</span>
                        {r.commit_msg && <span className="commit-msg">{r.commit_msg}</span>}
                      </div>
                    </td>
                    <td>
                      <Link to={`/instances/${r.instance_id}`} className="entity-link mono">
                        {instancesById?.get(r.instance_id) || r.instance_id}
                      </Link>
                    </td>
                    <td>
                      <div className="cell-title">
                        <span>{r.mode === 'apply' ? 'Apply' : 'Check'}</span>
                        {r.trigger && <span className="cell-sub">{r.trigger}</span>}
                      </div>
                    </td>
                    <td>
                      <Badge state={r.status === 'failed' ? 'failed' : r.status} label={r.status} small />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pager total={total} limit={PAGE} offset={offset} onPage={setOffset} />
        </>
      )}
    </Modal>
  )
}
