import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import { useAuth } from '../App.jsx'

const emptyApp = { name: '', branch: 'main', path: 'site.yml', autoheal: false }
const emptyRepo = { name: '', url: '', username: '', password: '', ssh_key: '', insecure_skip_verify: false }

export default function Applications() {
  const { can } = useAuth()
  const [apps, setApps] = useState([])
  const [repos, setRepos] = useState([])
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(emptyApp)
  const [varsApp, setVarsApp] = useState(null)
  const [repoMode, setRepoMode] = useState('existing')
  const [repoId, setRepoId] = useState('')
  const [repo, setRepo] = useState(emptyRepo)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [formError, setFormError] = useState('')

  const refresh = useCallback(async () => {
    try {
      setError('')
      const [a, r] = await Promise.all([api.applications(), api.repositories()])
      setApps(a)
      setRepos(r)
    } catch (e) {
      setError(e.message)
    }
  }, [])
  useEffect(() => {
    refresh()
  }, [refresh])

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
    setForm({ name: app.name, branch: app.branch, path: app.path, autoheal: app.autoheal })
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
      const app = { ...form, repo_url: repoUrl }
      if (editing) await api.updateApplication({ ...app, id: editing.id })
      else await api.createApplication(app)
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (app) => {
    if (!window.confirm(`Delete application ${app.name}?`)) return
    try {
      setError('')
      await api.deleteApplication(app.id)
      refresh()
    } catch (e) {
      setError(e.message)
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

  return (
    <section>
      <h1>Applications</h1>

      <div className="row-toolbar">
        <span className={`muted ${apps.length ? '' : 'hidden'}`}>
          {apps.length} application{apps.length === 1 ? '' : 's'}
        </span>
        <button onClick={openNew} disabled={!can('applications', 'create')}>
                  ＋ New application
                </button>
      </div>

      {error && <div className="error">{error}</div>}

      {showForm && (
        <Modal
          title={editing ? `Edit ${editing.name}` : 'New application'}
          onClose={closeForm}
        >
          <form className="form-card" onSubmit={submit}>
            <label>
              Name
              <input required autoFocus {...f('name')} />
            </label>

            <div className="sub" style={{ margin: '2px 0 6px' }}>
              Repository
              <div className="two" style={{ marginTop: 4 }}>
                <label className="row small">
                  <input
                    type="radio"
                    checked={repoMode === 'existing'}
                    onChange={() => repoMode !== 'existing' && setRepoMode('existing')}
                  />{' '}
                  Use existing
                </label>
                <label className="row small">
                  <input
                    type="radio"
                    checked={repoMode === 'new'}
                    onChange={() => setRepoMode('new')}
                  />{' '}
                  Create new
                </label>
              </div>
            </div>

            {repoMode === 'existing' ? (
              <label>
                <span className="sub">Repository</span>
                <select value={repoId} onChange={(e) => setRepoId(e.target.value)}>
                  {repos.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name} — {r.url}
                    </option>
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
                  <input
                    type="checkbox"
                    checked={repo.insecure_skip_verify}
                    onChange={(e) => setRepo((p) => ({ ...p, insecure_skip_verify: e.target.checked }))}
                  />{' '}
                  Insecure skip verify
                </label>
              </fieldset>
            )}

            <div className="two">
              <label>
                Revision (branch, tag or semver range, e.g. >=1.2.0 &lt;2.0.0)
                <input placeholder="main, v1.2.0, >=1.0.0" {...f('branch')} />
              </label>
              <label>
                Playbook path (file or dir with site.yml)
                <input {...f('path')} />
              </label>
            </div>
            <label className="row">
              <input
                type="checkbox"
                checked={form.autoheal}
                onChange={(e) => setForm((p) => ({ ...p, autoheal: e.target.checked }))}
              />{' '}
              Auto-heal drift (apply on detection)
            </label>
            {formError && <div className="error">{formError}</div>}
            <div>
              <button type="submit" disabled={saving}>
                {saving ? 'Saving…' : editing ? 'Save' : 'Create'}
              </button>{' '}
              <button type="button" className="ghost" onClick={closeForm}>
                Cancel
              </button>
            </div>
          </form>
        </Modal>
      )}

      <div className="table-wrap">
        <table className="grid">
          <thead>
          <tr>
            <th>Name</th>
            <th>Repo</th>
            <th>Path</th>
            <th>Auto-heal</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {apps.map((a) => {
            const m = matchRepo(a.repo_url)
            return (
              <tr key={a.id}>
                <td>
                  <strong>{a.name}</strong>
                  <div className="sub">id {a.id} · {a.branch}</div>
                </td>
                <td className="mono">
                  {a.repo_url}
                  <div className="sub">{m ? `repo: ${m.name}` : 'no registered repo'}</div>
                </td>
                <td className="mono">{a.path}</td>
                <td>{a.autoheal ? 'yes' : 'no'}</td>
                <td>
                  <button className="small" disabled={!can('applications', 'update')} onClick={() => openEdit(a)}>
                    Edit
                  </button>{' '}
                  <button className="small" disabled={!can('applications', 'update')} onClick={() => setVarsApp(a)}>
                    Variables
                  </button>{' '}
                  <button className="small danger" disabled={!can('applications', 'delete')} onClick={() => remove(a)}>
                    Delete
                  </button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      </div>

      {varsApp && (
        <VariableEditor
          title={`Variables · ${varsApp.name}`}
          subtitle="Application defaults, shared by every assigned host (≈ group_vars). Per-host overrides live on the instance."
          load={() => api.appVariables(varsApp.id)}
          save={(items) => api.saveAppVariables(varsApp.id, items)}
          onClose={() => setVarsApp(null)}
        />
      )}
    </section>
  )
}