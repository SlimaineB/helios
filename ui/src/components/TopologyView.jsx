import { Fragment, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, ChevronRight, ChevronUp, Network, Server, Terminal } from 'lucide-react'
import { projectLabel, stateLabel, instanceOnline } from '../api/client.js'

const MAX_HOSTS = 6

const L = {
  pad: 28,
  hubW: 214,
  hubH: 116,
  runnerW: 214,
  runnerH: 92,
  appW: 292,
  appH: 52,
  hostW: 248,
  hostH: 88,
  colGap: 92,
  rowGap: 56,
  hostGap: 8,
  moreH: 40,
}
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
  error: 'Failed',
  stuck: 'Stuck',
  unknown: 'No check',
}

const isOnline = (inst) => instanceOnline(inst)

const agentState = (inst) => {
  if (!isOnline(inst)) return 'offline'
  const s = inst?.state
  return s === 'ok' || s === 'drift' || s === 'error' ? s : 'unknown'
}

const appState = (app) => {
  if (app?.stuck) return 'stuck'
  const s = app?.latest_check?.status
  if (s === 'failed') return 'error'
  return s === 'ok' || s === 'drift' ? s : 'unknown'
}

const aggregateState = (hosts) => {
  const seen = new Set(hosts.map((host) => appState(host.assignment)))
  if (seen.has('error')) return 'error'
  if (seen.has('stuck')) return 'stuck'
  if (seen.has('drift')) return 'drift'
  if (seen.has('ok') && seen.size === 1) return 'ok'
  return 'unknown'
}

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

const runnerKeyFor = (inst) => inst?.tunnel_gateway || 'bastion'

const edgePath = (edge) => {
  const bend = Math.max(36, (edge.x2 - edge.x1) * 0.48)
  return `M ${edge.x1} ${edge.y1} C ${edge.x1 + bend} ${edge.y1}, ${edge.x2 - bend} ${edge.y2}, ${edge.x2} ${edge.y2}`
}

function buildGroups(instances) {
  const byApp = new Map()
  const unassigned = []
  for (const inst of instances) {
    const assignments = inst.apps || []
    if (!assignments.length) unassigned.push({ inst, assignment: null })
    for (const assignment of assignments) {
      const app = assignment.application
      if (!app) continue
      let group = byApp.get(app.id)
      if (!group) {
        group = { key: `app-${app.id}`, app, hosts: [] }
        byApp.set(app.id, group)
      }
      group.hosts.push({ inst, assignment })
    }
  }
  const groups = [...byApp.values()].sort((a, b) =>
    projectLabel(a.app.project).localeCompare(projectLabel(b.app.project))
    || a.app.name.localeCompare(b.app.name)
    || a.app.id - b.app.id)
  if (unassigned.length) groups.push({ key: 'unassigned', app: null, hosts: unassigned })
  return groups
}

