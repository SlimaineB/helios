import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Activity, ArrowRight, CircleAlert, CircleCheck, Clock3, Gauge,
  LayoutGrid, RefreshCw, Server, TriangleAlert, WifiOff,
} from 'lucide-react'
import { api, instanceOnline, stateLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const FETCH_ALL = 500
const MAX_INSTANCES = 10000

const appState = (app) => {
  const s = app?.latest_check?.status
  return s === 'failed' ? 'error' : s || 'unknown'
}

const hostState = (inst) => {
  if (!instanceOnline(inst)) return 'offline'
  const states = (inst.apps || []).map(appState)
  if (states.includes('error')) return 'error'
  if (states.includes('drift')) return 'drift'
  if (states.includes('ok')) return 'ok'
  return 'unknown'
}

const isZeroTime = (value) => {
  if (!value) return true
  const t = new Date(value)
  return !Number.isFinite(t.getTime()) || t.getFullYear() <= 1
}

function fmtAgo(value) {
  if (isZeroTime(value)) return 'never'
  const t = new Date(value).getTime()
  const s = Math.max(0, Math.round((Date.now() - t) / 1000))
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  if (s < 604800) return `${Math.round(s / 86400)}d ago`
  return new Date(value).toLocaleDateString()
}

const fmtDate = (value) => (isZeroTime(value) ? 'never' : new Date(value).toLocaleString())

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

export default function Overview() {
  const { can } = useAuth()
  const [instances, setInstances] = useState(null)
  const [apps, setApps] = useState([])
  const [projects, setProjects] = useState([])
  const [loading, setLoading] = useState(true)
  const loadedRef = useRef(false)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      const [inst, appList, projectList] = await Promise.all([
        fetchAllInstances(),
        can('applications', 'get') ? api.applications().catch(() => []) : Promise.resolve([]),
        can('projects', 'get') ? api.projects().catch(() => []) : Promise.resolve([]),
      ])
      setInstances(inst)
      setApps(appList)
      setProjects(projectList)
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [can])

  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 30000 })

  const model = useMemo(() => {
    const list = instances || []
    const assignments = []
    for (const inst of list) {
      for (const app of inst.apps || []) assignments.push({ inst, app })
    }
    const online = list.filter(instanceOnline).length
    const hostCounts = { ok: 0, drift: 0, error: 0, unknown: 0, offline: 0 }
    for (const inst of list) hostCounts[hostState(inst)]++
    const appCounts = { ok: 0, drift: 0, error: 0, unknown: 0 }
    for (const { app } of assignments) appCounts[appState(app)]++
    const recent = assignments
      .filter(({ app }) => app.latest_check?.id || app.latest_apply?.id)
      .map(({ inst, app }) => {
        const run = app.latest_check?.id ? app.latest_check : app.latest_apply
        return { inst, app, run }
      })
      .sort((a, b) => new Date(b.run.finished_at || 0) - new Date(a.run.finished_at || 0))
      .slice(0, 9)
    const attention = []
    for (const inst of list) {
      if (!instanceOnline(inst)) {
        attention.push({ kind: 'offline', inst, at: inst.last_seen })
        continue
      }
      for (const app of inst.apps || []) {
        if (appState(app) === 'error') attention.push({ kind: 'failed', inst, app, at: app.latest_check?.finished_at })
        else if (app.stuck) attention.push({ kind: 'stuck', inst, app, at: app.stuck_at || app.latest_check?.finished_at })
        else if (appState(app) === 'drift') attention.push({ kind: 'drift', inst, app, at: app.latest_check?.finished_at })
      }
    }
    attention.sort((a, b) => new Date(b.at || 0) - new Date(a.at || 0))
    const versions = new Map()
    for (const inst of list) {
      const v = inst.version || 'unknown'
      versions.set(v, (versions.get(v) || 0) + 1)
    }
    return {
      list,
      assignments,
      online,
      offline: list.length - online,
      hostCounts,
      appCounts,
      recent,
      attention: attention.slice(0, 7),
      versions: [...versions.entries()].sort((a, b) => b[1] - a[1]),
      tunnel: list.filter((i) => i.execution_mode === 'tunnel').length,
    }
  }, [instances])

  if (loading && !instances) {
    return (
      <section>
        <h1>Overview</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  const totalHosts = model.list.length || 1
  const pct = (n) => `${Math.round((n / totalHosts) * 100)}%`

  return (
    <section className="overview">
      <div className="ov-head">
        <div>
          <h1><Gauge size={24} /> Overview</h1>
          <p className="page-intro">Fleet health, recent checks and the hosts that need attention.</p>
        </div>
        <div className="ov-actions">
          <span className="ov-updated"><Activity size={14} /> live · {model.online} online</span>
          <button type="button" className="btn ghost small" onClick={refresh} title="Refresh overview">
            <RefreshCw size={14} /> Refresh
          </button>
        </div>
      </div>

      <div className="stats ov-stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{model.list.length}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{model.online}</span><span className="stat-label">Online</span></div>
        <div className="stat-card"><span className="stat-dot offline" /><span className="stat-num">{model.offline}</span><span className="stat-label">Offline</span></div>
        <div className="stat-card"><span className="stat-dot apps" /><span className="stat-num">{apps.length}</span><span className="stat-label">Applications</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{model.appCounts.drift}</span><span className="stat-label">Drifted apps</span></div>
        <div className="stat-card"><span className="stat-dot error" /><span className="stat-num">{model.appCounts.error}</span><span className="stat-label">Failed apps</span></div>
      </div>

      <div className="ov-grid">
        <div className="card ov-health">
          <div className="card-head">
            <div>
              <h3>Fleet health</h3>
              <span className="sub">Host state from the latest agent check</span>
            </div>
            <Link className="btn ghost small" to="/instances">View instances <ArrowRight size={13} /></Link>
          </div>
          <div className="card-body">
            <div className="ov-bar" aria-label="Host health distribution">
              {['ok', 'drift', 'error', 'offline', 'unknown'].map((k) => {
                const n = model.hostCounts[k]
                return n > 0 ? <span key={k} className={`ov-bar-seg ${k}`} style={{ width: pct(n) }} title={`${stateLabel(k)}: ${n}`} /> : null
              })}
            </div>
            <div className="ov-legend">
              {['ok', 'drift', 'error', 'offline', 'unknown'].map((k) => (
                <div key={k} className="ov-legend-item">
                  <span className={`status-dot ${k}`} />
                  <span>{stateLabel(k)}</span>
                  <strong>{model.hostCounts[k]}</strong>
                </div>
              ))}
            </div>
            <div className="ov-split">
              <div>
                <span className="ov-kicker">Assigned apps</span>
                <strong>{model.assignments.length}</strong>
                <span className="sub">{apps.length ? `${apps.length} defined in the catalog` : 'no application catalog access'}</span>
              </div>
              <div>
                <span className="ov-kicker">Tunnel instances</span>
                <strong>{model.tunnel}</strong>
                <span className="sub">agentless hosts via bastion</span>
              </div>
              <div>
                <span className="ov-kicker">Projects</span>
                <strong>{projects.length}</strong>
                <span className="sub">RBAC scopes configured</span>
              </div>
            </div>
          </div>
        </div>

        <div className="card ov-attention">
          <div className="card-head">
            <div>
              <h3>Needs attention</h3>
              <span className="sub">Offline, failed, stuck or drifting assignments</span>
            </div>
            <Link className="btn ghost small" to="/drift">Open drift <ArrowRight size={13} /></Link>
          </div>
          <div className="card-body">
            {model.attention.length === 0 ? (
              <div className="ov-empty"><CircleCheck size={18} /> Everything looks healthy.</div>
            ) : (
              <ul className="ov-attention-list">
                {model.attention.map((item, i) => (
                  <li key={`${item.kind}-${item.inst.id}-${item.app?.application.id || i}`}>
                    <span className={`ov-att-icon ${item.kind}`}>
                      {item.kind === 'offline' ? <WifiOff size={15} /> : item.kind === 'failed' ? <CircleAlert size={15} /> : <TriangleAlert size={15} />}
                    </span>
                    <div className="ov-att-main">
                      <Link to={`/instances/${item.inst.id}`}>{item.inst.name}</Link>
                      {item.app ? (
                        <Link className="ov-att-app" to={`/applications?app=${item.app.application.id}`}>{item.app.application.name}</Link>
                      ) : (
                        <span className="sub">last seen {fmtDate(item.inst.last_seen)}</span>
                      )}
                    </div>
                    <Badge state={item.kind === 'offline' ? 'offline' : item.kind === 'failed' ? 'error' : item.kind} label={item.kind} small />
                    <span className="ov-att-time">{fmtAgo(item.at)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>

      <div className="ov-grid ov-bottom">
        <div className="card ov-recent">
          <div className="card-head">
            <div>
              <h3>Recent activity</h3>
              <span className="sub">Latest check/apply result per assignment</span>
            </div>
            <span className="sub"><Clock3 size={13} /> {model.recent.length} shown</span>
          </div>
          <div className="table-wrap">
            <table className="grid ov-table">
              <thead>
                <tr>
                  <th>State</th>
                  <th>Instance</th>
                  <th>Application</th>
                  <th>Mode</th>
                  <th>When</th>
                  <th>Commit</th>
                </tr>
              </thead>
              <tbody>
                {model.recent.map(({ inst, app, run }) => (
                  <tr key={`${inst.id}-${app.application.id}-${run.id}`}>
                    <td><span className={`state-badge ${appState(app)}`}>{stateLabel(appState(app))}</span></td>
                    <td><Link to={`/instances/${inst.id}`} className="entity-link">{inst.name}</Link></td>
                    <td><Link to={`/applications?app=${app.application.id}`} className="entity-link">{app.application.name}</Link></td>
                    <td><span className="mono">{run.mode}</span></td>
                    <td className="muted">{fmtAgo(run.finished_at)}</td>
                    <td className="mono muted">{run.commit_after?.slice(0, 7) || '—'}</td>
                  </tr>
                ))}
                {model.recent.length === 0 && (
                  <tr><td colSpan={6} className="muted">No checks have been reported yet.</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>

        <div className="card ov-versions">
          <div className="card-head">
            <div>
              <h3>Agent versions</h3>
              <span className="sub">Spread across reporting hosts</span>
            </div>
            <Link className="btn ghost small" to="/instances">Hosts <ArrowRight size={13} /></Link>
          </div>
          <div className="card-body">
            {model.versions.length === 0 ? (
              <span className="muted">No agent has checked in yet.</span>
            ) : (
              <div className="ov-version-list">
                {model.versions.map(([version, count]) => (
                  <div key={version} className="ov-version">
                    <span className="mono">{version}</span>
                    <div className="ov-version-track"><span style={{ width: pct(count) }} /></div>
                    <strong>{count}</strong>
                  </div>
                ))}
              </div>
            )}
            <div className="ov-quick">
              <Link className="btn ghost small" to="/applications"><LayoutGrid size={14} /> Applications</Link>
              <Link className="btn ghost small" to="/instances"><Server size={14} /> Instances</Link>
              <Link className="btn ghost small" to="/map">World map <ArrowRight size={13} /></Link>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
