import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  CircleCheck, ClipboardCheck, History, Play, RefreshCw, Server, TriangleAlert, Pencil,
} from 'lucide-react'
import { api, instanceOnline, projectLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import AutoHealChip, { appAutoHealMeta } from '../components/AutoHealChip.jsx'
import DiffModal from '../components/DiffModal.jsx'
import QuickMenu from '../components/QuickMenu.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const FETCH_ALL = 500
const MAX_INSTANCES = 10000

async function fetchAllInstances() {
  const all = []
  let offset = 0
  let total = Infinity
  while (all.length < total && all.length < MAX_INSTANCES) {
    const data = await api.instances(`?limit=${FETCH_ALL}&offset=${offset}`)
    const items = Array.isArray(data.items) ? data.items : []
    if (!items.length) break
    all.push(...items)
    total = Number.isFinite(Number(data.total)) ? Number(data.total) : all.length
    offset += items.length
  }
  return all
}

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')

export default function Drift() {
  const { can } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [instances, setInstances] = useState(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [projectFilter, setProjectFilter] = useState(searchParams.get('project') || '')
  const [appFilter, setAppFilter] = useState(searchParams.get('app') || '')
  const [diffRun, setDiffRun] = useState(null)
  const [busy, setBusy] = useState({})
  const loadedRef = useRef(false)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      setInstances(await fetchAllInstances())
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [])

  const commands = useCommandRunner(refresh)
  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 10000 })
  useEffect(() => {
    setProjectFilter(searchParams.get('project') || '')
    setAppFilter(searchParams.get('app') || '')
  }, [searchParams])

  const rows = useMemo(() => {
    const out = []
    for (const inst of instances || []) {
      for (const app of inst.apps || []) {
        const status = app.latest_check?.status
        if (status !== 'drift' && !app.stuck) continue
        out.push({
          inst,
          app,
          state: app.stuck ? 'stuck' : 'drift',
          at: app.latest_check?.finished_at || app.last_check_at,
        })
      }
    }
    return out.sort((a, b) => new Date(b.at || 0) - new Date(a.at || 0))
  }, [instances])

  const projectOptions = useMemo(() => {
    const set = new Set()
    for (const row of rows) set.add(row.inst.project || '')
    return [...set].sort()
  }, [rows])

  const appOptions = useMemo(() => {
    const map = new Map()
    for (const row of rows) map.set(row.app.application.id, row.app.application.name)
    return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [rows])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    return rows.filter(({ inst, app }) => {
      if (projectFilter) {
        const p = inst.project || ''
        if (projectFilter === '__global__' ? p !== '' : p !== projectFilter) return false
      }
      if (appFilter && String(app.application.id) !== appFilter) return false
      if (q && ![inst.name, inst.id, inst.region, inst.project, app.application.name].filter(Boolean).join(' ').toLowerCase().includes(q)) return false
      return true
    })
  }, [rows, search, projectFilter, appFilter])

  const stats = useMemo(() => {
    const inst = new Set()
    const apps = new Set()
    let autoheal = 0
    for (const { inst: i, app } of rows) {
      inst.add(i.id)
      apps.add(app.application.id)
      if (app.autoheal) autoheal++
    }
    return { assignments: rows.length, instances: inst.size, apps: apps.size, autoheal }
  }, [rows])

  const setParam = (key, value) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next, { replace: true })
  }

  const toggleAutoHeal = async (inst, app) => {
    const key = `${inst.id}:${app.application.id}`
    setBusy((b) => ({ ...b, [key]: true }))
    try {
      await api.setAutoHeal(inst.id, app.application.id, !app.autoheal)
      toast.success(app.autoheal ? 'Auto-heal disabled for this host' : 'Auto-heal enabled for this host')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setBusy((b) => ({ ...b, [key]: false }))
    }
  }

  if (loading && !instances) {
    return (
      <section>
        <h1>Drift</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  return (
    <section className="drift-page">
      <div className="ov-head">
        <div>
          <h1><TriangleAlert size={24} /> Drift</h1>
          <p className="page-intro">Assignments that differ from the desired revision, with the next action one click away.</p>
        </div>
        <div className="ov-actions">
          <button type="button" className="btn ghost small" onClick={refresh} title="Refresh drift">
            <RefreshCw size={14} /> Refresh
          </button>
        </div>
      </div>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{stats.assignments}</span><span className="stat-label">Drifted assignments</span></div>
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{stats.instances}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot apps" /><span className="stat-num">{stats.apps}</span><span className="stat-label">Applications</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{stats.autoheal}</span><span className="stat-label">Auto-heal enabled</span></div>
      </div>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search instance, region or application…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select value={projectFilter} onChange={(e) => { setProjectFilter(e.target.value); setParam('project', e.target.value) }} aria-label="Project">
          <option value="">All projects</option>
          <option value="__global__">Global</option>
          {projectOptions.filter(Boolean).map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <select value={appFilter} onChange={(e) => { setAppFilter(e.target.value); setParam('app', e.target.value) }} aria-label="Application">
          <option value="">All applications</option>
          {appOptions.map(([id, name]) => <option key={id} value={String(id)}>{name}</option>)}
        </select>
        {(search || projectFilter || appFilter) && (
          <button type="button" className="ghost small" onClick={() => { setSearch(''); setProjectFilter(''); setAppFilter(''); setSearchParams({}, { replace: true }) }}>Clear</button>
        )}
      </div>

      {filtered.length === 0 ? (
        <div className="empty">
          <p className="muted"><CircleCheck size={16} /> {rows.length ? 'No drift matches these filters.' : 'No drift detected. Every assignment matches its desired revision.'}</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid summary-table drift-table">
            <thead>
              <tr>
                <th style={{ width: '100px' }}>State</th>
                <th>Instance</th>
                <th>Application</th>
                <th>Changed tasks</th>
                <th style={{ width: '180px' }}>Auto-heal</th>
                <th style={{ width: '150px' }}>Checked</th>
                <th style={{ width: '210px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(({ inst, app, state, at }) => {
                const key = `${inst.id}:${app.application.id}`
                const pending = commands.pending[key]
                const ah = appAutoHealMeta(inst, app)
                const tasks = app.latest_check?.changed_tasks || []
                const menu = [
                  { label: 'Open instance', icon: Server, to: `/instances/${inst.id}` },
                  { label: 'Open application', icon: History, to: `/applications?app=${app.application.id}` },
                  { separator: true },
                  {
                    label: 'Check now',
                    icon: ClipboardCheck,
                    onClick: () => commands.run(inst.id, app.application.id, 'check'),
                    disabled: !can('commands', 'create') || !!pending,
                  },
                  {
                    label: 'Apply now',
                    icon: Play,
                    onClick: () => commands.run(inst.id, app.application.id, 'apply'),
                    disabled: !can('commands', 'create') || !!pending,
                  },
                  { separator: true },
                  {
                    label: app.autoheal ? 'Disable auto-heal' : 'Enable auto-heal',
                    icon: Pencil,
                    onClick: () => toggleAutoHeal(inst, app),
                    disabled: !can('instances', 'update') || !!busy[key] || ah.locked,
                    title: ah.reason,
                  },
                ]
                return (
                  <tr key={key}>
                    <td><Badge state={state} label={state === 'stuck' ? 'Stuck' : 'Drift'} small /></td>
                    <td>
                      <div className="cell-title">
                        <Link to={`/instances/${inst.id}`} className="entity-link"><strong>{inst.name}</strong></Link>
                        <span className="cell-sub">
                          <span>{projectLabel(inst.project)}</span>
                          {inst.region && <span>· {inst.region}</span>}
                          {!instanceOnline(inst) && <span className="offline-text">· offline</span>}
                        </span>
                      </div>
                    </td>
                    <td>
                      <Link to={`/applications?app=${app.application.id}`} className="entity-link">{app.application.name}</Link>
                      {app.stuck && <div className="cell-sub drift-text">Auto-heal paused after repeated non-convergence</div>}
                    </td>
                    <td>
                      {tasks.length ? (
                        <div className="drift-tasks">
                          {tasks.slice(0, 3).map((t, i) => (
                            <span key={i} className="drift-chip" title={t.host ? `host: ${t.host}` : undefined}>{t.task}</span>
                          ))}
                          {tasks.length > 3 && <span className="drift-chip">+{tasks.length - 3}</span>}
                        </div>
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                    <td>
                      <div className="cell-title">
                        <AutoHealChip
                          enabled={!!app.autoheal}
                          scope="assignment"
                          label={app.autoheal ? 'on' : ah.label}
                          reason={ah.reason}
                        />
                        {!ah.locked ? (
                          <label className="switch" title="Auto-apply on drift for this application on this host">
                            <input
                              type="checkbox"
                              checked={!!app.autoheal}
                              disabled={!can('instances', 'update') || !!busy[key]}
                              onChange={() => toggleAutoHeal(inst, app)}
                            />
                            <span className="slider" />
                          </label>
                        ) : (
                          <span className="cell-sub">{ah.hostLocked ? 'Host auto-heal is off' : 'Application auto-heal is off'}</span>
                        )}
                      </div>
                    </td>
                    <td className="muted">{fmt(at)}</td>
                    <td>
                      <div className="row-actions">
                        {app.latest_check?.id && (
                          <button type="button" className="btn warn small" onClick={() => setDiffRun(app.latest_check.id)}>
                            Review diff
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn small"
                          disabled={!can('commands', 'create') || !!pending}
                          aria-busy={pending?.action === 'apply'}
                          onClick={() => commands.run(inst.id, app.application.id, 'apply')}
                        >
                          {pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}
                          Apply now
                        </button>
                        <QuickMenu label={`Actions for ${inst.name} / ${app.application.name}`} items={menu} />
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
    </section>
  )
}