export default function TopologyView({ instances, emptyMessage = 'No instances to display.' }) {
  const navigate = useNavigate()
  const [expanded, setExpanded] = useState(() => new Set())
  const [focus, setFocus] = useState(null)

  const hasTunnels = instances.some((inst) => inst.execution_mode === 'tunnel')
  const runnerX = L.pad + L.hubW + L.colGap
  const appX = hasTunnels ? runnerX + L.runnerW + L.colGap : L.pad + L.hubW + L.colGap
  const hostX = appX + L.appW + L.colGap
  const canvasW = hostX + L.hostW + L.pad

  const stats = useMemo(() => {
    const assignments = instances.flatMap((inst) => inst.apps || [])
    const online = instances.filter(isOnline).length
    const tunnels = instances.filter((inst) => inst.execution_mode === 'tunnel')
    return {
      instances: instances.length,
      tunnels: tunnels.length,
      runners: new Set(tunnels.map(runnerKeyFor)).size,
      online,
      offline: instances.length - online,
      links: assignments.length,
      unique: new Set(assignments.map((app) => app.application.id)).size,
    }
  }, [instances])

  const layout = useMemo(() => {
    const sourceGroups = buildGroups(instances)
    const rows = sourceGroups.map((group) => {
      const allHosts = group.hosts
      const isExpanded = expanded.has(group.key)
      const displayHosts = isExpanded ? allHosts : allHosts.slice(0, MAX_HOSTS)
      const hidden = allHosts.length - displayHosts.length
      const showMore = hidden > 0 || (isExpanded && allHosts.length > MAX_HOSTS)
      const moreH = showMore ? L.moreH : 0
      const hostsH = displayHosts.length * L.hostH
        + Math.max(0, displayHosts.length - 1) * L.hostGap
        + (showMore ? (displayHosts.length ? L.hostGap : 0) + moreH : 0)
      const groupH = Math.max(L.appH, hostsH)
      const firstRunner = allHosts.length && allHosts.every((host) =>
        host.inst.execution_mode === 'tunnel'
        && runnerKeyFor(host.inst) === runnerKeyFor(allHosts[0].inst))
        ? runnerKeyFor(allHosts[0].inst)
        : null
      return {
        ...group,
        allHosts,
        displayHosts,
        hidden,
        isExpanded,
        showMore,
        moreH,
        hostsH,
        groupH,
        runnerKey: firstRunner,
        state: group.app ? aggregateState(allHosts) : 'unknown',
      }
    })

    const contentH = rows.reduce((sum, row) => sum + row.groupH, 0)
      + L.rowGap * Math.max(0, rows.length - 1)
    const height = Math.max(contentH + L.pad * 2 + 20, MIN_CANVAS_H)
    let y = (height - contentH) / 2
    const groups = rows.map((row) => {
      const groupTop = y
      const appTop = groupTop + (row.groupH - L.appH) / 2
      const appCenter = appTop + L.appH / 2
      const hostsTop = groupTop + (row.groupH - row.hostsH) / 2
      let hostY = hostsTop
      const hosts = row.displayHosts.map((host) => {
        const top = hostY
        hostY += L.hostH + L.hostGap
        return { ...host, top }
      })
      const moreTop = row.showMore
        ? (hosts.length ? hosts[hosts.length - 1].top + L.hostH + L.hostGap : hostsTop)
        : null
      y += row.groupH + L.rowGap
      return { ...row, groupTop, appTop, appCenter, hostsTop, hosts, moreTop }
    })

    const hubCenter = groups.length ? Math.min(height / 2, groups[0].appCenter) : height / 2
    const hubTop = hubCenter - L.hubH / 2

    const runnerMap = new Map()
    for (const group of groups) {
      if (!group.runnerKey) continue
      let runner = runnerMap.get(group.runnerKey)
      if (!runner) {
        runner = {
          key: group.runnerKey,
          gateway: group.allHosts[0]?.inst.tunnel_gateway || 'bastion (gateway not configured)',
          groups: [],
          appIds: new Set(),
          instances: new Map(),
        }
        runnerMap.set(group.runnerKey, runner)
      }
      runner.groups.push(group)
      if (group.app) runner.appIds.add(group.app.id)
      for (const host of group.allHosts) runner.instances.set(host.inst.id, host.inst)
    }
    const runners = [...runnerMap.values()].map((runner) => {
      const center = runner.groups.reduce((sum, group) => sum + group.appCenter, 0) / runner.groups.length
      const list = [...runner.instances.values()]
      return {
        ...runner,
        center,
        top: center - L.runnerH / 2,
        count: list.length,
        online: list.filter(isOnline).length,
      }
    })
    const runnerByKey = new Map(runners.map((runner) => [runner.key, runner]))

    const edges = []
    for (const runner of runners) {
      edges.push({
        key: `hub-runner-${runner.key}`,
        kind: 'runner-hub',
        runnerKey: runner.key,
        instanceIds: [...runner.instances.keys()],
        x1: L.pad + L.hubW,
        y1: hubCenter,
        x2: runnerX,
        y2: runner.center,
        tone: runner.online > 0 ? 'accent' : 'muted',
      })
    }
    for (const group of groups) {
      const runner = group.runnerKey ? runnerByKey.get(group.runnerKey) : null
      edges.push({
        key: `source-${group.key}`,
        kind: 'source',
        appId: group.app?.id || null,
        runnerKey: group.runnerKey,
        instanceIds: group.allHosts.map((host) => host.inst.id),
        x1: runner ? runnerX + L.runnerW : L.pad + L.hubW,
        y1: runner ? runner.center : hubCenter,
        x2: appX,
        y2: group.appCenter,
        tone: group.allHosts.some((host) => isOnline(host.inst)) ? 'accent' : 'muted',
      })
      for (const host of group.hosts) {
        edges.push({
          key: `host-${group.key}-${host.inst.id}`,
          kind: 'app-host',
          appId: group.app?.id || null,
          instanceId: host.inst.id,
          x1: appX + L.appW,
          y1: group.appCenter,
          x2: hostX,
          y2: host.top + L.hostH / 2,
          tone: group.app ? appState(host.assignment) : (isOnline(host.inst) ? 'accent' : 'muted'),
        })
      }
    }

    return { groups, runners, height, hubTop, hubCenter, edges, runnerX, appX, hostX, canvasW }
  }, [instances, expanded, runnerX, appX, hostX, canvasW])

  const edgeActive = (edge) => {
    if (!focus) return true
    if (focus.kind === 'runner') return edge.runnerKey === focus.runnerKey
    if (edge.kind === 'runner-hub') {
      if (focus.kind === 'app') return (edge.instanceIds || []).some((id) =>
        layout.groups.some((group) => group.app?.id === focus.appId && group.allHosts.some((host) => host.inst.id === id)))
      if (focus.kind === 'host') return (edge.instanceIds || []).includes(focus.instanceId)
      return true
    }
    if (focus.kind === 'app') return edge.appId === focus.appId
    if (focus.kind === 'host') {
      if (edge.instanceId === focus.instanceId) return true
      return edge.kind === 'source' && (edge.instanceIds || []).includes(focus.instanceId)
    }
    return true
  }

  const groupActive = (group) => !focus
    || (focus.kind === 'runner' && group.runnerKey === focus.runnerKey)
    || (focus.kind === 'app' && group.app?.id === focus.appId)
    || (focus.kind === 'host' && group.allHosts.some((host) => host.inst.id === focus.instanceId))

  const hostActive = (group, instanceId) => !focus
    || (focus.kind === 'host' && focus.instanceId === instanceId)
    || (focus.kind === 'app' && group.app?.id === focus.appId)
    || (focus.kind === 'runner' && group.runnerKey === focus.runnerKey)

  const runnerActive = (runner) => !focus
    || (focus.kind === 'runner' && focus.runnerKey === runner.key)
    || (focus.kind === 'host' && runner.instances.has(focus.instanceId))
    || (focus.kind === 'app' && runner.appIds.has(focus.appId))

  const toggleExpanded = (key) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const expandAll = () => {
    setExpanded(new Set(layout.groups
      .filter((group) => group.allHosts.length > MAX_HOSTS)
      .map((group) => group.key)))
  }

  const hasExpandable = layout.groups.some((group) => group.allHosts.length > MAX_HOSTS)
  const anyExpanded = layout.groups.some((group) => expanded.has(group.key))

  if (!instances.length) {
    return (
      <div className="empty topology-empty">
        <Network size={30} />
        <p className="muted">{emptyMessage}</p>
      </div>
    )
  }

  return (
    <div className="topology-shell">
      <div className="topology-toolbar">
        <div className="topology-legend">
          <span><span className="legend-dot ok" />Synced</span>
          <span><span className="legend-dot drift" />Drift</span>
          <span><span className="legend-dot error" />Failed</span>
          <span><span className="legend-dot stuck" />Stuck</span>
          <span><span className="legend-dot unknown" />No check</span>
          <span className="legend-divider" />
          <span><span className="legend-line" />Application → host</span>
          <span><Terminal size={12} /> Bastion runner</span>
          <span className="legend-divider" />
          <span>Tunnel hosts run through the bastion</span>
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
            width: layout.canvasW,
            height: layout.height,
            '--hub-y': `${layout.hubCenter}px`,
          }}
          onMouseLeave={() => setFocus(null)}
        >
          <svg
            className="topology-edges"
            width={layout.canvasW}
            height={layout.height}
            viewBox={`0 0 ${layout.canvasW} ${layout.height}`}
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
            {layout.edges.filter((edge) => edge.kind !== 'app-host').map((edge) => (
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
              <span><b>{stats.instances}</b> hosts</span>
              <span><b>{stats.unique}</b> app{stats.unique === 1 ? '' : 's'}</span>
              <span><b>{stats.runners}</b> runner{stats.runners === 1 ? '' : 's'}</span>
              <span><b>{stats.links}</b> assignments</span>
            </span>
          </div>

          {layout.runners.map((runner) => (
            <div
              key={runner.key}
              className={`topology-node topology-runner ${runner.online === 0 ? 'is-offline' : ''} ${runnerActive(runner) ? 'is-active' : ''}`}
              style={{ left: layout.runnerX, top: runner.top, width: L.runnerW, height: L.runnerH }}
              onMouseEnter={() => setFocus({ kind: 'runner', runnerKey: runner.key })}
              onFocus={() => setFocus({ kind: 'runner', runnerKey: runner.key })}
              onBlur={() => setFocus(null)}
              tabIndex={0}
              role="group"
              title={`helios-runner on ${runner.gateway} · executes Ansible through ${plural(runner.count, 'reverse SSH tunnel')}`}
              aria-label={`Bastion runner ${runner.gateway}, ${plural(runner.count, 'tunnel host')}`}
            >
              <span className="topology-runner-icon"><Terminal size={18} /></span>
              <span className="topology-runner-main">
                <span className="topology-runner-title">
                  <strong>helios-runner</strong>
                  <span className="topology-mode-badge is-tunnel">bastion</span>
                </span>
                <span className="topology-runner-gateway">{runner.gateway}</span>
                <span className="topology-runner-foot">
                  {plural(runner.count, 'tunnel')} · {runner.online}/{runner.count} online
                </span>
                <span className="topology-runner-hint">Ansible over reverse SSH</span>
              </span>
            </div>
          ))}

          {layout.groups.map((group) => {
            const state = group.state
            return (
              <Fragment key={group.key}>
                {group.app ? (
                  <button
                    type="button"
                    className={`topology-node topology-app tone-${state} ${groupActive(group) ? 'is-active' : ''}`}
                    style={{ left: layout.appX, top: group.appTop, width: L.appW, height: L.appH }}
                    onMouseEnter={() => setFocus({ kind: 'app', appId: group.app.id })}
                    onFocus={() => setFocus({ kind: 'app', appId: group.app.id })}
                    onBlur={() => setFocus(null)}
                    onClick={() => navigate(`/applications/${group.app.id}`)}
                    title={`${group.app.name} · ${APP_STATE_LABEL[state]} · ${plural(group.allHosts.length, 'host')}`}
                    aria-label={`Open application ${group.app.name}`}
                  >
                    <span className={`status-dot ${state}`} />
                    <span className="topology-app-copy">
                      <span className="topology-app-name">{group.app.name}</span>
                      <span className="topology-app-sub">
                        {projectLabel(group.app.project)} · {group.app.branch || 'main'}
                        {' · '}{plural(group.allHosts.length, 'host')}
                      </span>
                    </span>
                    <span className={`topology-app-state ${state}`}>{APP_STATE_LABEL[state]}</span>
                  </button>
                ) : (
                  <div
                    className="topology-app-empty"
                    style={{
                      left: layout.appX,
                      top: group.appTop,
                      width: L.appW,
                      height: L.appH,
                    }}
                  >
                    No application assigned
                  </div>
                )}

                {group.hosts.map(({ inst, top }) => {
                  const online = isOnline(inst)
                  const aState = agentState(inst)
                  return (
                    <button
                      type="button"
                      key={`${group.key}-${inst.id}`}
                      className={`topology-node topology-agent topology-host agent-${aState} ${inst.execution_mode === 'tunnel' ? 'is-tunnel' : ''} ${online ? '' : 'is-offline'} ${hostActive(group, inst.id) ? 'is-active' : ''}`}
                      style={{ left: layout.hostX, top, width: L.hostW, height: L.hostH }}
                      onMouseEnter={() => setFocus({ kind: 'host', instanceId: inst.id })}
                      onFocus={() => setFocus({ kind: 'host', instanceId: inst.id })}
                      onBlur={() => setFocus(null)}
                      onClick={() => navigate(`/instances/${inst.id}`)}
                      title={`${inst.name} · ${group.app ? `runs ${group.app.name}` : 'no application'} · ${inst.execution_mode === 'tunnel' ? `tunnel via ${inst.tunnel_gateway || 'bastion'}` : 'agent'} · ${stateLabel(aState)}`}
                      aria-label={`Open ${inst.execution_mode === 'tunnel' ? 'tunnel host via bastion' : 'agent'} ${inst.name}`}
                    >
                      <span className={`topology-agent-icon ${aState}`}>
                        {inst.execution_mode === 'tunnel' ? <Network size={18} /> : <Server size={18} />}
                      </span>
                      <span className="topology-agent-main">
                        <span className="topology-agent-title">
                          <strong>{inst.name}</strong>
                          <span className={`status-dot ${online ? 'online' : 'offline'}`} title={online ? 'online' : 'offline'} />
                        </span>
                        <span className="topology-agent-meta">
                          <span className={`topology-mode-badge ${inst.execution_mode === 'tunnel' ? 'is-tunnel' : ''}`}>
                            {inst.execution_mode === 'tunnel' ? 'bastion' : 'agent'}
                          </span>
                          <span className="project-badge">{projectLabel(inst.project)}</span>
                          <span>{plural((inst.apps || []).length, 'app')}</span>
                          {inst.version && <span>v{inst.version}</span>}
                        </span>
                        <span className="topology-agent-foot">
                          <span className={`state-badge ${aState}`}>{stateLabel(aState)}</span>
                          <span>{online ? `seen ${relTime(inst.last_seen)}` : `offline · ${relTime(inst.last_seen)}`}</span>
                        </span>
                      </span>
                      <ChevronRight size={15} className="topology-node-chevron" />
                    </button>
                  )
                })}

                {group.moreTop != null && (
                  <button
                    type="button"
                    className="topology-more"
                    style={{ left: layout.hostX, top: group.moreTop, width: L.hostW, height: L.moreH }}
                    onClick={() => toggleExpanded(group.key)}
                  >
                    {group.isExpanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                    {group.isExpanded ? 'Show fewer hosts' : `Show ${plural(group.hidden, 'more host')}`}
                  </button>
                )}
              </Fragment>
            )
          })}
        </div>
      </div>
    </div>
  )
}
