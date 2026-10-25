import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Pager from '../components/Pager.jsx'
import Badge from '../components/Badge.jsx'
import CopyButton from '../components/CopyButton.jsx'
import { SkeletonTable } from '../components/Skeleton.jsx'
import { toast } from '../components/Toast.jsx'

const PAGE_SIZE = 50

const METHOD_CLASS = {
  GET: 'get',
  POST: 'post',
  PUT: 'put',
  PATCH: 'patch',
  DELETE: 'delete',
}

export default function Audit() {
  const [rows, setRows] = useState([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const d = await api.audit(`?limit=${PAGE_SIZE}&offset=${offset}`)
      setRows(d.items || [])
      setTotal(d.total)
      if (!d.items.length && d.total > 0 && offset > 0) setOffset(0)
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [offset])

  useEffect(() => { refresh() }, [refresh])

  if (loading) {
    return (
      <section>
        <h1>Audit log</h1>
        <SkeletonTable rows={10} columns={7} />
      </section>
    )
  }

  return (
    <section>
      <h1>Audit log</h1>
      <p className="muted page-intro">Every mutating operator API call, with its identity and outcome.</p>

      <div className="row-toolbar">
        <span className="muted">{total} entr{total === 1 ? 'y' : 'ies'}</span>
      </div>

      {rows.length === 0 ? (
        <div className="empty"><p className="muted">No audit entries yet.</p></div>
      ) : (
        <div className="table-wrap">
          <table className="grid audit-table">
            <thead>
              <tr>
                <th style={{ width: '80px' }}>ID</th>
                <th style={{ width: '170px' }}>Time</th>
                <th style={{ width: '90px' }}>Method</th>
                <th>Path</th>
                <th style={{ width: '85px' }}>Status</th>
                <th style={{ width: '150px' }}>User</th>
                <th style={{ width: '140px' }}>Remote IP</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e) => (
                <tr key={e.id}>
                  <td className="mono muted">#{e.id}</td>
                  <td className="muted audit-time">{new Date(e.at).toLocaleString()}</td>
                  <td>
                    <span className={`method-badge ${METHOD_CLASS[e.method] || ''}`}>{e.method}</span>
                  </td>
                  <td>
                    <div className="audit-path-cell">
                      <CopyButton value={e.path} size="small" title="Copy path" />
                      <span className="mono">{e.path}</span>
                    </div>
                  </td>
                  <td>
                    <Badge state={e.status >= 400 ? 'failed' : 'ok'} label={String(e.status)} small />
                  </td>
                  <td className="audit-user" title={e.user || ''}>{e.user || '—'}</td>
                  <td>
                    <div className="audit-path-cell">
                      <CopyButton value={e.remote_ip || ''} size="small" title="Copy IP" />
                      <span className="mono muted">{e.remote_ip || '—'}</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Pager total={total} limit={PAGE_SIZE} offset={offset} onPage={setOffset} />
    </section>
  )
}
