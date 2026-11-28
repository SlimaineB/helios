import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { LayoutGrid, List } from 'lucide-react'
import { api, instanceOnline, projectLabel, stateLabel } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
import SearchBox from '../components/SearchBox.jsx'
import TagList from '../components/TagList.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from '../components/GitOpsBadge.jsx'
import ProjectCard from '../components/ProjectCard.jsx'
import { matchesSearch } from '../search.js'
import { useUrlState } from '../hooks/useUrlState.js'

const empty = { name: '', repo_patterns: '' }

const GLOBAL_PROJECT = {
  id: '__default__',
  name: 'default (global)',
  key: '',
  repo_patterns: '',
  system: true,
}

const normalizeState = (state) => {
  if (state === 'failed') return 'error'
  return state || 'unknown'
}

const INSTANCE_SUMMARY_CAP = 5000

async function fetchInstanceSummaries() {
  const limit = 500
  let offset = 0
  let total = 0
  const items = []
  do {
    const page = await api.instances(`?limit=${limit}&offset=${offset}&compact=1`)
    const rows = Array.isArray(page.items) ? page.items : []
    items.push(...rows)
    total = Number(page.total) || 0
    offset += rows.length
    if (!rows.length || items.length >= total || items.length >= INSTANCE_SUMMARY_CAP) break
  } while (items.length < total && items.length < INSTANCE_SUMMARY_CAP)
  return { items, total, truncated: total > items.length }
}

function buildSummaries(projects, applications, instances) {
  const map = new Map()
  const projectByName = new Map(projects.map((project) => [project.name, project]))

  const ensure = (key) => {
    let summary = map.get(key)
    if (!summary) {
      const project = key ? projectByName.get(key) || {
        id: `project:${key}`,
        name: key,
        repo_patterns: '',
        created_at: '',
      } : GLOBAL_PROJECT
      summary = {
        key,
        project,
        appIds: new Set(),
        instances: 0,
        online: 0,
        tags: new Map(),
        states: new Set(),
        search: [project.name, project.repo_patterns || ''],
      }
      map.set(key, summary)
    }
    return summary
  }

  ensure('')
  for (const project of projects) ensure(project.name)

  for (const app of applications) {
    const summary = ensure(app.project || '')
    summary.appIds.add(app.id)
    summary.search.push(app.name)
  }

  for (const inst of instances) {
    const summary = ensure(inst.project || '')
    const isOnline = instanceOnline(inst)
    summary.instances++
    if (isOnline) summary.online++
    summary.states.add(normalizeState(!isOnline ? 'offline' : inst.state))
    summary.search.push(inst.name, inst.id, inst.region || '', ...(inst.tags || []))
    for (const tag of inst.tags || []) {
      const key = tag.toLowerCase()
      if (!summary.tags.has(key)) summary.tags.set(key, tag)
    }
    for (const app of inst.apps || []) {
      if (app.application) {
        summary.appIds.add(app.application.id)
        summary.search.push(app.application.name)
      }
      summary.states.add(normalizeState(app.latest_check?.status))
    }
  }

  return [...map.values()].map((summary) => {
    const tags = [...summary.tags.values()].sort((a, b) => a.localeCompare(b))
    return {
      ...summary,
      apps: summary.appIds.size,
      tags,
      tagKeys: new Set(tags.map((tag) => tag.toLowerCase())),
      searchText: summary.search.join(' ').toLowerCase(),
    }
  })
}

