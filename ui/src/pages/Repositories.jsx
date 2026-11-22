import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { LayoutGrid, List, Pencil, PlugZap, Trash2 } from 'lucide-react'
import { api, DEFAULT_PROJECT, projectForUi, projectForApi, projectLabel } from '../api/client.js'
import { useAuth } from '../App.jsx'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
import { SkeletonEmpty, SkeletonCard } from '../components/Skeleton.jsx'
import { toast } from '../components/Toast.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from '../components/GitOpsBadge.jsx'
import RepositoryCard from '../components/RepositoryCard.jsx'

const empty = { name: '', url: '', project: DEFAULT_PROJECT, username: '', password: '', ssh_key: '', insecure_skip_verify: false }

const authKind = (repo) => {
  if (repo.ssh_key_set) return 'ssh'
  if (repo.password_set) return 'token'
  if (repo.username) return 'user'
  return 'none'
}

const authLabel = (repo) => {
  if (repo.ssh_key_set) return 'SSH key'
  if (repo.password_set) return `${repo.username || 'token'} / token`
  return repo.username || 'none'
}

export default function Repositories() {
  const { can } = useAuth()
  const [repos, setRepos] = useState([])
  const [projects, setProjects] = useState([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [confirm, setConfirm] = useState(null)
  const [checking, setChecking] = useState(null)
  const [search, setSearch] = useState('')
  const [authFilter, setAuthFilter] = useState('')
  const [view, setView] = useState('cards')

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      const [r, p] = await Promise.all([api.repositories(), api.projects().catch(() => [])])
      setRepos(r)
      setProjects(p)
    } catch (e) { toast.error(e.message) }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const filteredRepos = useMemo(() => {
    const q = search.trim().toLowerCase()
    return repos.filter((repo) => {
      if (authFilter && authKind(repo) !== authFilter) return false
      if (!q) return true
      return [repo.name, repo.url, repo.username]
        .some((value) => (value || '').toLowerCase().includes(q))
    })
  }, [repos, search, authFilter])

  const openNew = () => { setEditing(null); setForm({ ...empty, project: projects[0]?.name || DEFAULT_PROJECT }); setFormError(''); setShowForm(true) }
  const openEdit = (repo) => {
    if (managedByGitOps(repo)) {
      toast.info(managedTitle(repo) || 'Managed by GitOps; edit in Git')
      return
    }
    setEditing(repo); setForm({ ...empty, name: repo.name, url: repo.url, project: projectForUi(repo.project), username: repo.username, insecure_skip_verify: repo.insecure_skip_verify }); setFormError(''); setShowForm(true)
  }
  const closeForm = () => { setShowForm(false); setEditing(null); setForm(empty); setFormError('') }

  const submit = async (e) => {
    e.preventDefault(); setFormError('')
    const body = { name: form.name, url: form.url, project: projectForApi(form.project), username: form.username, insecure_skip_verify: form.insecure_skip_verify }
    if (form.password) body.password = form.password
    if (form.ssh_key) body.ssh_key = form.ssh_key
    try {
      if (editing) await api.updateRepository({ ...body, id: editing.id })
      else await api.createRepository(body)
      toast.success(editing ? 'Repository updated' : 'Repository created')
      closeForm(); refresh()
    } catch (err) { setFormError(err.message) }
  }

  const checkRepo = async (repo) => {
    setChecking(repo.id)
    try {
      await api.checkRepository(repo.id)
      toast.success(`Connection OK: ${repo.url}`)
    } catch (e) {
      toast.error(e.message)
    } finally {
      setChecking(null)
    }
  }

  const confirmDelete = (repo) => {
    if (managedByGitOps(repo)) {
      toast.info(managedTitle(repo) || 'Managed by GitOps; edit in Git')
      return
    }
    setConfirm({ title: 'Delete repository', message: `Delete "${repo.url}"? Repositories still used by applications cannot be deleted.`, onConfirm: () => doDelete(repo), variant: 'danger' })
  }
  const doDelete = async (repo) => { try { await api.deleteRepository(repo.id); toast.success('Repository deleted'); refresh() } catch (e) { toast.error(e.message) } finally { setConfirm(null) } }

  const f = (k) => ({ value: form[k], onChange: (e) => setForm((p) => ({ ...p, [k]: e.target.value })) })
  const clearFilters = () => { setSearch(''); setAuthFilter('') }
  const hasFilters = !!search || !!authFilter

  if (loading) return <section><h1>Repositories</h1><div className="skeleton-grid">{[1,2,3,4,5].map(i => <SkeletonCard key={i} />)}</div></section>

  return (
    <section>
      <h1>Repositories</h1>
      <p className="muted">Git connections scoped to a project, reusable by its applications via <code>repo_url</code>. The agent authenticates against private repos using the matching tenant entry.</p>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search name, URL or username…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select value={authFilter} onChange={(e) => setAuthFilter(e.target.value)} aria-label="Filter by authentication">
          <option value="">All authentication</option>
          <option value="ssh">SSH key</option>
          <option value="token">Token / password</option>
          <option value="user">Username only</option>
          <option value="none">No authentication</option>
        </select>
        {hasFilters && <button type="button" className="ghost small" onClick={clearFilters}>Clear filters</button>}
        <button type="button" className="filter-add" onClick={openNew} disabled={!can('repositories', 'create')}>＋ New repository</button>
      </div>

      <div className="row-toolbar">
        <span className="muted">{filteredRepos.length} of {repos.length} repo{repos.length === 1 ? '' : 's'}</span>
        <div className="toolbar-right">
          <div className="seg" role="group" aria-label="View mode">
            <button
              type="button"
              className={'seg-btn' + (view === 'cards' ? ' active' : '')}
              onClick={() => setView('cards')}
              title="Card view"
            >
              <LayoutGrid size={14} /> Cards
            </button>
            <button
              type="button"
              className={'seg-btn' + (view === 'table' ? ' active' : '')}
              onClick={() => setView('table')}
              title="List view"
            >
              <List size={14} /> List
            </button>
          </div>
        </div>
      </div>

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}

      {view === 'cards' ? (
        <div className="cards-grid">
          {filteredRepos.length === 0 ? <SkeletonEmpty lines={2} /> : (
            filteredRepos.map((repo) => (
              <RepositoryCard
                key={repo.id}
                repo={repo}
                onEdit={openEdit}
                onDelete={confirmDelete}
                onCheck={checkRepo}
                checking={checking === repo.id}
              />
            ))
          )}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid summary-table">
            <thead>
              <tr>
                <th>Repository</th>
                <th style={{ width: '150px' }}>Project</th>
                <th style={{ width: '190px' }}>Authentication</th>
                <th style={{ width: '150px' }}>TLS</th>
                <th style={{ width: '260px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filteredRepos.map((repo) => {
                const readOnly = managedByGitOps(repo)
                return (
                <tr key={repo.id}>
                  <td>
                    <div className="cell-title">
                      <strong>{repo.name || repo.url}</strong>
                      <span className="cell-sub">
                        <CopyButton value={repo.url} size="small" title="Copy repository URL" />
                        <span className="mono">{repo.url}</span>
                        <GitOpsBadge obj={repo} />
                      </span>
                    </div>
                  </td>
                  <td><span className="project-badge">{projectLabel(repo.project)}</span></td>
                  <td>
                    <div className="cell-title">
                      <strong>{authLabel(repo)}</strong>
                      {repo.username && repo.password_set && <span className="cell-sub">{repo.username}</span>}
                    </div>
                  </td>
                  <td>{repo.insecure_skip_verify ? 'Skip verify' : 'Verified'}</td>
                  <td>
                    <div className="actions">
                      <Link className="btn ghost small" to={`/applications?repo=${encodeURIComponent(repo.url)}`}>Applications</Link>
                      <button className="btn ghost small" disabled={checking === repo.id} onClick={() => checkRepo(repo)}>
                        <PlugZap size={13} /> {checking === repo.id ? 'Testing…' : 'Test'}
                      </button>
                      <button className="btn ghost small" disabled={readOnly || !can('repositories', 'update')} title={managedTitle(repo)} onClick={() => openEdit(repo)}>
                        <Pencil size={13} /> Edit
                      </button>
                      <button className="btn danger small" disabled={readOnly || !can('repositories', 'delete')} title={managedTitle(repo)} onClick={() => confirmDelete(repo)}>
                        <Trash2 size={13} /> Delete
                      </button>
                    </div>
                  </td>
                </tr>
                )
              })}
              {filteredRepos.length === 0 && (
                <tr><td colSpan={5} className="muted">{hasFilters ? 'No repository matches these filters.' : 'No repositories yet.'}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {showForm && (
        <Modal title={editing ? 'Edit repository' : 'New repository'} onClose={closeForm} wide>
          <form className="form-card" onSubmit={submit}>
            <div className="two"><label>Name<input required autoFocus {...f('name')} /></label><label>Project<select required value={form.project} onChange={(e) => setForm((p) => ({ ...p, project: e.target.value }))}><option value={DEFAULT_PROJECT}>default (global)</option>{form.project !== DEFAULT_PROJECT && !projects.some((p) => p.name === form.project) && <option value={form.project}>{form.project}</option>}{projects.map((p) => <option key={p.id} value={p.name}>{p.name}</option>)}</select><span className="sub">Credentials are scoped to this tenant.</span></label></div>
            <label>Git URL<input required placeholder="https://github.com/you/infra.git" {...f('url')} /></label>
            <label>Username (leave empty to use the token as the user, e.g. GitHub fine-grained PAT)<input {...f('username')} /></label>
            <div className="two"><label>Password / token<input type="password" placeholder={editing && editing.password_set ? '•••••• (unchanged)' : 'e.g. github PAT'} {...f('password')} /></label><label className="row"><input type="checkbox" checked={form.insecure_skip_verify} onChange={(e) => setForm((p) => ({ ...p, insecure_skip_verify: e.target.checked }))} />{' '}Insecure skip verify (self-signed TLS)</label></div>
            <label>SSH private key (deploy key, optional)<textarea rows={4} spellCheck={false} placeholder={editing && editing.ssh_key_set ? '•••••• (unchanged)' : '-----BEGIN OPENSSH PRIVATE KEY-----'} {...f('ssh_key')} /></label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions"><button type="button" className="ghost" onClick={closeForm}>Cancel</button><button type="submit">{editing ? 'Save' : 'Create'}</button></div>
          </form>
        </Modal>
      )}
    </section>
  )
}
