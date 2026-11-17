import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ClipboardCheck, GitCommit, LayoutGrid, List, Network, Pencil, Play, History, Trash2, Variable } from 'lucide-react'
import { api, DEFAULT_PROJECT, projectForUi, projectForApi, projectLabel } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
import HealthBadge from '../components/HealthBadge.jsx'
import ApplicationCard from '../components/ApplicationCard.jsx'
import QuickMenu from '../components/QuickMenu.jsx'
import AutoHealChip from '../components/AutoHealChip.jsx'
import ApplicationHistory from '../components/ApplicationHistory.jsx'
import ApplicationDetail from '../components/ApplicationDetail.jsx'
import TopologyView from '../components/TopologyView.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const FETCH_ALL = 500
const MAX_INSTANCES = 10000

async function fetchAllInstances() {
  const all = []
  let offset = 0
  let total = Infinity
  while (all.length < total && all.length < MAX_INSTANCES) {
    const d = await api.instances(`?limit=${FETCH_ALL}&offset=${offset}`)
    const items = Array.isArray(d.items) ? d.items : []
    if (!items.length) break
    all.push(...items)
    total = Number.isFinite(Number(d.total)) ? Number(d.total) : all.length
    offset += items.length
  }
  return all
}

const emptyApp = { name: '', branch: 'main', path: 'site.yml', autoheal: false, sync_interval: 0, ignore_paths: '', project: DEFAULT_PROJECT }
const emptyRepo = { name: '', url: '', username: '', password: '', ssh_key: '', insecure_skip_verify: false }
const shortRepo = (url = '') => url.replace(/^https?:\/\//, '').replace(/^ssh:\/\//, '').replace(/\.git$/, '')

export default function Applications() {
  const { can } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [apps, setApps] = useState([])
  const [instances, setInstances] = useState([])
  const [repos, setRepos] = useState([])
  const [projects, setProjects] = useState([])
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(emptyApp)
  const [varsApp, setVarsApp] = useState(null)
  const [repoMode, setRepoMode] = useState('existing')
  const [repoId, setRepoId] = useState('')
  const [repo, setRepo] = useState(emptyRepo)
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const loadedRef = useRef(false)
  const [formError, setFormError] = useState('')
  const [confirm, setConfirm] = useState(null)
  const [historyApp, setHistoryApp] = useState(null)
  const [detailApp, setDetailApp] = useState(null)
  const [search, setSearch] = useState('')
  const [projectFilter, setProjectFilter] = useState(searchParams.get('project') || '')
  const [repoFilter, setRepoFilter] = useState(searchParams.get('repo') || '')
  const [view, setView] = useState(() => {
    const saved = localStorage.getItem('helios_apps_view')
    if (saved === 'topology' || saved === 'cards' || saved === 'table') return saved
    return 'cards'
  })
  const focusAppId = useMemo(() => {
    const raw = Number(searchParams.get('app'))
    return Number.isInteger(raw) && raw > 0 ? raw : null
  }, [searchParams])

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      const [a, r, inst] = await Promise.all([
        api.applications(),
        api.repositories().catch(() => []),
        fetchAllInstances().catch(() => []),
      ])
      setApps(a)
      setRepos(r)
      setInstances(inst)
      setProjects(await api.projects().catch(() => []))
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [])
  const modalOpen = showForm || !!confirm || !!varsApp || !!historyApp || !!detailApp
  useEffect(() => {
    refresh()
  }, [refresh])
  useLiveRefresh(refresh, { interval: 30000, enabled: !modalOpen })

  useEffect(() => {
    setProjectFilter(searchParams.get('project') || '')
    setRepoFilter(searchParams.get('repo') || '')
  }, [searchParams])

  const clearViewFilters = () => {
    setProjectFilter('')
    setRepoFilter('')
    setSearchParams({}, { replace: true })
  }

  const changeView = (v) => {
    setView(v)
    localStorage.setItem('helios_apps_view', v)
  }

  const changeSearch = (value) => {
    setSearch(value)
    if (focusAppId) {
      const next = new URLSearchParams(searchParams)
      next.delete('app')
      setSearchParams(next, { replace: true })
    }
  }

  const matchRepo = (url) => repos.find((r) => r.url === url)

  const openNew = () => {
    setEditing(null)
    setForm(emptyApp)
    setRepo(emptyRepo)
    const first = repos[0]
    setRepoId(first ? String(first.id) : '')
    setRepoMode(repos.length ? 'existing' : 'new')
    setFormError('')
    setShowForm(true)
  }
  const openEdit = (app) => {
    setEditing(app)
    setForm({ name: app.name, branch: app.branch, path: app.path, autoheal: app.autoheal, sync_interval: app.sync_interval || 0, ignore_paths: app.ignore_paths || '', project: projectForUi(app.project) })
    const m = matchRepo(app.repo_url)
    if (m) {
      setRepoId(String(m.id))
      setRepoMode('existing')
    } else {
      setRepoMode('new')
      setRepo({ ...emptyRepo, url: app.repo_url })
    }
    setFormError('')
    setShowForm(true)
  }
  const closeForm = () => {
    setShowForm(false)
    setEditing(null)
    setForm(emptyApp)
    setRepo(emptyRepo)
    setFormError('')
  }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true)
    setFormError('')
    try {
      let repoUrl
      if (repoMode === 'existing') {
        const r = repos.find((x) => String(x.id) === repoId)
        if (!r) throw new Error('select a repository')
        repoUrl = r.url
      } else {
        if (!repo.url.trim()) throw new Error('repo url is required')
        repoUrl = repo.url.trim()
        if (can('repositories', 'create')) {
          const body = {
            name: repo.name,
            url: repo.url,
            username: repo.username,
            insecure_skip_verify: repo.insecure_skip_verify,
          }
          if (repo.password) body.password = repo.password
          if (repo.ssh_key) body.ssh_key = repo.ssh_key
          const created = await api.createRepository(body)
          repoUrl = created.url
          refresh()
        }
      }
      if (form.sync_interval > 0 && form.sync_interval < 10) {
        throw new Error('sync interval must be 0 (inherit) or at least 10 seconds')
      }
      const app = { ...form, project: projectForApi(form.project), repo_url: repoUrl }
      if (editing) await api.updateApplication({ ...app, id: editing.id })
      else await api.createApplication(app)
      toast.success(editing ? 'Application updated' : 'Application created')
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const confirmDelete = (app) => setConfirm({
    title: 'Delete application',
    message: `Delete "${app.name}"? This cannot be undone.`,
    onConfirm: () => doDelete(app),
    variant: 'danger'
  })
  const doDelete = async (app) => {
    try {
      await api.deleteApplication(app.id)
      toast.success('Application deleted')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  const openDetail = (app) => setDetailApp(app)

  const appNodeIDs = (appID) =>
    instances.filter((i) => (i.apps || []).some((a) => a.application.id === appID)).map((i) => i.id)

  const runApp = async (app, action) => {
    const ids = appNodeIDs(app.id)
    if (!ids.length) {
      toast.info('No node is running this application')
      return
    }
    const label = action === 'apply' ? 'Apply' : 'Check'
    try {
      const results = await Promise.allSettled(
        ids.map((id) => (action === 'apply' ? api.sync(id, app.id) : api.check(id, app.id))),
      )
      const ok = results.filter((r) => r.status === 'fulfilled').length
      const failed = results.length - ok
      if (ok) toast.success(`${label} queued on ${ok} node${ok === 1 ? '' : 's'}`)
      if (failed) {
        const first = results.find((r) => r.status === 'rejected')
        toast.error(`${failed} node${failed === 1 ? '' : 's'} failed: ${first?.reason?.message || 'request failed'}`)
      }
      setTimeout(refresh, 800)
    } catch (e) {
      toast.error(e.message)
    }
  }
  const editFromDetail = (app) => {
    setDetailApp(null)
    openEdit(app)
  }
  const varsFromDetail = (app) => {
    setDetailApp(null)
    setVarsApp(app)
  }
  const historyFromDetail = (app) => {
    setDetailApp(null)
    setHistoryApp(app)
  }
  const deleteFromDetail = (app) => {
    setDetailApp(null)
    confirmDelete(app)
  }
  const unassignFromDetail = (app, inst) => setConfirm({
    title: 'Unassign application',
    message: `Unassign "${app.name}" from "${inst.name}"? The application stays on other instances.`,
    onConfirm: () => doUnassign(app, inst),
    variant: 'danger',
  })
  const doUnassign = async (app, inst) => {
    try {
      await api.unassign(inst.id, app.id)
      toast.success('Application unassigned')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }
  const assignFromDetail = async (app, inst) => {
    try {
      await api.assign(inst.id, app.id)
      toast.success('Application assigned')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const f = (k) => ({
    value: form[k],
    onChange: (e) => setForm((p) => ({ ...p, [k]: e.target.value })),
  })
  const rf = (k) => ({
    value: repo[k],
    onChange: (e) => setRepo((p) => ({ ...p, [k]: e.target.value })),
  })

  const instanceNames = useMemo(() => new Map(instances.map((i) => [i.id, i.name])), [instances])

  const appStats = useMemo(() => {
    const map = new Map()
    for (const app of apps) map.set(app.id, { instances: 0, synced: 0, unknown: 0, drift: 0, error: 0, stuck: 0, autoheal: 0, revision: '', revisionMsg: '', revisionAt: 0, lastAt: 0 })
    for (const inst of instances) {
      for (const a of inst.apps || []) {
        const s = map.get(a.application.id)
        if (!s) continue
        s.instances++
        if (a.stuck) s.stuck++
        if (a.autoheal) s.autoheal++
        const st = a.latest_check?.status
        if (st === 'ok') s.synced++
        else if (st === 'drift') s.drift++
        else if (st === 'failed' || st === 'error') s.error++
        else s.unknown++
        const checkedAt = new Date(a.last_check_at || a.latest_check?.finished_at || 0).getTime()
        if (checkedAt > s.lastAt) s.lastAt = checkedAt
        const run = a.latest_check
        if (run?.commit_after) {
          const at = new Date(run.finished_at || 0).getTime()
          if (at >= s.revisionAt) {
            s.revision = run.commit_after
            s.revisionMsg = run.commit_msg || ''
            s.revisionAt = at
          }
        }
      }
    }
    return map
  }, [apps, instances])

  const totals = useMemo(() => {
    let assigned = 0
    let drifting = 0
    let unhealthy = 0
    for (const s of appStats.values()) {
      assigned += s.instances
      drifting += s.drift
      unhealthy += s.error + s.stuck
    }
    return { assigned, drifting, unhealthy }
  }, [appStats])

  const focusApp = useMemo(
    () => (focusAppId ? apps.find((a) => a.id === focusAppId) : null),
    [apps, focusAppId],
  )

  const filteredApps = useMemo(() => {
    const q = search.trim().toLowerCase()
    return apps.filter((a) => {
      if (focusAppId && a.id !== focusAppId) return false
      if (projectFilter) {
        const p = a.project || ''
        if (projectFilter === '__global__' ? p !== '' : p !== projectFilter) return false
      }
      if (repoFilter && a.repo_url !== repoFilter) return false
      if (q && ![a.name, a.project, a.repo_url, a.branch, a.path].join(' ').toLowerCase().includes(q)) return false
      return true
    })
  }, [apps, search, projectFilter, repoFilter, focusAppId])

  const topologyInstances = useMemo(() => {
    const appIds = new Set(filteredApps.map((a) => a.id))
    return instances
      .map((inst) => ({
        ...inst,
        apps: (inst.apps || []).filter((a) => appIds.has(a.application.id)),
      }))
      .filter((inst) => inst.apps.length > 0)
  }, [instances, filteredApps])

  if (loading) {
    return (
      <section>
        <h1>Applications</h1>
        <div className="skeleton-grid">
          {[1, 2, 3, 4, 5].map(i => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  return (
    <section>
      <h1>Applications</h1>
      <p className="muted page-intro">
        Entry point: each application rolls out to the instances it is assigned to. Pick an application
        to see and operate its instances.
      </p>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{apps.length}</span><span className="stat-label">Applications</span></div>
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{totals.assigned}</span><span className="stat-label">Assignments</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{totals.drifting}</span><span className="stat-label">In drift</span></div>
        <div className="stat-card"><span className="stat-dot offline" /><span className="stat-num">{totals.unhealthy}</span><span className="stat-label">Failed apps</span></div>
        <div className="stats-note">Pull-based drift detection and self-healing</div>
      </div>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search application, project, repository…"
          value={search}
          onChange={(e) => changeSearch(e.target.value)}
        />
        <button type="button" className="filter-add" onClick={openNew} disabled={!can('applications', 'create')}>
          ＋ New application
        </button>
      </div>

      {(focusAppId || projectFilter || repoFilter) && (
        <div className="active-filter">
          <span>Showing</span>
          {focusAppId && (
            <span className="filter-chip">
              Application: {focusApp?.name || `#${focusAppId}`}
            </span>
          )}
          {projectFilter && (
            <span className="filter-chip">
              Project: {projectLabel(projectFilter === '__global__' ? '' : projectFilter)}
            </span>
          )}
          {repoFilter && <span className="filter-chip mono">{repoFilter}</span>}
          <span className="spacer" />
          <button type="button" className="ghost small" onClick={clearViewFilters}>Clear filters</button>
        </div>
      )}

      <div className="row-toolbar">
        <span className="muted">
          {filteredApps.length} of {apps.length} application{apps.length === 1 ? '' : 's'}
        </span>
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
              className={'seg-btn' + (view === 'cards' ? ' active' : '')}
              onClick={() => changeView('cards')}
              title="Card view"
            >
              <LayoutGrid size={14} /> Cards
            </button>
            <button
              type="button"
              className={'seg-btn' + (view === 'table' ? ' active' : '')}
              onClick={() => changeView('table')}
              title="List view"
            >
              <List size={14} /> List
            </button>
          </div>
        </div>
      </div>

      {showForm && (
        <Modal title={editing ? `Edit ${editing.name}` : 'New application'} onClose={closeForm} wide>
          <form className="form-card" onSubmit={submit}>
            <div className="two">
              <label>
                Name
                <input required autoFocus {...f('name')} />
              </label>
              <label>
                Project
                <select required value={form.project} onChange={(e) => setForm((p) => ({ ...p, project: e.target.value }))}>
                  <option value={DEFAULT_PROJECT}>default (global)</option>
                  {form.project !== DEFAULT_PROJECT && !projects.some((p) => p.name === form.project) && (
                    <option value={form.project}>{form.project}</option>
                  )}
                  {projects.map((p) => (
                    <option key={p.id} value={p.name}>{p.name}</option>
                  ))}
                </select>
                <span className="sub">Required · scopes RBAC and the repository allowlist.</span>
              </label>
            </div>

            <div className="sub" style={{ margin: '2px 0 6px' }}>
              Repository
              <div className="two" style={{ marginTop: 4 }}>
                <label className="row small">
                  <input type="radio" checked={repoMode === 'existing'} onChange={() => repoMode !== 'existing' && setRepoMode('existing')} />{' '}
                  Use existing
                </label>
                <label className="row small">
                  <input type="radio" checked={repoMode === 'new'} onChange={() => setRepoMode('new')} />{' '}
                  Create new
                </label>
              </div>
            </div>

            {repoMode === 'existing' ? (
              <label>
                <span className="sub">Repository</span>
                <select value={repoId} onChange={(e) => setRepoId(e.target.value)}>
                  {repos.map((r) => (
                    <option key={r.id} value={r.id}>{r.name} — {r.url}</option>
                  ))}
                </select>
              </label>
            ) : (
              <fieldset className="repo-fields">
                <div className="two">
                  <label>
                    URL
                    <input required placeholder="https://github.com/you/infra.git" {...rf('url')} />
                  </label>
                  <label>
                    Name (optional)
                    <input placeholder="defaults to url" {...rf('name')} />
                  </label>
                </div>
                <div className="two">
                  <label>
                    Username
                    <input {...rf('username')} />
                  </label>
                  <label>
                    Password / token
                    <input type="password" {...rf('password')} />
                  </label>
                </div>
                <label>
                  SSH private key (deploy key)
                  <textarea rows={3} spellCheck={false} {...rf('ssh_key')} />
                </label>
                <label className="row">
                  <input type="checkbox" checked={repo.insecure_skip_verify} onChange={(e) => setRepo((p) => ({ ...p, insecure_skip_verify: e.target.checked }))} />{' '}
                  Insecure skip verify
                </label>
              </fieldset>
            )}

            <div className="two">
              <label>
                Revision (branch, tag or semver range, e.g. {'>=1.2.0 <2.0.0'})
                <input placeholder={"main, v1.2.0, >=1.0.0"} {...f('branch')} />
              </label>
              <label>
                Playbook path (file or dir with site.yml)
                <input {...f('path')} />
              </label>
            </div>
            <label className="row" title="Automatically apply when a check detects drift">
              <input type="checkbox" checked={form.autoheal} onChange={(e) => setForm((p) => ({ ...p, autoheal: e.target.checked }))} />{' '}
              Auto-apply on drift (apply on detection)
            </label>
            <label title="How often to check this application for drift">
              Check interval (seconds)
              <input type="number" min="0" step="10" value={form.sync_interval} onChange={(e) => setForm((p) => ({ ...p, sync_interval: Number(e.target.value) || 0 }))} />
              <span className="sub">0 = inherit the agent poll interval (otherwise min 10s)</span>
            </label>
            <label>
              Ignore drift paths / task patterns (one per line)
              <textarea rows={3} spellCheck={false} placeholder="copy /etc/motd&#10;write /etc/foo/*&#10;prod-*" value={form.ignore_paths} onChange={(e) => setForm((p) => ({ ...p, ignore_paths: e.target.value }))} />
              <span className="sub">Glob patterns matched against changed task names, hosts and resource targets.</span>
            </label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions">
              <button type="button" className="ghost" onClick={closeForm}>Cancel</button>
              <button type="submit" disabled={saving}>
                {saving ? 'Saving…' : editing ? 'Save' : 'Create'}
              </button>
            </div>
          </form>
        </Modal>
      )}

      {filteredApps.length === 0 ? (
        <div className="empty">
          <p className="muted">
            {apps.length === 0
              ? 'No applications yet. Create one to get started.'
              : 'No applications match this search.'}
          </p>
        </div>
      ) : view === 'topology' ? (
        <TopologyView
          instances={topologyInstances}
          emptyMessage="No assignment matches these filters."
        />
      ) : view === 'cards' ? (
        <div className="cards-grid">
          {filteredApps.map((a) => (
            <ApplicationCard
              key={a.id}
              app={a}
              stats={appStats.get(a.id)}
              repo={repos.find((r) => r.url === a.repo_url)}
              focused={a.id === focusAppId}
              onDetail={openDetail}
              onHistory={() => setHistoryApp(a)}
              can={can}
              onCheckAll={() => runApp(a, 'check')}
              onApplyAll={() => runApp(a, 'apply')}
              onEdit={openEdit}
              onDelete={confirmDelete}
              onVars={setVarsApp}
            />
          ))}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid summary-table">
            <thead>
              <tr>
                <th>Application</th>
                <th style={{ width: '150px' }}>Project</th>
                <th style={{ width: '150px' }}>Nodes</th>
                <th style={{ width: '170px' }}>State</th>
                <th style={{ width: '190px' }}>Auto-heal</th>
                <th style={{ width: '130px' }}>Revision</th>
                <th style={{ width: '130px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filteredApps.map((a) => {
                const st = appStats.get(a.id) || {}
                const s = { instances: 0, synced: 0, unknown: 0, drift: 0, error: 0, stuck: 0, autoheal: 0, ...st }
                const unhealthy = s.error + s.stuck
                const health = unhealthy > 0 ? 'failed' : s.drift > 0 ? 'drift' : s.instances === 0 || s.synced < s.instances ? 'unknown' : 'ok'
                const healthLabel = unhealthy > 0
                  ? `${unhealthy} failed`
                  : s.drift > 0
                    ? `${s.drift} drifted`
                    : s.instances === 0
                      ? 'Unassigned'
                      : s.synced === s.instances
                        ? 'Synced'
                        : `${s.synced}/${s.instances} synced`
                const canRun = can('commands', 'create') && s.instances > 0
                const menu = [
                  { label: 'View nodes', icon: Network, to: `/instances?app=${a.id}` },
                  { label: 'Check all nodes', icon: ClipboardCheck, onClick: () => runApp(a, 'check'), disabled: !canRun },
                  { label: 'Apply to all nodes', icon: Play, onClick: () => runApp(a, 'apply'), disabled: !canRun },
                  { label: 'History', icon: History, onClick: () => setHistoryApp(a) },
                  { separator: true },
                  { label: 'Edit', icon: Pencil, onClick: () => openEdit(a), disabled: !can('applications', 'update') },
                  { label: 'Variables', icon: Variable, onClick: () => setVarsApp(a), disabled: !can('applications', 'update') },
                  { separator: true },
                  { label: 'Delete', icon: Trash2, onClick: () => confirmDelete(a), disabled: !can('applications', 'delete'), danger: true },
                ]
                return (
                  <tr key={a.id} id={`app-${a.id}`} className={a.id === focusAppId ? 'is-focused' : undefined}>
                    <td>
                      <div className="cell-title">
                        <button
                          type="button"
                          className="app-name-link"
                          onClick={() => openDetail(a)}
                          title="Open application details"
                        >
                          <strong>{a.name}</strong>
                        </button>
                        <span className="cell-sub">
                          <span className="mono">id {a.id}</span>
                          <span className="mono repo-cell" title={a.repo_url}>{shortRepo(a.repo_url)}</span>
                        </span>
                      </div>
                    </td>
                    <td><span className="project-badge">{projectLabel(a.project)}</span></td>
                    <td>
                      <div className="cell-title">
                        <strong className="mono">{s.synced}/{s.instances}</strong>
                        <span className="cell-sub">
                          {s.instances} node{s.instances === 1 ? '' : 's'}
                          {s.unknown ? ` · ${s.unknown} unknown` : ''}
                        </span>
                      </div>
                    </td>
                    <td><HealthBadge state={health} label={healthLabel} size={13} small /></td>
                    <td>
                      <AutoHealChip
                        enabled={!!a.autoheal}
                        scope="app"
                        label={a.autoheal ? `on · ${s.autoheal}/${s.instances} hosts` : 'off'}
                        reason={a.autoheal ? 'Drift is applied automatically on hosts where auto-heal is enabled' : 'Drift is detected but never applied automatically'}
                      />
                    </td>
                    <td>
                      <button
                        type="button"
                        className="rev-link"
                        onClick={() => setHistoryApp(a)}
                        title={s.revisionMsg || 'View revision history'}
                      >
                        <GitCommit size={12} />
                        <span className="mono">{s.revision ? s.revision.slice(0, 7) : 'History'}</span>
                      </button>
                      <div className="cell-sub mono">{a.branch}</div>
                    </td>
                    <td>
                      <div className="row-actions">
                        <Link className="btn ghost small" to={`/instances?app=${a.id}`}>Open</Link>
                        <QuickMenu label={`Actions for ${a.name}`} items={menu} />
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {detailApp && (
        <ApplicationDetail
          app={detailApp}
          stats={appStats.get(detailApp.id)}
          repo={repos.find((r) => r.url === detailApp.repo_url)}
          instances={instances}
          can={can}
          onClose={() => setDetailApp(null)}
          onEdit={editFromDetail}
          onVars={varsFromDetail}
          onHistory={historyFromDetail}
          onDelete={deleteFromDetail}
          onUnassign={(inst) => unassignFromDetail(detailApp, inst)}
          onAssign={(inst) => assignFromDetail(detailApp, inst)}
        />
      )}

      {historyApp && (
        <ApplicationHistory
          app={historyApp}
          instancesById={instanceNames}
          onClose={() => setHistoryApp(null)}
        />
      )}

      {varsApp && (
        <VariableEditor
          title={`Variables · ${varsApp.name}`}
          subtitle="Application defaults, shared by every assigned host (≈ group_vars). Per-host overrides live on the instance."
          load={() => api.appVariables(varsApp.id)}
          save={(items) => api.saveAppVariables(varsApp.id, items)}
          onClose={() => setVarsApp(null)}
        />
      )}

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
    </section>
  )
}