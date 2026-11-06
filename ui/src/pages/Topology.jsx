import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Activity,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Network,
  RefreshCw,
  Search,
  Server,
} from 'lucide-react'
import { api, projectLabel, stateLabel } from '../api/client.js'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { toast } from '../components/Toast.jsx'

const FETCH_ALL = 500
const REFRESH_MS = 10000
const ONLINE_WINDOW = 7 * 60 * 1000
const MAX_APPS = 6

const L = {
  pad: 28,
  hubW: 214,
  hubH: 116,
  agentW: 248,
  agentH: 88,
  appW: 292,
  appH: 44,
  colGap: 92,
  rowGap: 56,
  appGap: 8,
}
const AGENT_X = L.pad + L.hubW + L.colGap
const APP_X = AGENT_X + L.agentW + L.colGap
const CANVAS_W = APP_X + L.appW + L.pad
const MIN_CANVAS_H = 380

const TONES = ['accent', 'ok', 'drift', 'error', 'stuck', 'unknown', 'muted']
const TONE_VAR = {
  accent: '--accent',
  ok: '--ok',
  drift: '--drift',
  error: '--err',
  stuck: '--stuck',
  unknown: '--unknown',
  muted: '--muted-2',
}
const APP_STATE_LABEL = {
  ok: 'Synced',
  drift: 'Drift',
  error: 'Error',
  stuck: 'Stuck',
  unknown: 'No check',
}

async function fetchAllInstances() {
  const all = []
  let offset = 0
  let total = Infinity
  while (all.length < total) {
    const d = await api.instances(`?limit=${FETCH_ALL}&offset=${offset}`)
    all.push(...d.items)
    total = d.total
    if (!d.items.length) break
    offset += d.items.length
  }
  return all
}

const isOnline = (inst) =>
  !!inst?.last_seen && Date.now() - new Date(inst.last_seen).getTime() < ONLINE_WINDOW

const agentState = (inst) => {
  const s = inst?.state
  return s === 'ok' || s === 'drift' || s === 'error' ? s : 'unknown'
}

const appState = (app) => {
  if (app?.stuck) return 'stuck'
  const s = app?.latest_check?.status
  if (s === 'failed') return 'error'
  return s === 'ok' || s === 'drift' ? s : 'unknown'
}

const matchesAgent = (inst, q) =>
  [inst.name, inst.id, inst.project, inst.hostname]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
    .includes(q)

const matchesApp = (app, q) =>
  [app.application?.name, app.application?.id, app.application?.branch, app.application?.path]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
    .includes(q)

const relTime = (value) => {
  if (!value) return 'never'
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 10) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.round(hours / 24)}d ago`
}

const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`

const edgePath = (edge) => {
  const bend = Math.max(36, (edge.x2 - edge.x1) * 0.48)
  return `M ${edge.x1} ${edge.y1} C ${edge.x1 + bend} ${edge.y1}, ${edge.x2 - bend} ${edge.y2}, ${edge.x2} ${edge.y2}`
}

