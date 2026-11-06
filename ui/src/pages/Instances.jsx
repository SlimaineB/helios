import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { api, stateLabel, DEFAULT_PROJECT, projectForApi, projectLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import Pager from '../components/Pager.jsx'
import CopyButton from '../components/CopyButton.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const PAGE_SIZE = 20
const FETCH_ALL = 500

const online = (inst) =>
  inst.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000

const fmtTime = (t) => (t ? new Date(t).toLocaleTimeString() : '—')

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

function appState(app) {
  const s = app?.latest_check?.status
  return s === 'failed' ? 'error' : s || 'unknown'
}

function instanceMatches(inst, q) {
  return !q || [inst.name, inst.id, inst.project].join(' ').toLowerCase().includes(q)
}

function appMatches(inst, app, { appFilter, stateFilter, q }) {
  if (appFilter && String(app.application.id) !== appFilter) return false
  if (stateFilter && appState(app) !== stateFilter) return false
  if (q && !instanceMatches(inst, q) && !app.application.name.toLowerCase().includes(q)) return false
  return true
}

function InstancesTree({ instances, onAction, onDiff, onLogs, onVars, onUnassign, can, collapsed, toggle, pendingFor, appFilter, search, stateFilter }) {
  const q = search.trim().toLowerCase()
  const appsFor = (inst) => inst.apps.filter((a) => appMatches(inst, a, { appFilter, stateFilter, q }))

  const groups = useMemo(() => {
    const map = new Map()
    for (const inst of instances) {
      const key = inst.project || ''
      if (!map.has(key)) map.set(key, [])
      map.get(key).push(inst)
    }
    return [...map.entries()].sort((a, b) => (a[0] || '~').localeCompare(b[0] || '~'))
  }, [instances])

  return (
    <div className="table-wrap">
      <table className="grid tree-table">
        <thead>
          <tr>
            <th style={{ width: '36px' }} aria-label="Tree" />
            <th style={{ width: '36px' }} aria-label="Status" />
            <th>Instance / application</th>
            <th style={{ width: '110px' }}>State</th>
            <th style={{ width: '120px' }}>Last check</th>
            <th>Details</th>
            <th style={{ width: '300px' }}>Actions</th>
          </tr>
        </thead>
        {groups.map(([project, list]) => {
          const pkey = `p:${project}`
          const pOpen = !collapsed.has(pkey)
          const appCount = list.reduce((n, i) => n + appsFor(i).length, 0)
          return (
            <tbody key={pkey}>
              <tr className="tree-group">
                <td colSpan={7}>
                  <button
                    type="button"
                    className="tree-toggle"
                    onClick={() => toggle(pkey)}
                    aria-expanded={pOpen}
                  >
                    {pOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                  </button>
                  <span className="tree-name">{projectLabel(project)}</span>
                  <span className="tree-count">
                    {list.length} instance{list.length === 1 ? '' : 's'} · {appCount} app{appCount === 1 ? '' : 's'}
                  </span>
                </td>
              </tr>

              {pOpen && list.map((inst) => {
                const ikey = `i:${inst.id}`
                const iOpen = !collapsed.has(ikey)
                const apps = appsFor(inst)
                return (
                  <Fragment key={ikey}>
                    <tr className="tree-instance">
                      <td>
                        <button
                          type="button"
                          className="tree-toggle"
                          onClick={() => toggle(ikey)}
                          aria-expanded={iOpen}
                          aria-label={iOpen ? `Collapse ${inst.name}` : `Expand ${inst.name}`}
                        >
                          {iOpen ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                        </button>
                      </td>
                      <td>
                        <span
                          className={`status-dot ${online(inst) ? 'online' : 'offline'}`}
                          title={online(inst) ? 'online' : 'offline'}
                        />
                      </td>
                      <td>
                        <div className="cell-title">
                          <Link to={`/instances/${inst.id}`} className="instance-link">
                            <strong>{inst.name}</strong>
                          </Link>
                          <span className="cell-sub">
                            <CopyButton value={String(inst.id)} size="small" title="Copy instance ID" />
                            <span className="mono">{inst.id}</span>
                          </span>
                        </div>
                      </td>
                      <td>
                        <span className={`state-badge ${inst.state}`}>{stateLabel(inst.state)}</span>
                      </td>
                      <td className="muted">{fmtTime(inst.last_check_at)}</td>
                      <td>
                        <span className="muted">
                          {apps.length
                            ? `${apps.length} application${apps.length === 1 ? '' : 's'}`
                            : 'no application'}
                        </span>
                      </td>
                      <td>
                        <Link className="btn ghost small" to={`/instances/${inst.id}`}>Open</Link>
                      </td>
                    </tr>

                    {iOpen && apps.map((app) => {
                      const pending = pendingFor(inst.id, app.application.id)
                      return (
                      <tr className="tree-app" key={`${inst.id}-${app.application.id}`}>
                        <td />
                        <td>
                          <span
                            className={`status-dot ${appState(app)}`}
                            title={appState(app)}
                          />
                        </td>
                        <td>
                          <div className="cell-title">
                            <div className="app-name">
                              <Link className="entity-link" to={`/instances?app=${app.application.id}`}>
                                {app.application.name}
                              </Link>
                              {app.stuck && <Badge state="stuck" label="stuck" small />}
                            </div>
                            {app.latest_check?.commit_after && (
                              <span className="cell-sub">
                                <span className="mono">{app.latest_check.commit_after.slice(0, 7)}</span>
                                {app.latest_check.commit_msg && (
                                  <span className="commit-msg">{app.latest_check.commit_msg}</span>
                                )}
                              </span>
                            )}
                          </div>
                        </td>
                        <td>
                          <span className={`state-badge ${appState(app)}`}>{stateLabel(appState(app))}</span>
                        </td>
                        <td className="muted">{fmtTime(app.last_check_at || app.latest_check?.finished_at)}</td>
                        <td>
                          {app.latest_check?.changed_tasks?.length > 0 ? (
                            <div className="drift-tasks">
                              {app.latest_check.changed_tasks.map((t, i) => (
                                <span
                                  key={i}
                                  className="drift-chip"
                                  title={t.host ? `host: ${t.host}` : undefined}
                                >
                                  {t.task}
                                </span>
                              ))}
                            </div>
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </td>
                        <td>
                          <div className="app-actions">
                            {app.stuck ? (
                              <button
                                className="btn warn small"
                                disabled={!can('instances', 'update') || app.autoheal}
                                onClick={() => onAction(inst.id, app.application.id, 'rearm')}
                                title="Re-enable autoheal once the playbook is fixed"
                              >
                                Rearm
                              </button>
                            ) : (
                              <>
                                <label className="switch" title="Auto-heal / re-apply on drift detection">
                                  <input
                                    type="checkbox"
                                    checked={app.autoheal}
                                    disabled={!can('instances', 'update')}
                                    onChange={() => onAction(inst.id, app.application.id, 'toggle-autoheal')}
                                  />
                                  <span className="slider" />
                                </label>
                                <span className={`sub ${app.autoheal ? '' : 'muted'}`}>
                                  {app.autoheal ? (app.autoheal_set ? 'on (override)' : 'on') : 'off'}
                                </span>
                              </>
                            )}
                            <button
                              className="btn small"
                              disabled={!can('commands', 'create') || !!pending}
                              aria-busy={pending?.action === 'apply'}
                              onClick={() => onAction(inst.id, app.application.id, 'apply')}
                            >
                              {pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}
                              Sync
                            </button>
                            <button
                              className="btn ghost small"
                              disabled={!can('commands', 'create') || !!pending}
                              aria-busy={pending?.action === 'check'}
                              onClick={() => onAction(inst.id, app.application.id, 'check')}
                            >
                              {pending?.action === 'check' && <span className="spinner xs" aria-hidden="true" />}
                              Check
                            </button>
                            {app.latest_check?.id && (
                              <>
                                <button
                                  className="btn ghost small"
                                  title="Pre-apply diff"
                                  onClick={() => onDiff(app.latest_check.id)}
                                >
                                  Diff
                                </button>
                                <button
                                  className="btn ghost small"
                                  title="Full run log"
                                  onClick={() => onLogs(app.latest_check.id)}
                                >
                                  Logs
                                </button>
                              </>
                            )}
                            <button
                              className="btn ghost small"
                              disabled={!can('instances', 'update')}
                              onClick={() => onVars({ inst, app })}
                            >
                              Vars
                            </button>
                            <button
                              className="btn danger small"
                              disabled={!can('instances', 'update')}
                              onClick={() => onUnassign(inst, app)}
                              title="Remove this application from this instance only"
                            >
                              Unassign
                            </button>
                          </div>
                        </td>
                      </tr>
                      )
                    })}

                    {iOpen && !apps.length && (
                      <tr className="tree-app tree-empty">
                        <td />
                        <td />
                        <td colSpan={5} className="muted">
                          {appFilter || stateFilter || (q && !instanceMatches(inst, q))
                            ? 'No application matches these filters.'
                            : 'No application assigned — assign one from the Applications page.'}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          )
        })}
      </table>
    </div>
  )
}

export default function Instances() {
  const { can } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [instances, setInstances] = useState([])
  const [loading, setLoading] = useState(true)
  const loadedRef = useRef(false)
  const [showReg, setShowReg] = useState(false)
  const [regName, setRegName] = useState('')
  const [regProject, setRegProject] = useState(DEFAULT_PROJECT)
  const [projects, setProjects] = useState([])
  const [regResult, setRegResult] = useState(null)
  const [regError, setRegError] = useState('')
  const [downloads, setDownloads] = useState([])
  const [busy, setBusy] = useState({})
  const [runModal, setRunModal] = useState(null)
  const [diffModal, setDiffModal] = useState(null)
  const [varsApp, setVarsApp] = useState(null)
  const [confirm, setConfirm] = useState(null)
  const [pageOff, setPageOff] = useState(0)
  const [collapsed, setCollapsed] = useState(() => new Set())

  const [search, setSearch] = useState('')
  const [stateFilter, setStateFilter] = useState('')
  const [projectFilter, setProjectFilter] = useState(searchParams.get('project') || '')
  const [appFilter, setAppFilter] = useState(searchParams.get('app') || '')
  const [onlineOnly, setOnlineOnly] = useState(false)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      setInstances(await fetchAllInstances())
      setProjects(await api.projects().catch(() => []))
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [])

  const commands = useCommandRunner(refresh)
  const modalOpen = showReg || !!runModal || !!diffModal || !!varsApp || !!confirm
  useEffect(() => {
    refresh()
  }, [refresh])
  useEffect(() => {
    if (modalOpen) return
    const t = setInterval(() => { if (!document.hidden) refresh() }, 10000)
    return () => clearInterval(t)
  }, [refresh, modalOpen])

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
    setCollapsed(new Set())
  }, [search, stateFilter, projectFilter, appFilter, onlineOnly])

  const addInstance = async (e) => {
    e.preventDefault()
    setRegError('')
    try {
      const res = await api.createInstance(regName.trim(), projectForApi(regProject))
      setRegResult(res)
      setRegName('')
      setRegProject(DEFAULT_PROJECT)
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
    setRegError('')
  }

  useEffect(() => {
    if (!showReg) return
    api.downloads().then((d) => setDownloads(d.files || [])).catch(() => setDownloads([]))
  }, [showReg])

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

  const act = async (instId, appId, action) => {
    if (action === 'apply' || action === 'check') {
      commands.run(instId, appId, action)
      return
    }
    const key = `${instId}:${appId}:${action}`
    setBusy((b) => ({ ...b, [key]: true }))
    try {
      if (action === 'toggle-autoheal') {
        const cur = instances
          .find((i) => i.id === instId)
          ?.apps?.find((a) => a.application.id === appId)?.autoheal
        await api.setAutoHeal(instId, appId, !cur)
      } else if (action === 'rearm') {
        await api.setAutoHeal(instId, appId, true)
      }
      toast.success('Done')
    } catch (e) {
      toast.error(e.message)
    } finally {
      setBusy((b) => ({ ...b, [key]: false }))
      refresh()
    }
  }

  const confirmUnassign = (inst, app) => setConfirm({
    title: 'Unassign application',
    message: `Unassign "${app.application.name}" from "${inst.name}"? The application stays on other instances.`,
    onConfirm: () => doUnassign(inst, app),
    variant: 'danger',
  })

  const doUnassign = async (inst, app) => {
    try {
      await api.unassign(inst.id, app.application.id)
      toast.success('Application unassigned')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  const projectOptions = useMemo(() => {
    const set = new Set()
    for (const i of instances) set.add(i.project || '')
    return [...set].sort()
  }, [instances])

  const appOptions = useMemo(() => {
    const map = new Map()
    for (const i of instances) {
      for (const a of i.apps) map.set(a.application.id, a.application.name)
    }
    return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [instances])

  const q = search.trim().toLowerCase()

  const filtered = useMemo(() => {
    return instances.filter((inst) => {
      if (stateFilter && inst.state !== stateFilter
        && !inst.apps.some((a) => appState(a) === stateFilter)) return false
      if (projectFilter) {
        const p = inst.project || ''
        if (projectFilter === '__global__' ? p !== '' : p !== projectFilter) return false
      }
      if (appFilter && !inst.apps.some((a) => String(a.application.id) === appFilter)) return false
      if (onlineOnly && !online(inst)) return false
      if (q && !instanceMatches(inst, q)
        && !inst.apps.some((a) => a.application.name.toLowerCase().includes(q))) return false
      return true
    })
  }, [instances, q, stateFilter, projectFilter, appFilter, onlineOnly])

  const visibleApps = useMemo(
    () => filtered.flatMap((inst) => inst.apps.filter((a) => appMatches(inst, a, { appFilter, stateFilter, q }))),
    [filtered, appFilter, stateFilter, q]
  )

  const stats = {
    total: filtered.length,
    ok: visibleApps.filter((a) => appState(a) === 'ok').length,
    drift: visibleApps.filter((a) => appState(a) === 'drift').length,
    offline: filtered.filter((i) => !online(i)).length,
  }

  const toggleCollapse = (key) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  const pageItems = filtered.slice(pageOff, pageOff + PAGE_SIZE)
  const filtersActive = !!(search || stateFilter || projectFilter || appFilter || onlineOnly)
  const clearFilters = () => {
    setSearch('')
    setStateFilter('')
    setProjectFilter('')
    setAppFilter('')
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
        <div className="stats-note">App states from latest checks · agents poll every 10s</div>
      </div>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search name, id, project or application…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
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
          <option value="error">Error</option>
          <option value="unknown">Unknown</option>
        </select>
        <select value={projectFilter} onChange={(e) => applyProjectFilter(e.target.value)} aria-label="Project">
          <option value="">All projects</option>
          <option value="__global__">Global</option>
          {projectOptions.filter(Boolean).map((p) => (
            <option key={p} value={p}>{p}</option>
          ))}
        </select>
        <select value={appFilter} onChange={(e) => applyAppFilter(e.target.value)} aria-label="Application">
          <option value="">All applications</option>
          {appOptions.map(([id, name]) => (
            <option key={id} value={String(id)}>{name}</option>
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

      {(appFilter || projectFilter || stateFilter) && (
        <div className="active-filter">
          <span>Showing</span>
          {appFilter && (
            <span className="filter-chip">
              Application: {appOptions.find(([id]) => String(id) === appFilter)?.[1] || `#${appFilter}`}
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
          <span className="spacer" />
          <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>
        </div>
      )}

      <div className="row-toolbar">
        <span className="muted">
          {filtered.length} of {instances.length} instance{instances.length === 1 ? '' : 's'}
        </span>
      </div>

      {showReg && (
        <Modal title="Register a new agent" onClose={closeReg}>
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
              {regError && <div className="error">{regError}</div>}
              <div className="form-actions">
                <button type="button" className="ghost" onClick={closeReg}>Cancel</button>
                <button type="submit" disabled={!regName.trim()}>Create</button>
              </div>
            </form>
          )}
        </Modal>
      )}

      {pageItems.length === 0 ? (
        <div className="empty">
          <p className="muted">
            {filtersActive ? 'No instances match these filters.' : 'No instances yet. Register one to get started.'}
          </p>
        </div>
      ) : (
        <InstancesTree
          instances={pageItems}
          onAction={act}
          onDiff={(runId) => setDiffModal(runId)}
          onLogs={(runId) => setRunModal({ runId, view: 'text' })}
          onVars={setVarsApp}
          onUnassign={confirmUnassign}
          can={can}
          collapsed={collapsed}
          toggle={toggleCollapse}
          pendingFor={(instId, appId) => commands.pending[`${instId}:${appId}`] || null}
          appFilter={appFilter}
          search={search}
          stateFilter={stateFilter}
        />
      )}

      {runModal && <LogsModal runId={runModal.runId} initialView={runModal.view} onClose={() => setRunModal(null)} />}
      {diffModal && <DiffModal runId={diffModal} onClose={() => setDiffModal(null)} />}
      {varsApp && <VariableEditor title={`Host variables · ${varsApp.inst.name}`} subtitle={`Overrides for ${varsApp.app.application.name} on this host only (≈ host_vars); win over the app defaults.`} load={() => api.assignmentVariables(varsApp.inst.id, varsApp.app.application.id)} save={(items) => api.saveAssignmentVariables(varsApp.inst.id, varsApp.app.application.id, items)} onClose={() => setVarsApp(null)} />}
      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
      <Pager total={filtered.length} limit={PAGE_SIZE} offset={pageOff} onPage={setPageOff} />
    </section>
  )
}
