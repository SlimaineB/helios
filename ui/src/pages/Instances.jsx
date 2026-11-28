import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ClipboardCheck, LayoutGrid, List, Network, Play, RefreshCw, Server } from 'lucide-react'
import { api, stateLabel, instanceOnline, DEFAULT_PROJECT, projectForApi, projectLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import AutoHealChip, { hostAutoHealOn } from '../components/AutoHealChip.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import Pager from '../components/Pager.jsx'
import CopyButton from '../components/CopyButton.jsx'
import RegionPicker from '../components/RegionPicker.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import TopologyView from '../components/TopologyView.jsx'
import InstancesCard from '../components/InstancesCard.jsx'
import TagList, { parseTags } from '../components/TagList.jsx'
import QuickMenu from '../components/QuickMenu.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const PAGE_SIZE = 20
const TOPOLOGY_LIMIT = 300

const online = (inst) => instanceOnline(inst)

const fmtTime = (t) => (t ? new Date(t).toLocaleTimeString() : '—')

function appState(app) {
  const s = app?.latest_check?.status
  return s === 'failed' ? 'error' : s || 'unknown'
}

function instanceMatches(inst, q) {
  return !q || [inst.name, inst.id, inst.project, inst.region, ...(inst.tags || [])].join(' ').toLowerCase().includes(q)
}

function appMatches(inst, app, { appFilter, stateFilter, q }) {
  if (appFilter && String(app.application.id) !== appFilter) return false
  if (stateFilter && appState(app) !== stateFilter) return false
  if (q && !instanceMatches(inst, q) && !app.application.name.toLowerCase().includes(q)) return false
  return true
}

function instanceSummary(inst) {
  const apps = inst.apps || []
  return apps.reduce((acc, a) => {
    acc.total++
    if (a.stuck) acc.stuck++
    if (a.autoheal) acc.autoheal++
    const st = a.latest_check?.status
    if (st === 'ok') acc.synced++
    else if (st === 'drift') acc.drift++
    else if (st === 'failed' || st === 'error') acc.failed++
    else acc.unknown++
    return acc
  }, { total: 0, synced: 0, drift: 0, failed: 0, stuck: 0, unknown: 0, autoheal: 0 })
}

function InstancesTree({ instances, onCheckAll, onApplyAll, onHostAutoHeal, can, selected, onSelect }) {
  const rows = useMemo(
    () => [...instances].sort((a, b) => (a.project || '~').localeCompare(b.project || '~') || a.name.localeCompare(b.name)),
    [instances],
  )

  return (
    <div className="table-wrap">
      <table className="grid summary-table">
        <thead>
          <tr>
            <th style={{ width: '36px' }} aria-label="Select" />
            <th style={{ width: '36px' }} aria-label="Status" />
            <th>Instance</th>
            <th style={{ width: '150px' }}>Project</th>
            <th style={{ width: '160px' }}>Server</th>
            <th style={{ width: '190px' }}>Applications</th>
            <th style={{ width: '200px' }}>Auto-heal</th>
            <th style={{ width: '120px' }}>Last check</th>
            <th style={{ width: '130px' }}>Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((inst) => {
            const isOnline = online(inst)
            const c = instanceSummary(inst)
            const hostOn = hostAutoHealOn(inst)
            const hostState = !isOnline ? 'offline' : inst.state === 'ok' ? 'ok' : inst.state || 'unknown'
            const canRun = can('commands', 'create') && c.total > 0
            const menu = [
              { label: 'Open details', icon: Server, to: `/instances/${inst.id}` },
              { separator: true },
              { label: 'Check all applications', icon: ClipboardCheck, onClick: () => onCheckAll?.(inst), disabled: !canRun },
              { label: 'Apply all applications', icon: Play, onClick: () => onApplyAll?.(inst), disabled: !canRun },
              c.total === 1
                ? { label: 'Open application', icon: Network, to: `/applications/${inst.apps[0].application.id}` }
                : null,
              { separator: true },
              {
                label: hostOn ? 'Disable host auto-heal' : 'Enable host auto-heal',
                icon: RefreshCw,
                onClick: () => onHostAutoHeal?.(inst, !hostOn),
                disabled: !can('instances', 'update'),
              },
            ]
            return (
              <tr key={inst.id} className="summary-row">
                <td>
                  <input
                    type="checkbox"
                    className="row-select"
                    checked={selected?.has(inst.id) || false}
                    onChange={() => onSelect?.(inst.id)}
                    aria-label={`Select ${inst.name}`}
                  />
                </td>
                <td>
                  <span
                    className={`status-dot ${isOnline ? 'online' : 'offline'}`}
                    title={isOnline ? 'online' : 'offline'}
                  />
                </td>
                <td>
                  <div className="cell-title">
                    <Link to={`/instances/${inst.id}`} className="instance-link">
                      <strong>{inst.name}</strong>
                    </Link>
                    <span className="cell-sub">
                      <Badge state="agent" label="agent" small />
                      <span className="mono">{inst.id}</span>
                      {inst.region && <span>· {inst.region}</span>}
                      <TagList tags={inst.tags} />
                    </span>
                  </div>
                </td>
                <td><span className="project-badge">{projectLabel(inst.project)}</span></td>
                <td>
                  <div className="cell-title">
                    <Badge state={hostState} label={stateLabel(hostState)} small />
                    <span className="cell-sub">{isOnline ? 'reachable' : 'no recent heartbeat'}</span>
                  </div>
                </td>
                <td>
                  <div className="cell-title">
                    <strong className="mono">{c.synced}/{c.total}</strong>
                    <span className="cell-sub">
                      {c.total} deployed
                      {c.drift ? ` · ${c.drift} drift` : ''}
                      {c.failed + c.stuck ? ` · ${c.failed + c.stuck} failed` : ''}
                    </span>
                  </div>
                </td>
                <td>
                  <div className="cell-title">
                    <AutoHealChip
                      enabled={hostOn}
                      scope="host"
                      reason={hostOn ? 'Host auto-apply enabled' : 'No application on this host is applied automatically'}
                    />
                    <span className="cell-sub">{c.autoheal}/{c.total} apps</span>
                  </div>
                </td>
                <td className="muted">{fmtTime(inst.last_check_at)}</td>
                <td>
                  <div className="row-actions">
                    <Link className="btn ghost small" to={`/instances/${inst.id}`}>Open</Link>
                    <QuickMenu label={`Actions for ${inst.name}`} items={menu} />
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export default function Instances() {
  const { can } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [instances, setInstances] = useState([])
  const [total, setTotal] = useState(0)
  const [facets, setFacets] = useState({ projects: [], tags: [], apps: [] })
  const [serverStats, setServerStats] = useState(null)
  const [topologyInstances, setTopologyInstances] = useState([])
  const [topologyTotal, setTopologyTotal] = useState(0)
  const [topologyFetched, setTopologyFetched] = useState(0)
  const [loading, setLoading] = useState(true)
  const loadedRef = useRef(false)
  const reqSeq = useRef(0)
  const [showReg, setShowReg] = useState(false)
  const [regName, setRegName] = useState('')
  const [regProject, setRegProject] = useState(DEFAULT_PROJECT)
  const [regRegion, setRegRegion] = useState('')
  const [regTags, setRegTags] = useState('')
  const [projects, setProjects] = useState([])
  const [regions, setRegions] = useState([])
  const [regResult, setRegResult] = useState(null)
  const [regError, setRegError] = useState('')
  const [downloads, setDownloads] = useState([])
  const [busy, setBusy] = useState({})
  const [confirm, setConfirm] = useState(null)
  const [pageOff, setPageOff] = useState(0)
  const [selected, setSelected] = useState(() => new Set())
  const [bulkBusy, setBulkBusy] = useState('')
  const [view, setView] = useState(() => {
    const saved = localStorage.getItem('helios_instances_view')
    if (saved === 'topology' || saved === 'list' || saved === 'cards') return saved
    return 'cards'
  })

  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [stateFilter, setStateFilter] = useState('')
  const [projectFilter, setProjectFilter] = useState(searchParams.get('project') || '')
  const [appFilter, setAppFilter] = useState(searchParams.get('app') || '')
  const [tagFilter, setTagFilter] = useState('')
  const [onlineOnly, setOnlineOnly] = useState(false)

  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput.trim()), 250)
    return () => clearTimeout(timer)
  }, [searchInput])

  const refresh = useCallback(async () => {
    const seq = ++reqSeq.current
    if (!loadedRef.current) setLoading(true)
    try {
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String(pageOff), facets: '1', stats: '1' })
      if (search) params.set('q', search)
      if (stateFilter) params.set('state', stateFilter)
      if (projectFilter) params.set('project', projectFilter)
      if (appFilter) params.set('app_id', appFilter)
      if (tagFilter) params.set('tag', tagFilter)
      if (onlineOnly) params.set('online', '1')
      const [d, projectList, regionList] = await Promise.all([
        api.instances(`?${params}`),
        api.projects().catch(() => []),
        api.regions().catch(() => []),
      ])
      if (seq !== reqSeq.current) return
      const items = Array.isArray(d.items) ? d.items : []
      const nextTotal = Number(d.total) || 0
      setInstances(items)
      setTotal(nextTotal)
      setFacets(d.facets || { projects: [], tags: [], apps: [] })
      setServerStats(d.stats || null)
      setProjects(projectList)
      setRegions(regionList)
      if (!items.length && pageOff > 0 && nextTotal > 0) {
        const last = Math.floor((nextTotal - 1) / PAGE_SIZE) * PAGE_SIZE
        setPageOff(last)
      }
    } catch (e) {
      if (seq === reqSeq.current && !loadedRef.current) toast.error(e.message)
    } finally {
      if (seq === reqSeq.current) {
        loadedRef.current = true
        setLoading(false)
      }
    }
  }, [appFilter, onlineOnly, pageOff, projectFilter, search, stateFilter, tagFilter])

  const commands = useCommandRunner(refresh)
  const modalOpen = showReg || !!confirm
  useEffect(() => {
    refresh()
  }, [refresh])
  useLiveRefresh(refresh, { interval: 30000, enabled: !modalOpen })

  useEffect(() => {
    setAppFilter(searchParams.get('app') || '')
    setProjectFilter(searchParams.get('project') || '')
  }, [searchParams])

  const setParam = (key, value) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next, { replace: true })
  }
  const applyAppFilter = (v) => { setAppFilter(v); setParam('app', v) }
  const applyProjectFilter = (v) => { setProjectFilter(v); setParam('project', v) }

  useEffect(() => {
    setPageOff(0)
  }, [search, stateFilter, projectFilter, appFilter, tagFilter, onlineOnly])

  const addInstance = async (e) => {
    e.preventDefault()
    setRegError('')
    try {
      const res = await api.createInstance(regName.trim(), projectForApi(regProject), regRegion.trim(), parseTags(regTags))
      setRegResult(res)
      setRegName('')
      setRegProject(DEFAULT_PROJECT)
      setRegRegion('')
      setRegTags('')
      toast.success('Instance registered')
      refresh()
    } catch (err) {
      setRegError(err.message)
    }
  }

  const closeReg = () => {
    setShowReg(false)
    setRegResult(null)
    setRegName('')
    setRegRegion('')
    setRegTags('')
    setRegError('')
  }

  useEffect(() => {
    if (!showReg) return
    api.downloads().then((d) => setDownloads(d.files || [])).catch(() => setDownloads([]))
  }, [showReg])

  useEffect(() => {
    if (searchParams.get('register') !== '1') return
    setRegName(searchParams.get('name') || '')
    setRegProject(searchParams.get('project') || DEFAULT_PROJECT)
    setShowReg(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const hubURL = location.origin || `${location.protocol}//${location.host}`
  const hasHubDownload = downloads.some((f) => /^helios-agent-linux-(amd64|arm64)\.tar\.gz$/.test(f.name))
  const installCommand = regResult && hasHubDownload
    ? [
        'arch=$(uname -m)',
        'case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac',
        `curl -fsSLO ${hubURL}/downloads/helios-agent-linux-\${arch}.tar.gz`,
        'tar -xzf "helios-agent-linux-${arch}.tar.gz"',
        `sudo ./install-agent.sh --server ${hubURL} --token '${regResult.token}'`,
      ].join('\n')
    : ''

  const toggleHostAutoHeal = async (inst, enabled) => {
    try {
      await api.setInstanceAutoHeal(inst.id, enabled)
      toast.success(enabled ? 'Host auto-apply enabled' : 'Host auto-apply disabled')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const runAll = (inst, action) => {
    for (const app of inst?.apps || []) commands.run(inst.id, app.application.id, action)
  }

  const projectOptions = useMemo(() => {
    const set = new Set(facets.projects || [])
    if (projectFilter && projectFilter !== '__global__') set.add(projectFilter)
    return [...set].filter(Boolean).sort((a, b) => a.localeCompare(b))
  }, [facets.projects, projectFilter])

  const appOptions = useMemo(() => {
    const list = (facets.apps || []).map((a) => ({ id: a.id, name: a.name }))
    if (appFilter && !list.some((a) => String(a.id) === appFilter)) {
      list.unshift({ id: Number(appFilter), name: `#${appFilter}` })
    }
    return list
  }, [facets.apps, appFilter])

  const tagOptions = useMemo(() => {
    const set = new Set(facets.tags || [])
    if (tagFilter) set.add(tagFilter)
    return [...set].sort((a, b) => a.localeCompare(b))
  }, [facets.tags, tagFilter])

  const q = search.trim().toLowerCase()

  useEffect(() => {
    if (view !== 'topology') return undefined
    let cancelled = false
    const params = new URLSearchParams({ limit: String(TOPOLOGY_LIMIT) })
    if (search) params.set('q', search)
    if (stateFilter) params.set('state', stateFilter)
    if (projectFilter) params.set('project', projectFilter)
    if (appFilter) params.set('app_id', appFilter)
    if (tagFilter) params.set('tag', tagFilter)
    if (onlineOnly) params.set('online', '1')
    api.instances(`?${params}`).then((d) => {
      if (cancelled) return
      const items = Array.isArray(d.items) ? d.items : []
      setTopologyInstances(items.map((inst) => ({
        ...inst,
        apps: (inst.apps || []).filter((a) => appMatches(inst, a, { appFilter, stateFilter, q })),
      })))
      setTopologyTotal(Number(d.total) || 0)
      setTopologyFetched(items.length)
    }).catch((e) => {
      if (!cancelled) toast.error(e.message)
    })
    return () => { cancelled = true }
  }, [view, search, stateFilter, projectFilter, appFilter, tagFilter, onlineOnly, q])

  const stats = {
    total: serverStats?.total ?? total,
    ok: serverStats?.synced ?? 0,
    drift: serverStats?.drift ?? 0,
    offline: serverStats?.offline ?? instances.filter((i) => !online(i)).length,
  }

  const changeView = (next) => {
    setView(next)
    localStorage.setItem('helios_instances_view', next)
  }

  const toggleSelected = (id) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const pageItems = instances
  const pageAllSelected = pageItems.length > 0 && pageItems.every((i) => selected.has(i.id))
  const togglePageSelection = () => {
    const ids = pageItems.map((i) => i.id)
    setSelected((prev) => {
      const next = new Set(prev)
      for (const id of ids) {
        if (pageAllSelected) next.delete(id)
        else next.add(id)
      }
      return next
    })
  }

  const bulk = async (action) => {
    const ids = [...selected]
    if (!ids.length || bulkBusy) return
    setBulkBusy(action)
    try {
      const res = await api.bulkCommands(action, ids)
      const label = action === 'apply' ? 'Apply' : 'Check'
      if (res.queued) toast.success(`${label} queued for ${res.queued} assignment${res.queued === 1 ? '' : 's'}`)
      else toast.info('Nothing was queued')
      if (res.skipped) toast.info(`${res.skipped} assignment${res.skipped === 1 ? '' : 's'} skipped`)
      if (res.errors?.length) toast.error(`${res.errors.length} host${res.errors.length === 1 ? '' : 's'} failed: ${res.errors[0].error}`)
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setBulkBusy('')
    }
  }

  const confirmBulkApply = () => setConfirm({
    title: 'Apply to selected hosts',
    message: `Queue an apply for every assigned application across ${selected.size} selected host${selected.size === 1 ? '' : 's'}? This changes the managed hosts now.`,
    variant: 'warn',
    onConfirm: () => { setConfirm(null); bulk('apply') },
  })
  const filtersActive = !!(search || stateFilter || projectFilter || appFilter || tagFilter || onlineOnly)
  const clearFilters = () => {
    setSearchInput('')
    setSearch('')
    setStateFilter('')
    setProjectFilter('')
    setAppFilter('')
    setModeFilter('')
    setTagFilter('')
    setOnlineOnly(false)
    setSearchParams({}, { replace: true })
  }

  if (loading) {
    return (
      <section>
        <h1>Instances</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  return (
    <section>
      <h1>Instances</h1>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{stats.total}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{stats.ok}</span><span className="stat-label">Synced apps</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{stats.drift}</span><span className="stat-label">Drifted apps</span></div>
        <div className="stat-card"><span className="stat-dot offline" /><span className="stat-num">{stats.offline}</span><span className="stat-label">Offline</span></div>
        <div className="stats-note">
          App states from latest checks · live refresh every 30s
        </div>
      </div>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search name, id, region, tag, project or application…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
        />
        <select
          value={stateFilter}
          onChange={(e) => setStateFilter(e.target.value)}
          aria-label="State"
          title="Filters agents and their applications"
        >
          <option value="">All states</option>
          <option value="ok">Synced</option>
          <option value="drift">Drift</option>
          <option value="error">Failed</option>
          <option value="unknown">Unknown</option>
        </select>
        <select value={projectFilter} onChange={(e) => applyProjectFilter(e.target.value)} aria-label="Project">
          <option value="">All projects</option>
          <option value="__global__">Global</option>
          {projectOptions.filter(Boolean).map((p) => (
            <option key={p} value={p}>{p}</option>
          ))}
        </select>
        <select value={tagFilter} onChange={(e) => setTagFilter(e.target.value)} aria-label="Tag">
          <option value="">All tags</option>
          {tagOptions.map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>
        <select value={appFilter} onChange={(e) => applyAppFilter(e.target.value)} aria-label="Application">
          <option value="">All applications</option>
          {appOptions.map((a) => (
            <option key={a.id} value={String(a.id)}>{a.name}</option>
          ))}
        </select>
        <label className="row small filter-check">
          <input type="checkbox" checked={onlineOnly} onChange={(e) => setOnlineOnly(e.target.checked)} />
          Online only
        </label>
        {filtersActive && (
          <button type="button" className="ghost small" onClick={clearFilters}>Clear</button>
        )}
        <button
          type="button"
          className="filter-add"
          onClick={() => setShowReg(true)}
          disabled={!can('instances', 'create')}
        >
          ＋ Register instance
        </button>
      </div>

      {(appFilter || projectFilter || stateFilter || tagFilter) && (
        <div className="active-filter">
          <span>Showing</span>
          {appFilter && (
            <span className="filter-chip">
              Application: {appOptions.find((a) => String(a.id) === appFilter)?.name || `#${appFilter}`}
            </span>
          )}
          {projectFilter && (
            <span className="filter-chip">
              Project: {projectLabel(projectFilter === '__global__' ? '' : projectFilter)}
            </span>
          )}
          {stateFilter && (
            <span className="filter-chip">State: {stateLabel(stateFilter)}</span>
          )}
          {tagFilter && (
            <span className="filter-chip">Tag: {tagFilter}</span>
          )}
          <span className="spacer" />
          <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>
        </div>
      )}

      <div className="row-toolbar">
        <span className="muted">
          {pageItems.length} of {total} instance{total === 1 ? '' : 's'}
        </span>
        {view !== 'topology' && pageItems.length > 0 && (
          <label className="bulk-select-page">
            <input type="checkbox" checked={pageAllSelected} onChange={togglePageSelection} />
            Select page
          </label>
        )}
        <div className="toolbar-right">
          <div className="seg" role="group" aria-label="View mode">
            <button
              type="button"
              className={'seg-btn' + (view === 'topology' ? ' active' : '')}
              onClick={() => changeView('topology')}
              title="Topology view"
            >
              <Network size={14} /> Topology
            </button>
            <button
              type="button"
              className={'seg-btn' + (view === 'list' ? ' active' : '')}
              onClick={() => changeView('list')}
              title="List view"
            >
              <List size={14} /> List
            </button>
            <button
              type="button"
              className={'seg-btn' + (view === 'cards' ? ' active' : '')}
              onClick={() => changeView('cards')}
              title="Card view"
            >
              <LayoutGrid size={14} /> Cards
            </button>
          </div>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="bulk-bar">
          <strong>{selected.size} selected</strong>
          <span className="muted">across {selected.size} host{selected.size === 1 ? '' : 's'}</span>
          <span className="spacer" />
          <button
            type="button"
            className="btn small"
            disabled={!can('commands', 'create') || !!bulkBusy || !selected.size}
            onClick={() => bulk('check')}
          >
            {bulkBusy === 'check' ? 'Queueing…' : 'Check selected'}
          </button>
          <button
            type="button"
            className="btn small warn"
            disabled={!can('commands', 'create') || !!bulkBusy || !selected.size}
            onClick={confirmBulkApply}
          >
            {bulkBusy === 'apply' ? 'Queueing…' : 'Apply selected'}
          </button>
          <button type="button" className="btn ghost small" onClick={() => setSelected(new Set())}>
            Clear
          </button>
        </div>
      )}

      {showReg && (
        <Modal title="Register an instance" onClose={closeReg}>
          {regResult ? (
            <div className="form-card">
              <p><strong>{regResult.instance.name}</strong> registered. The bootstrap token is only shown once — put it in the agent&apos;s config:</p>
              <pre className="token-pre">
                server: {`${location.origin || location.protocol + '//' + location.host}`}
                {'\n'}
                token: {regResult.token}
                {'\n'}
                instance_id: {regResult.instance.id}
              </pre>
              {installCommand && (
                <>
                  <p><strong>Install from this hub</strong> <CopyButton value={installCommand} title="Copy install command" /></p>
                  <pre className="token-pre">{installCommand}</pre>
                </>
              )}
              <div className="form-actions">
                <button className="ghost" onClick={() => { setRegResult(null); setRegError(''); }}>Register another</button>
                <button onClick={closeReg}>Done</button>
              </div>
            </div>
          ) : (
            <form className="form-card" onSubmit={addInstance}>
              <label>Instance name<input autoFocus value={regName} onChange={(e) => setRegName(e.target.value)} placeholder="e.g. web-prod-01" /></label>
              <label>Project<select required value={regProject} onChange={(e) => setRegProject(e.target.value)}><option value={DEFAULT_PROJECT}>default (global)</option>{regProject !== DEFAULT_PROJECT && !projects.some((p) => p.name === regProject) && <option value={regProject}>{regProject}</option>}{projects.map((p) => <option key={p.id} value={p.name}>{p.name}</option>)}</select><span className="sub">Required · scopes RBAC and the repository allowlist.</span></label>
              <div className="form-field">
                <span className="form-field-label">Region</span>
                <RegionPicker
                  value={regRegion}
                  onChange={setRegRegion}
                  regions={regions}
                  onRegionCreated={(region) => setRegions((prev) =>
                    [...prev.filter((r) => r.id !== region.id), region].sort((a, b) => a.name.localeCompare(b.name)))}
                />
                <span className="sub">
                  Optional · shown on the world map and used by filters.{' '}
                  <Link to="/regions">Manage regions</Link>
                </span>
              </div>
              <label>Tags<input value={regTags} onChange={(e) => setRegTags(e.target.value)} placeholder="e.g. prod, team-a" /><span className="sub">Optional · comma-separated labels for search and grouping.</span></label>
              {regError && <div className="error">{regError}</div>}
              <div className="form-actions">
                <button type="button" className="ghost" onClick={closeReg}>Cancel</button>
                <button type="submit" disabled={!regName.trim()}>Create</button>
              </div>
            </form>
          )}
        </Modal>
      )}

      {total === 0 ? (
        <div className="empty">
          <p className="muted">
            {filtersActive ? 'No instances match these filters.' : 'No instances yet. Register one to get started.'}
          </p>
        </div>
      ) : view === 'topology' ? (
        <>
          {topologyTotal > topologyFetched && (
            <p className="muted">
              Showing the first {topologyFetched} of {topologyTotal} matching hosts to keep the topology responsive.
            </p>
          )}
          <TopologyView
            instances={topologyInstances}
            emptyMessage="No instances match these filters."
          />
        </>
      ) : view === 'cards' ? (
        <div className="cards-grid">
          {pageItems.map((inst) => (
            <InstancesCard
              key={inst.id}
              instance={inst}
              onHostAutoHeal={toggleHostAutoHeal}
              onCheckAll={(i) => runAll(i, 'check')}
              onApplyAll={(i) => runAll(i, 'apply')}
              selected={selected.has(inst.id)}
              onSelect={toggleSelected}
            />
          ))}
        </div>
      ) : (
        <InstancesTree
          instances={pageItems}
          onHostAutoHeal={toggleHostAutoHeal}
          onCheckAll={(i) => runAll(i, 'check')}
          onApplyAll={(i) => runAll(i, 'apply')}
          can={can}
          selected={selected}
          onSelect={toggleSelected}
        />
      )}

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
      {view !== 'topology' && (
        <Pager total={total} limit={PAGE_SIZE} offset={pageOff} onPage={setPageOff} />
      )}
    </section>
  )
}