export default function Topology() {
  const navigate = useNavigate()
  const [instances, setInstances] = useState([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [lastUpdated, setLastUpdated] = useState(null)
  const [search, setSearch] = useState('')
  const [projectFilter, setProjectFilter] = useState('')
  const [appFilter, setAppFilter] = useState('')
  const [onlineOnly, setOnlineOnly] = useState(false)
  const [expanded, setExpanded] = useState(() => new Set())
  const [focus, setFocus] = useState(null)
  const loadedRef = useRef(false)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    else setRefreshing(true)
    try {
      setInstances(await fetchAllInstances())
      setLastUpdated(new Date())
    } catch (e) {
      toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    const t = setInterval(() => {
      if (!document.hidden) refresh()
    }, REFRESH_MS)
    return () => clearInterval(t)
  }, [refresh])

  useEffect(() => {
    setFocus(null)
  }, [search, projectFilter, appFilter, onlineOnly])

  const projectOptions = useMemo(() => {
    const set = new Set()
    for (const inst of instances) set.add(inst.project || '')
    return [...set].sort((a, b) => (a || '~').localeCompare(b || '~'))
  }, [instances])

  const appOptions = useMemo(() => {
    const map = new Map()
    for (const inst of instances) {
      for (const app of inst.apps || []) {
        map.set(String(app.application.id), app.application.name)
      }
    }
    return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [instances])

  const graphAgents = useMemo(() => {
    const q = search.trim().toLowerCase()
    const out = []
    for (const inst of instances) {
      const project = inst.project || ''
      if (projectFilter === '__global__' ? project !== '' : projectFilter && project !== projectFilter) {
        continue
      }
      if (onlineOnly && !isOnline(inst)) continue
      let apps = inst.apps || []
      if (appFilter) {
        apps = apps.filter((app) => String(app.application.id) === appFilter)
        if (!apps.length) continue
      }
      if (q && !matchesAgent(inst, q)) {
        apps = apps.filter((app) => matchesApp(app, q))
        if (!apps.length) continue
      }
      out.push({ ...inst, apps })
    }
    return out
  }, [instances, search, projectFilter, appFilter, onlineOnly])

  const stats = useMemo(() => {
    const apps = graphAgents.flatMap((inst) => inst.apps || [])
    const online = graphAgents.filter(isOnline).length
    return {
      agents: graphAgents.length,
      online,
      offline: graphAgents.length - online,
      links: apps.length,
      unique: new Set(apps.map((app) => app.application.id)).size,
      attention: apps.filter((app) => ['drift', 'error', 'stuck'].includes(appState(app))).length,
    }
  }, [graphAgents])

  const layout = useMemo(() => {
    const rows = graphAgents.map((inst) => {
      const allApps = inst.apps || []
      const isExpanded = expanded.has(inst.id)
      const displayApps = isExpanded ? allApps : allApps.slice(0, MAX_APPS)
      const hidden = allApps.length - displayApps.length
      const showMore = hidden > 0 || (isExpanded && allApps.length > MAX_APPS)
      const rowCount = displayApps.length + (showMore ? 1 : 0)
      const appsH = rowCount
        ? rowCount * L.appH + (rowCount - 1) * L.appGap
        : 0
      const groupH = Math.max(L.agentH, appsH)
      return {
        inst,
        allApps,
        displayApps,
        hidden,
        isExpanded,
        showMore,
        appsH,
        groupH,
      }
    })

    const contentH = rows.reduce((sum, row) => sum + row.groupH, 0)
      + L.rowGap * Math.max(0, rows.length - 1)
    const height = Math.max(contentH + L.pad * 2 + 20, MIN_CANVAS_H)
    let y = (height - contentH) / 2
    const groups = rows.map((row) => {
      const groupTop = y
      const agentTop = groupTop + (row.groupH - L.agentH) / 2
      const appsTop = groupTop + (row.groupH - row.appsH) / 2
      const agentCenter = agentTop + L.agentH / 2
      const apps = row.displayApps.map((app, i) => ({
        app,
        top: appsTop + i * (L.appH + L.appGap),
      }))
      y += row.groupH + L.rowGap
      return {
        ...row,
        groupTop,
        agentTop,
        agentCenter,
        apps,
        moreTop: row.showMore ? appsTop + row.displayApps.length * (L.appH + L.appGap) : null,
      }
    })

    const hubCenter = groups.length
      ? Math.min(height / 2, groups[0].agentCenter)
      : height / 2
    const hubTop = hubCenter - L.hubH / 2
    const edges = []
    for (const group of groups) {
      edges.push({
        key: `hub-${group.inst.id}`,
        kind: 'hub',
        instanceId: group.inst.id,
        x1: L.pad + L.hubW,
        y1: hubCenter,
        x2: AGENT_X,
        y2: group.agentCenter,
        tone: isOnline(group.inst) ? 'accent' : 'muted',
      })
      for (const { app, top } of group.apps) {
        edges.push({
          key: `app-${group.inst.id}-${app.application.id}`,
          kind: 'app',
          instanceId: group.inst.id,
          appId: app.application.id,
          x1: AGENT_X + L.agentW,
          y1: group.agentCenter,
          x2: APP_X,
          y2: top + L.appH / 2,
          tone: appState(app),
        })
      }
    }

    return { groups, height, hubTop, hubCenter, edges }
  }, [graphAgents, expanded])

  const edgeActive = (edge) => {
    if (!focus) return true
    if (edge.instanceId !== focus.instanceId) return false
    if (edge.kind === 'hub') return true
    return focus.kind === 'agent' || focus.appId === edge.appId
  }

  const agentActive = (id) => !focus || focus.instanceId === id
  const appActive = (instanceId, appId) =>
    !focus || (focus.instanceId === instanceId
      && (focus.kind === 'agent' || focus.appId === appId))

  const toggleExpanded = (id) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const expandAll = () => {
    setExpanded(new Set(graphAgents
      .filter((inst) => (inst.apps || []).length > MAX_APPS)
      .map((inst) => inst.id)))
  }

  const clearFilters = () => {
    setSearch('')
    setProjectFilter('')
    setAppFilter('')
    setOnlineOnly(false)
  }

  const filtersActive = !!(search || projectFilter || appFilter || onlineOnly)
  const hasExpandable = graphAgents.some((inst) => (inst.apps || []).length > MAX_APPS)
  const anyExpanded = graphAgents.some((inst) => expanded.has(inst.id))
  const updatedLabel = lastUpdated
    ? lastUpdated.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : null

  if (loading) {
    return (
      <section>
        <h1>Topology</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  return (
    <section>
      <div className="topology-page-head">
        <div>
          <h1>Topology</h1>
          <p className="page-intro">
            A live map of the hub, its agents and the applications each agent runs.
            Edges are assignments, not network connections.
          </p>
        </div>
        <div className="topology-live" title="Refreshes automatically every 10 seconds">
          <span className="live-dot" />
          <Activity size={13} />
          <span>
            {refreshing ? 'Refreshing…' : updatedLabel ? `Live · updated ${updatedLabel}` : 'Live · connecting…'}
          </span>
        </div>
      </div>

      {instances.length === 0 ? (
        <div className="empty topology-empty">
          <Network size={30} />
          <h2>No agents yet</h2>
          <p className="muted">Register an agent to see the hub-to-agent topology.</p>
          <Link className="btn primary" to="/instances">Open instances</Link>
        </div>
      ) : (
        <>
          <div className="stats">
            <div className="stat-card">
              <span className="stat-dot total" />
              <span className="stat-num">{stats.agents}</span>
              <span className="stat-label">Agents</span>
            </div>
            <div className="stat-card">
              <span className="stat-dot apps" />
              <span className="stat-num">{stats.links}</span>
              <span className="stat-label">Assignments</span>
            </div>
            <div className="stat-card">
              <span className="stat-dot drift" />
              <span className="stat-num">{stats.attention}</span>
              <span className="stat-label">Attention</span>
            </div>
            <div className="stat-card">
              <span className="stat-dot offline" />
              <span className="stat-num">{stats.offline}</span>
              <span className="stat-label">Offline</span>
            </div>
            <div className="stats-note">
              {stats.online} online · {plural(stats.unique, 'unique application')} · auto-refresh 10s
            </div>
          </div>

          <div className="filters topology-filters">
            <div className="topology-search">
              <Search size={14} />
              <input
                type="search"
                placeholder="Filter by agent, project or application…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                aria-label="Filter topology"
              />
            </div>
            <select
              value={projectFilter}
              onChange={(e) => setProjectFilter(e.target.value)}
              aria-label="Project"
            >
              <option value="">All projects</option>
              <option value="__global__">Global</option>
              {projectOptions.filter(Boolean).map((project) => (
                <option key={project} value={project}>{project}</option>
              ))}
            </select>
            <select
              value={appFilter}
              onChange={(e) => setAppFilter(e.target.value)}
              aria-label="Application"
            >
              <option value="">All applications</option>
              {appOptions.map(([id, name]) => (
                <option key={id} value={id}>{name}</option>
              ))}
            </select>
            <label className="row small filter-check">
              <input
                type="checkbox"
                checked={onlineOnly}
                onChange={(e) => setOnlineOnly(e.target.checked)}
              />
              Online only
            </label>
            {filtersActive && (
              <button type="button" className="ghost small" onClick={clearFilters}>Clear</button>
            )}
            <button
              type="button"
              className="btn ghost small topology-refresh"
              onClick={refresh}
              disabled={refreshing}
            >
              <RefreshCw size={14} className={refreshing ? 'spin' : ''} />
              Refresh
            </button>
          </div>

          {graphAgents.length === 0 ? (
            <div className="empty">
              <p className="muted">No agents match these filters.</p>
              <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>
            </div>
          ) : (
            <div className="topology-shell">
              <div className="topology-toolbar">
                <div className="topology-legend">
                  <span><span className="legend-dot ok" />Synced</span>
                  <span><span className="legend-dot drift" />Drift</span>
                  <span><span className="legend-dot error" />Error</span>
                  <span><span className="legend-dot stuck" />Stuck</span>
                  <span><span className="legend-dot unknown" />No check</span>
                  <span className="legend-divider" />
                  <span><span className="legend-line" />Assignment</span>
                </div>
                <div className="topology-toolbar-actions">
                  {hasExpandable && (
                    <button type="button" className="ghost small" onClick={expandAll}>Expand all</button>
                  )}
                  {anyExpanded && (
                    <button type="button" className="ghost small" onClick={() => setExpanded(new Set())}>
                      Collapse all
                    </button>
                  )}
                </div>
              </div>

              <div className="topology-scroll">
                <div
                  className={`topology-canvas ${focus ? 'has-focus' : ''}`}
                  style={{
                    width: CANVAS_W,
                    height: layout.height,
                    '--hub-y': `${layout.hubCenter}px`,
                  }}
                  onMouseLeave={() => setFocus(null)}
                >
                  <svg
                    className="topology-edges"
                    width={CANVAS_W}
                    height={layout.height}
                    viewBox={`0 0 ${CANVAS_W} ${layout.height}`}
                    aria-hidden="true"
                  >
                    <defs>
                      {TONES.map((tone) => (
                        <marker
                          key={tone}
                          id={`topo-arrow-${tone}`}
                          viewBox="0 0 8 8"
                          refX="7"
                          refY="4"
                          markerWidth="6"
                          markerHeight="6"
                          orient="auto"
                        >
                          <path d="M0 0 L8 4 L0 8 z" fill={`var(${TONE_VAR[tone]})`} />
                        </marker>
                      ))}
                    </defs>
                    {layout.edges.map((edge) => (
                      <path
                        key={edge.key}
                        d={edgePath(edge)}
                        className={`topology-edge tone-${edge.tone} ${edgeActive(edge) ? 'is-active' : ''}`}
                        markerEnd={`url(#topo-arrow-${edge.tone})`}
                      />
                    ))}
                    {layout.edges.filter((edge) => edge.kind === 'hub').map((edge) => (
                      <path
                        key={`flow-${edge.key}`}
                        d={edgePath(edge)}
                        className={`topology-flow tone-${edge.tone} ${edgeActive(edge) ? 'is-active' : ''}`}
                      />
                    ))}
                  </svg>

                  <div
                    className="topology-node topology-hub is-active"
                    style={{ left: L.pad, top: layout.hubTop, width: L.hubW, height: L.hubH }}
                  >
                    <span className="topology-hub-icon"><Network size={21} /></span>
                    <span className="topology-hub-copy">
                      <strong>Helios hub</strong>
                      <span>control plane</span>
                    </span>
                    <span className="topology-hub-stats">
                      <span><b>{stats.agents}</b> agents</span>
                      <span><b>{stats.links}</b> assignments</span>
                    </span>
                  </div>

                  {layout.groups.map((group) => {
                    const online = isOnline(group.inst)
                    const aState = agentState(group.inst)
                    return (
                      <Fragment key={group.inst.id}>
                        <button
                          type="button"
                          className={`topology-node topology-agent agent-${aState} ${online ? '' : 'is-offline'} ${agentActive(group.inst.id) ? 'is-active' : ''}`}
                          style={{
                            left: AGENT_X,
                            top: group.agentTop,
                            width: L.agentW,
                            height: L.agentH,
                          }}
                          onMouseEnter={() => setFocus({ kind: 'agent', instanceId: group.inst.id })}
                          onFocus={() => setFocus({ kind: 'agent', instanceId: group.inst.id })}
                          onBlur={() => setFocus(null)}
                          onClick={() => navigate(`/instances/${group.inst.id}`)}
                          title={`${group.inst.name} · ${stateLabel(aState)} · ${plural(group.allApps.length, 'application')}`}
                          aria-label={`Open agent ${group.inst.name}`}
                        >
                          <span className={`topology-agent-icon ${aState}`}>
                            <Server size={18} />
                          </span>
                          <span className="topology-agent-main">
                            <span className="topology-agent-title">
                              <strong>{group.inst.name}</strong>
                              <span
                                className={`status-dot ${online ? 'online' : 'offline'}`}
                                title={online ? 'online' : 'offline'}
                              />
                            </span>
                            <span className="topology-agent-meta">
                              <span className="project-badge">{projectLabel(group.inst.project)}</span>
                              <span>{plural(group.allApps.length, 'app')}</span>
                              {group.inst.version && <span>v{group.inst.version}</span>}
                            </span>
                            <span className="topology-agent-foot">
                              <span className={`state-badge ${aState}`}>{stateLabel(aState)}</span>
                              <span>{online ? `seen ${relTime(group.inst.last_seen)}` : `offline · ${relTime(group.inst.last_seen)}`}</span>
                            </span>
                          </span>
                          <ChevronRight size={15} className="topology-node-chevron" />
                        </button>

                        {group.apps.map(({ app, top }) => {
                          const state = appState(app)
                          const lastCheck = app.latest_check?.finished_at
                            ? new Date(app.latest_check.finished_at).toLocaleString()
                            : 'never'
                          return (
                            <button
                              type="button"
                              key={`${group.inst.id}-${app.application.id}`}
                              className={`topology-node topology-app tone-${state} ${appActive(group.inst.id, app.application.id) ? 'is-active' : ''}`}
                              style={{
                                left: APP_X,
                                top,
                                width: L.appW,
                                height: L.appH,
                              }}
                              onMouseEnter={() => setFocus({
                                kind: 'app',
                                instanceId: group.inst.id,
                                appId: app.application.id,
                              })}
                              onFocus={() => setFocus({
                                kind: 'app',
                                instanceId: group.inst.id,
                                appId: app.application.id,
                              })}
                              onBlur={() => setFocus(null)}
                              onClick={() => navigate(`/?app=${app.application.id}`)}
                              title={`${app.application.name} on ${group.inst.name} · ${APP_STATE_LABEL[state]} · last check ${lastCheck}`}
                              aria-label={`Open application ${app.application.name}`}
                            >
                              <span className={`status-dot ${state}`} />
                              <span className="topology-app-copy">
                                <span className="topology-app-name">{app.application.name}</span>
                                <span className="topology-app-sub">
                                  {app.target_revision
                                    ? `pinned ${app.target_revision.slice(0, 7)}`
                                    : app.application.branch}
                                  {' · app #'}
                                  {app.application.id}
                                </span>
                              </span>
                              <span className={`topology-app-state ${state}`}>{APP_STATE_LABEL[state]}</span>
                            </button>
                          )
                        })}

                        {group.moreTop != null && (
                          <button
                            type="button"
                            className="topology-more"
                            style={{
                              left: APP_X,
                              top: group.moreTop,
                              width: L.appW,
                              height: L.appH,
                            }}
                            onClick={() => toggleExpanded(group.inst.id)}
                          >
                            {group.isExpanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                            {group.isExpanded
                              ? 'Show less'
                              : `Show ${plural(group.hidden, 'more application')}`}
                          </button>
                        )}

                        {group.apps.length === 0 && (
                          <div
                            className="topology-app-empty"
                            style={{
                              left: APP_X,
                              top: group.groupTop + (group.groupH - L.appH) / 2,
                              width: L.appW,
                              height: L.appH,
                            }}
                          >
                            No applications assigned
                          </div>
                        )}
                      </Fragment>
                    )
                  })}
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </section>
  )
}
