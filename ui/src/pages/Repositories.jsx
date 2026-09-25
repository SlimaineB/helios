import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import { useAuth } from '../App.jsx'
import Modal from '../components/Modal.jsx'

const empty = { name: '', url: '', username: '', password: '', ssh_key: '', insecure_skip_verify: false }

export default function Repositories() {
  const { can } = useAuth()
  const [repos, setRepos] = useState([])
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [error, setError] = useState('')
  const [formError, setFormError] = useState('')

  const refresh = useCallback(async () => {
    try {
      setError('')
      setRepos(await api.repositories())
    } catch (e) {
      setError(e.message)
    }
  }, [])
  useEffect(() => {
    refresh()
  }, [refresh])

  const openNew = () => {
    setEditing(null)
    setForm(empty)
    setFormError('')
    setShowForm(true)
  }
  const openEdit = (repo) => {
    setEditing(repo)
    setForm({ ...empty, name: repo.name, url: repo.url, username: repo.username, insecure_skip_verify: repo.insecure_skip_verify })
    setFormError('')
    setShowForm(true)
  }
  const closeForm = () => {
    setShowForm(false)
    setEditing(null)
    setForm(empty)
    setFormError('')
  }

  const submit = async (e) => {
    e.preventDefault()
    setFormError('')
    const body = {
      name: form.name,
      url: form.url,
      username: form.username,
      insecure_skip_verify: form.insecure_skip_verify,
    }
    if (form.password) body.password = form.password
    if (form.ssh_key) body.ssh_key = form.ssh_key
    try {
      if (editing) await api.updateRepository({ ...body, id: editing.id })
      else await api.createRepository(body)
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    }
  }

  const remove = async (repo) => {
    if (!window.confirm(`Delete repository ${repo.url}?`)) return
    try {
      setError('')
      await api.deleteRepository(repo.id)
      refresh()
    } catch (e) {
      setError(e.message)
    }
  }

  const f = (k) => ({
    value: form[k],
    onChange: (e) => setForm((p) => ({ ...p, [k]: e.target.value })),
  })

  const authLabel = (repo) => {
    if (repo.password_set || repo.ssh_key_set) {
      return [repo.username || (repo.password_set ? 'token' : 'ssh'), repo.ssh_key_set ? 'ssh key' : null]
        .filter(Boolean)
        .join(' + ')
    }
    return repo.username || 'none'
  }

  return (
    <section>
      <h1>Repositories</h1>

      <p className="muted">
        Git connections, reusable by any application via <code>repo_url</code> — like ArgoCD's
        repositories. The agent authenticates against private repos using the matching entry.
      </p>

      <div className="row-toolbar">
        <span className={`muted ${repos.length ? '' : 'hidden'}`}>
          {repos.length} repo{repos.length === 1 ? '' : 's'}
        </span>
        <button onClick={openNew} disabled={!can('repositories', 'create')}>
                  ＋ New repository
                </button>
      </div>

      {error && <div className="error">{error}</div>}

      {showForm && (
        <Modal title={editing ? `Edit repository` : 'New repository'} onClose={closeForm} wide>
          <form className="form-card" onSubmit={submit}>
            <div className="two">
              <label>
                Name
                <input required autoFocus {...f('name')} />
              </label>
              <label>
                Git URL
                <input
                  required
                  placeholder="https://github.com/you/infra.git"
                  {...f('url')}
                />
              </label>
            </div>
            <label>
              Username (leave empty to use the token as the user, e.g. GitHub fine-grained PAT)
              <input {...f('username')} />
            </label>
            <div className="two">
              <label>
                Password / token
                <input
                  type="password"
                  placeholder={
                    editing && editing.password_set ? '•••••• (unchanged)' : 'e.g. github PAT'
                  }
                  {...f('password')}
                />
              </label>
              <label className="row">
                <input
                  type="checkbox"
                  checked={form.insecure_skip_verify}
                  onChange={(e) =>
                    setForm((p) => ({ ...p, insecure_skip_verify: e.target.checked }))
                  }
                />{' '}
                Insecure skip verify (self-signed TLS)
              </label>
            </div>
            <label>
              SSH private key (deploy key, optional)
              <textarea
                rows={4}
                spellCheck={false}
                placeholder={editing && editing.ssh_key_set ? '•••••• (unchanged)' : '-----BEGIN OPENSSH PRIVATE KEY-----'}
                {...f('ssh_key')}
              />
            </label>
            {formError && <div className="error">{formError}</div>}
            <div>
              <button type="submit">{editing ? 'Save' : 'Create'}</button>{' '}
              <button type="button" className="ghost" onClick={closeForm}>
                Cancel
              </button>
            </div>
          </form>
        </Modal>
      )}

      {repos.length === 0 ? (
        <div className="empty muted">No repositories yet.</div>
      ) : (
        <div className="table-wrap">
        <table className="grid">
          <thead>
            <tr>
              <th>Name</th>
              <th>URL</th>
              <th>Auth</th>
              <th>Insecure</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {repos.map((repo) => (
              <tr key={repo.id}>
                <td>
                  <strong>{repo.name}</strong>
                  <div className="sub">id {repo.id}</div>
                </td>
                <td className="mono">{repo.url}</td>
                <td className="mono">{authLabel(repo)}</td>
                <td>{repo.insecure_skip_verify ? 'yes' : 'no'}</td>
                <td>
                  <button className="small" disabled={!can('repositories', 'update')} onClick={() => openEdit(repo)}>
                    Edit
                  </button>{' '}
                  <button className="small danger" disabled={!can('repositories', 'delete')} onClick={() => remove(repo)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      )}
    </section>
  )
}