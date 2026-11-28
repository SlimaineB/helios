import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api/client.js'
import Pager from '../components/Pager.jsx'
import Badge from '../components/Badge.jsx'
import CopyButton from '../components/CopyButton.jsx'
import SearchBox from '../components/SearchBox.jsx'
import { SkeletonTable } from '../components/Skeleton.jsx'
import { toast } from '../components/Toast.jsx'
import { useUrlState } from '../hooks/useUrlState.js'

const PAGE_SIZE = 50

const METHOD_CLASS = {
  GET: 'get',
  POST: 'post',
  PUT: 'put',
  PATCH: 'patch',
  DELETE: 'delete',
}

const STATUS_OPTIONS = [
  { value: '', label: 'All outcomes' },
  { value: 'ok', label: 'Success' },
  { value: '4xx', label: 'Client error' },
  { value: '5xx', label: 'Server error' },
  { value: 'error', label: 'Failed' },
]

export default function Audit() {
  const [rows, setRows] = useState([])
  const [total, setTotal] = useState(0)
  const [facets, setFacets] = useState({ methods: [], users: [] })
  const [loading, setLoading] = useState(true)
  const [loaded, setLoaded] = useState(false)
  const [list, setList, resetList] = useUrlState(
    { q: '', method: '', status: '', user: '', from: '', to: '', offset: 0, limit: PAGE_SIZE },
    { clamp: { limit: [1, 500], offset: [0, 1e9] } },
  )
  const { q: search, method, status, user, from, to, offset, limit: pageSize } = list

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const params = new URLSearchParams({ limit: String(pageSize), offset: String(offset) })
      if (search) params.set('q', search)
      if (method) params.set('method', method)
      if (status) params.set('status', status)
      if (user) params.set('user', user)
      if (from) params.set('from', from)
      if (to) params.set('to', to)
      const d = await api.audit(`?${params}`)
      setRows(d.items || [])
      setTotal(d.total)
      setFacets(d.facets || { methods: [], users: [] })
      if (!d.items?.length && d.total > 0 && offset > 0) setList({ offset: 0 })
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
      setLoaded(true)
    }
  }, [offset, pageSize, search, method, status, user, from, to, setList])

  useEffect(() => { refresh() }, [refresh])

  const filtersActive = !!(search || method || status || user || from || to)
  const clearFilters = () => resetList(['q', 'method', 'status', 'user', 'from', 'to', 'offset'])

  const methodOptions = useMemo(() => {
    const set = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
    for (const m of facets.methods || []) set.add(m)
    return [...set].sort()
  }, [facets.methods])

  const userOptions = useMemo(() => {
    const list = [...(facets.users || [])]
    if (user && !list.includes(user)) list.unshift(user)
    return list
  }, [facets.users, user])

  const statusLabel = STATUS_OPTIONS.find((o) => o.value === status)?.label

  if (!loaded) {
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

      <div className="filters">
        <SearchBox
          className="filter-search"
          placeholder="Search path, user, IP or ID…"
          value={search}
          onSearch={(value) => setList({ q: value, offset: 0 })}
        />
        <select value={method} onChange={(e) => setList({ method: e.target.value, offset: 0 })} aria-label="HTTP method">
          <option value="">All methods</option>
          {methodOptions.map((m) => <option key={m} value={m}>{m}</option>)}
        </select>
        <select value={status} onChange={(e) => setList({ status: e.target.value, offset: 0 })} aria-label="Outcome">
          {STATUS_OPTIONS.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <select value={user} onChange={(e) => setList({ user: e.target.value, offset: 0 })} aria-label="User">
          <option value="">All users</option>
          {userOptions.map((u) => <option key={u} value={u}>{u}</option>)}
        </select>
        <label className="date-filter">
          From
          <input type="date" value={from} max={to || undefined} onChange={(e) => setList({ from: e.target.value, offset: 0 })} />
        </label>
        <label className="date-filter">
          To
          <input type="date" value={to} min={from || undefined} onChange={(e) => setList({ to: e.target.value, offset: 0 })} />
        </label>
        {filtersActive && (
          <button type="button" className="ghost small" onClick={clearFilters}>Clear</button>
        )}
      </div>

      {filtersActive && (
        <div className="active-filter">
          <span>Showing</span>
          {search && <span className="filter-chip">Search: {search}</span>}
          {method && <span className="filter-chip">Method: {method}</span>}
          {status && <span className="filter-chip">Outcome: {statusLabel}</span>}
          {user && <span className="filter-chip">User: {user}</span>}
          {from && <span className="filter-chip">From: {from}</span>}
          {to && <span className="filter-chip">To: {to}</span>}
          <span className="spacer" />
          <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>
        </div>
      )}

      <div className="row-toolbar">
        <span className="muted">
          {loading ? 'Loading…' : `${total} entr${total === 1 ? 'y' : 'ies'}`}
        </span>
      </div>

      {rows.length === 0 ? (
        <div className="empty">
          <p className="muted">{filtersActive ? 'No entries match these filters.' : 'No audit entries yet.'}</p>
        </div>
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

      <Pager
        total={total}
        limit={pageSize}
        offset={offset}
        onPage={(offset) => setList({ offset }, { push: true })}
        onLimit={(limit) => setList({ limit, offset: 0 })}
        label="entries"
      />
    </section>
  )
}
