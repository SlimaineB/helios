import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const empty = {
  name: '',
  url: '',
  branch: 'main',
  path: '',
  poll_interval: 300,
  enabled: true,
  prune: false,
}

function when(value) {
  if (!value) return 'never'
  const d = new Date(value)
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return 'never'
  return d.toLocaleString()
}

function summaryText(summary) {
  if (!summary) return ''
  const parts = []
  for (const key of ['created', 'updated', 'deleted', 'unchanged', 'errors']) {
    if (summary[key]) parts.push(`${key} ${summary[key]}`)
  }
  return parts.join(' · ')
}

export default function ConfigRepos() {
  const { can } = useAuth()
  const [repos, setRepos] = useState([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [syncing, setSyncing] = useState(null)
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setRepos(await api.configRepos())
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const openNew = () => {
    setEditing(null)
    setForm({ ...empty })
    setFormError('')
    setShowForm(true)
  }
  const openEdit = (repo) => {
    setEditing(repo)
    setForm({
      name: repo.name || '',
      url: repo.url || '',
      branch: repo.branch || 'main',
      path: repo.path || '',
      poll_interval: repo.poll_interval || 300,
      enabled: repo.enabled !== false,
      prune: !!repo.prune,
    })
    setFormError('')
    setShowForm(true)
  }
  const closeForm = () => { setShowForm(false); setEditing(null); setFormError('') }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true)
    setFormError('')
    try {
      const body = { ...form, poll_interval: Number(form.poll_interval) || 300 }
      if (editing) await api.updateConfigRepo(editing.id, body)
      else await api.createConfigRepo(body)
      toast.success(editing ? 'Config repo updated' : 'Config repo created')
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const syncNow = async (repo) => {
    setSyncing(repo.id)
    try {
      await api.syncConfigRepo(repo.id)
      toast.success(`Reconciled ${repo.name}`)
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setSyncing(null)
    }
  }

  const confirmDelete = (repo) => setConfirm({
    title: 'Delete config repo',
    message: `Stop reconciling "${repo.name}"? Resources already created by it are kept and become manually managed; nothing is pruned by this action.`,
    onConfirm: () => doDelete(repo),
    variant: 'danger',
  })
  const doDelete = async (repo) => {
    try {
      await api.deleteConfigRepo(repo.id)
      toast.success('Config repo deleted')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  if (loading) {
    return <section><h1>GitOps</h1><div className="skeleton-grid">{[1, 2, 3].map(i => <SkeletonCard key={i} />)}</div></section>
  }

  return (
    <section>
      <h1>GitOps</h1>
      <p className="muted page-intro">
        Optional Git sources of truth. Enabled repositories are polled and their Helios YAML manifests
        reconcile projects, repositories, applications and assignments. The rest of the UI stays fully manual:
        resources are only pruned when <strong>prune</strong> is explicitly enabled.
      </p>

      <details className="card" style={{ marginBottom: '1rem' }}>
        <summary><strong>Manifest format</strong> (multi-document YAML, .yaml/.yml)</summary>
        <pre className="mono" style={{ overflowX: 'auto', margin: '.75rem 0 0', fontSize: 11.5 }}>{`apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: team
spec:
  repoPatterns: "https://git.example.com/team/*"
---
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: team-infra
spec:
  url: https://git.example.com/team/infra.git
---
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: web
  project: team
spec:
  repoRef: team-infra
  branch: main
  path: site.yml
  autoHeal: true
---
apiVersion: helios.dev/v1alpha1
kind: Assignment
metadata:
  name: web-on-prod-1
spec:
  application: web
  instance: ins-...
  autoHeal: true`}</pre>
      </details>

      <div className="row-toolbar">
        <span className="muted">{repos.length} config repo{repos.length === 1 ? '' : 's'}</span>
        <button onClick={openNew} disabled={!can('configrepos', 'create')}>＋ New config repo</button>
      </div>

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}

      {repos.length === 0 ? (
        <div className="empty">
          <p className="muted">No GitOps config repo. Add one to declare projects, applications and assignments in Git.</p>
        </div>
      ) : (
        <div className="cards-grid">
          {repos.map((repo) => (
            <article key={repo.id} className="card">
              <div className="card-head">
                <div className="repo-main">
                  <div className="app-name">{repo.name}</div>
                  <div className="instance-meta">
                    <span className="mono">{repo.url}</span>
                    <span>{repo.branch || 'main'}{repo.path ? ` / ${repo.path}` : ''}</span>
                  </div>
                </div>
                <div className="card-actions">
                  <span className={`state-badge ${repo.enabled ? 'enabled' : 'disabled'}`}>
                    {repo.enabled ? 'enabled' : 'disabled'}
                  </span>
                  <button className="ghost small" onClick={() => syncNow(repo)} disabled={syncing === repo.id}>
                    {syncing === repo.id ? 'Syncing…' : 'Sync now'}
                  </button>
                  <button className="ghost small" onClick={() => openEdit(repo)} disabled={!can('configrepos', 'update')}>Edit</button>
                  <button className="ghost small danger" onClick={() => confirmDelete(repo)} disabled={!can('configrepos', 'delete')}>Delete</button>
                </div>
              </div>
              <div className="card-body">
                <div className="instance-meta">
                  <span>Every {repo.poll_interval || 300}s</span>
                  <span>Prune: {repo.prune ? 'on' : 'off'}</span>
                  <span>Last sync: {when(repo.last_synced)}</span>
                  {repo.last_commit && <span className="mono">{repo.last_commit.slice(0, 12)}</span>}
                </div>
                {summaryText(repo.last_summary) && (
                  <div className="muted"><span className="sub">{summaryText(repo.last_summary)}</span></div>
                )}
                {repo.last_error && <div className="error">{repo.last_error}</div>}
              </div>
            </article>
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? `Edit ${editing.name}` : 'New config repo'} onClose={closeForm}>
          <form className="form-card" onSubmit={submit}>
            <label>Name<input required autoFocus value={form.name} onChange={(e) => setForm((p) => ({ ...p, name: e.target.value }))} placeholder="platform-config" /></label>
            <label>Git URL<input required value={form.url} onChange={(e) => setForm((p) => ({ ...p, url: e.target.value }))} placeholder="https://github.com/acme/helios-config.git" /><span className="sub">Credentials are read from the matching Repositories entry when present.</span></label>
            <div className="two">
              <label>Branch<input value={form.branch} onChange={(e) => setForm((p) => ({ ...p, branch: e.target.value }))} placeholder="main" /></label>
              <label>Path<input value={form.path} onChange={(e) => setForm((p) => ({ ...p, path: e.target.value }))} placeholder="clusters/prod (empty = root)" /></label>
            </div>
            <label>Poll interval (seconds)<input type="number" min="10" value={form.poll_interval} onChange={(e) => setForm((p) => ({ ...p, poll_interval: e.target.value }))} /><span className="sub">Minimum 10 seconds. The server also checks quickly after create/update.</span></label>
            <label className="row"><input type="checkbox" checked={form.enabled} onChange={(e) => setForm((p) => ({ ...p, enabled: e.target.checked }))} />{' '}Enabled (poll this repo automatically)</label>
            <label className="row"><input type="checkbox" checked={form.prune} onChange={(e) => setForm((p) => ({ ...p, prune: e.target.checked }))} />{' '}Prune resources removed from Git<span className="sub">Off by default. Manual assignments still block deletion and are reported as sync errors.</span></label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions">
              <button type="button" className="ghost" onClick={closeForm}>Cancel</button>
              <button type="submit" disabled={saving}>{saving ? 'Saving…' : editing ? 'Save' : 'Create'}</button>
            </div>
          </form>
        </Modal>
      )}
    </section>
  )
}