function ProjectTable({ rows, onEdit, onDelete, can }) {
  return (
    <div className="table-wrap">
      <table className="grid summary-table">
        <thead>
          <tr>
            <th>Project</th>
            <th style={{ width: '30%' }}>Allowed repositories</th>
            <th style={{ width: '90px' }}>Apps</th>
            <th style={{ width: '120px' }}>Instances</th>
            <th style={{ width: '130px' }}>Created</th>
            <th style={{ width: '250px' }}>Actions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((summary) => {
            const project = summary.project
            const readOnly = !project.system && managedByGitOps(project)
            return (
              <tr key={summary.key || '__global__'} className="summary-row">
                <td>
                  <div className="cell-title">
                    <strong>{project.name}</strong>
                    {project.system && <span className="project-badge">system</span>}
                    {!project.system && <GitOpsBadge obj={project} />}
                    <span className="cell-sub">
                      {summary.tags.length > 0 ? <TagList tags={summary.tags} /> : <span className="muted">no instance tags</span>}
                    </span>
                  </div>
                </td>
                <td>
                  {project.repo_patterns ? (
                    <span className="mono project-pattern-cell" title={project.repo_patterns}>{project.repo_patterns}</span>
                  ) : (
                    <span className="muted">any repository</span>
                  )}
                </td>
                <td>
                  <strong className="mono">{summary.apps}</strong>
                </td>
                <td>
                  <div className="cell-title">
                    <strong className="mono">{summary.instances}</strong>
                    <span className="cell-sub">{summary.online} online · {summary.instances - summary.online} offline</span>
                  </div>
                </td>
                <td className="muted">
                  {project.created_at ? new Date(project.created_at).toLocaleDateString() : '—'}
                </td>
                <td>
                  <div className="row-actions">
                    <Link className="btn ghost small" to={`/instances?project=${encodeURIComponent(project.system ? '__global__' : project.name)}`}>Instances</Link>
                    <Link className="btn ghost small" to={`/applications?project=${encodeURIComponent(project.system ? '__global__' : project.name)}`}>Applications</Link>
                    {!project.system && (
                      <>
                        <button className="btn ghost small" disabled={readOnly || !can('projects', 'update')} title={managedTitle(project)} onClick={() => onEdit(project)}>Edit</button>
                        <button className="btn danger small" disabled={readOnly || !can('projects', 'delete')} title={managedTitle(project)} onClick={() => onDelete(project)}>Delete</button>
                      </>
                    )}
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

export default function Projects() {
  const { can } = useAuth()
  const [projects, setProjects] = useState([])
  const [applications, setApplications] = useState([])
  const [instances, setInstances] = useState([])
  const [instanceTotal, setInstanceTotal] = useState(0)
  const [instancesTruncated, setInstancesTruncated] = useState(false)
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)
  const [list, setList, resetList] = useUrlState(() => ({
    q: '',
    state: '',
    project: '',
    tag: '',
    app: '',
    online: false,
    view: localStorage.getItem('helios_projects_view') === 'list' ? 'list' : 'cards',
  }))
  const { q: search, state: stateFilter, project: projectFilter, tag: tagFilter, app: appFilter, online: onlineOnly, view } = list

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const [projectList, appList, instancePage] = await Promise.all([
        api.projects(),
        api.applications().catch(() => []),
        fetchInstanceSummaries().catch(() => ({ items: [], total: 0, truncated: false })),
      ])
      setProjects(projectList)
      setApplications(appList)
      setInstances(instancePage.items)
      setInstanceTotal(instancePage.total)
      setInstancesTruncated(instancePage.truncated)
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const summaries = useMemo(
    () => buildSummaries(projects, applications, instances),
    [projects, applications, instances],
  )

  const projectOptions = useMemo(
    () => summaries
      .filter((summary) => summary.key)
      .map((summary) => summary.key)
      .sort((a, b) => a.localeCompare(b)),
    [summaries],
  )

  const tagOptions = useMemo(() => {
    const tags = new Map()
    for (const summary of summaries) {
      for (const tag of summary.tags) tags.set(tag.toLowerCase(), tag)
    }
    if (tagFilter && !tags.has(tagFilter.toLowerCase())) tags.set(tagFilter.toLowerCase(), tagFilter)
    return [...tags.values()].sort((a, b) => a.localeCompare(b))
  }, [summaries, tagFilter])

  const appOptions = useMemo(() => {
    const list = [...applications].sort((a, b) => a.name.localeCompare(b.name))
    if (appFilter && !list.some((app) => String(app.id) === appFilter)) {
      list.unshift({ id: Number(appFilter), name: `#${appFilter}` })
    }
    return list
  }, [applications, appFilter])

  const filtered = useMemo(() => summaries.filter((summary) => {
    if (projectFilter) {
      if (projectFilter === '__global__') {
        if (summary.key) return false
      } else if (summary.key !== projectFilter) {
        return false
      }
    }
    if (stateFilter && !summary.states.has(stateFilter)) return false
    if (tagFilter && !summary.tagKeys.has(tagFilter.toLowerCase())) return false
    if (appFilter && !summary.appIds.has(Number(appFilter))) return false
    if (onlineOnly && summary.online === 0) return false
    if (!matchesSearch(summary.searchText, search)) return false
    return true
  }).sort((a, b) => {
    if (!a.key) return -1
    if (!b.key) return 1
    return a.key.localeCompare(b.key)
  }), [summaries, projectFilter, stateFilter, tagFilter, appFilter, onlineOnly, search])

  const stats = useMemo(() => filtered.reduce((acc, summary) => ({
    projects: acc.projects + 1,
    apps: acc.apps + summary.apps,
    instances: acc.instances + summary.instances,
    online: acc.online + summary.online,
  }), { projects: 0, apps: 0, instances: 0, online: 0 }), [filtered])

  const filtersActive = !!(search || stateFilter || projectFilter || tagFilter || appFilter || onlineOnly)

  const applyProjectFilter = (value) => setList({ project: value })

  const clearFilters = () => resetList(['q', 'state', 'project', 'tag', 'app', 'online'])

  const changeView = (next) => {
    setList({ view: next })
    localStorage.setItem('helios_projects_view', next)
  }

  const openNew = () => { setEditing(null); setForm(empty); setFormError(''); setShowForm(true) }
  const openEdit = (p) => {
    if (managedByGitOps(p)) {
      toast.info(managedTitle(p) || 'Managed by GitOps; edit in Git')
      return
    }
    setEditing(p); setForm({ name: p.name, repo_patterns: p.repo_patterns || '' }); setFormError(''); setShowForm(true)
  }
  const closeForm = () => { setShowForm(false); setEditing(null); setFormError('') }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true); setFormError('')
    try {
      if (editing) await api.updateProject(editing.id, form)
      else await api.createProject(form)
      toast.success(editing ? 'Project updated' : 'Project created')
      closeForm(); refresh()
    } catch (err) { setFormError(err.message) }
    finally { setSaving(false) }
  }

  const confirmDelete = (p) => {
    if (managedByGitOps(p)) {
      toast.info(managedTitle(p) || 'Managed by GitOps; edit in Git')
      return
    }
    setConfirm({
      title: 'Delete project',
      message: `Delete "${p.name}"? Projects still used by applications or instances cannot be deleted.`,
      onConfirm: () => doDelete(p),
      variant: 'danger'
    })
  }
  const doDelete = async (p) => {
    try { await api.deleteProject(p.id); toast.success('Project deleted'); refresh() }
    catch (e) { toast.error(e.message) }
    finally { setConfirm(null) }
  }

  if (loading) {
    return <section><h1>Projects</h1><div className="skeleton-grid">{[1,2,3,4,5].map(i => <SkeletonCard key={i} />)}</div></section>
  }

  return (
    <section>
      <h1>Projects</h1>
      <p className="muted">Projects scope applications and instances (RBAC object), and can restrict which git repositories their applications may use. The built-in <strong>default</strong> project is global and cannot be deleted.</p>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{stats.projects}</span><span className="stat-label">Projects</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{stats.apps}</span><span className="stat-label">Applications</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{stats.online}</span><span className="stat-label">Online hosts</span></div>
        <div className="stat-card"><span className="stat-dot offline" /><span className="stat-num">{stats.instances - stats.online}</span><span className="stat-label">Offline hosts</span></div>
      </div>

      <div className="filters">
        <SearchBox
          className="filter-search"
          placeholder="Search project, repo pattern, instance, tag or application…"
          value={search}
          onSearch={(value) => setList({ q: value })}
        />
        <select value={stateFilter} onChange={(e) => setList({ state: e.target.value })} aria-label="State">
          <option value="">All states</option>
          <option value="ok">Synced</option>
          <option value="drift">Drift</option>
          <option value="error">Failed</option>
          <option value="unknown">Unknown</option>
        </select>
        <select value={projectFilter} onChange={(e) => applyProjectFilter(e.target.value)} aria-label="Project">
          <option value="">All projects</option>
          <option value="__global__">Global</option>
          {projectOptions.map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <select value={tagFilter} onChange={(e) => setList({ tag: e.target.value })} aria-label="Tag">
          <option value="">All tags</option>
          {tagOptions.map((tag) => <option key={tag} value={tag}>{tag}</option>)}
        </select>
        <select value={appFilter} onChange={(e) => setList({ app: e.target.value })} aria-label="Application">
          <option value="">All applications</option>
          {appOptions.map((app) => <option key={app.id} value={String(app.id)}>{app.name}</option>)}
        </select>
        <label className="row small filter-check">
          <input type="checkbox" checked={onlineOnly} onChange={(e) => setList({ online: e.target.checked })} />
          Online only
        </label>
        {filtersActive && <button type="button" className="ghost small" onClick={clearFilters}>Clear</button>}
        <button type="button" className="filter-add" onClick={openNew} disabled={!can('projects', 'create')}>＋ New project</button>
      </div>

      {(projectFilter || stateFilter || tagFilter || appFilter) && (
        <div className="active-filter">
          <span>Showing</span>
          {projectFilter && <span className="filter-chip">Project: {projectLabel(projectFilter === '__global__' ? '' : projectFilter)}</span>}
          {stateFilter && <span className="filter-chip">State: {stateLabel(stateFilter)}</span>}
          {tagFilter && <span className="filter-chip">Tag: {tagFilter}</span>}
          {appFilter && <span className="filter-chip">Application: {appOptions.find((app) => String(app.id) === appFilter)?.name || `#${appFilter}`}</span>}
          <span className="spacer" />
          <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>
        </div>
      )}

      <div className="row-toolbar">
        <span className="muted">{filtered.length} of {summaries.length} project{summaries.length === 1 ? '' : 's'}</span>
        <div className="toolbar-right">
          <div className="seg" role="group" aria-label="View mode">
            <button type="button" className={'seg-btn' + (view === 'list' ? ' active' : '')} onClick={() => changeView('list')} title="List view">
              <List size={14} /> List
            </button>
            <button type="button" className={'seg-btn' + (view === 'cards' ? ' active' : '')} onClick={() => changeView('cards')} title="Card view">
              <LayoutGrid size={14} /> Cards
            </button>
          </div>
        </div>
      </div>

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}

      {filtered.length === 0 ? (
        <div className="empty"><p className="muted">No project matches these filters.</p></div>
      ) : view === 'list' ? (
        <ProjectTable rows={filtered} onEdit={openEdit} onDelete={confirmDelete} can={can} />
      ) : (
        <div className="cards-grid">
          {filtered.map((summary) => (
            <ProjectCard
              key={summary.key || '__global__'}
              project={summary.project}
              summary={summary}
              onEdit={openEdit}
              onDelete={confirmDelete}
            />
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? `Edit ${editing.name}` : 'New project'} onClose={closeForm}>
          <form className="form-card" onSubmit={submit}>
            <label>Name<input required autoFocus value={form.name} onChange={(e) => setForm((p) => ({ ...p, name: e.target.value }))} placeholder="finance, platform…" /><span className="sub">Used as the RBAC object scope in policy rules.</span></label>
            <label>Allowed repository patterns (one per line)<textarea rows={4} spellCheck={false} value={form.repo_patterns} onChange={(e) => setForm((p) => ({ ...p, repo_patterns: e.target.value }))} placeholder="https://github.com/acme/*&#10;ssh://git@gitlab.example.com/infra/*" /><span className="sub">Empty = any repository. "*" matches any sequence including "/".</span></label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions"><button type="button" className="ghost" onClick={closeForm}>Cancel</button><button type="submit" disabled={saving}>{saving ? 'Saving…' : editing ? 'Save' : 'Create'}</button></div>
          </form>
        </Modal>
      )}
    </section>
  )
}
