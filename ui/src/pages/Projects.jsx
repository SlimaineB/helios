import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import ProjectCard from '../components/ProjectCard.jsx'

const empty = { name: '', repo_patterns: '' }

const DEFAULT_PROJECT = {
  id: '__default__',
  name: 'default (global)',
  repo_patterns: '',
  system: true,
}

export default function Projects() {
  const { can } = useAuth()
  const [projects, setProjects] = useState([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setProjects(await api.projects())
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const openNew = () => { setEditing(null); setForm(empty); setFormError(''); setShowForm(true) }
  const openEdit = (p) => { setEditing(p); setForm({ name: p.name, repo_patterns: p.repo_patterns || '' }); setFormError(''); setShowForm(true) }
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

  const confirmDelete = (p) => setConfirm({
    title: 'Delete project',
    message: `Delete "${p.name}"? Projects still used by applications or instances cannot be deleted.`,
    onConfirm: () => doDelete(p),
    variant: 'danger'
  })
  const doDelete = async (p) => {
    try { await api.deleteProject(p.id); toast.success('Project deleted'); refresh() }
    catch (e) { toast.error(e.message) }
    finally { setConfirm(null) }
  }

  if (loading) {
    return <section><h1>Projects</h1><div className="skeleton-grid">{[1,2,3,4,5].map(i => <SkeletonCard key={i} />)}</div></section>
  }

  const all = [DEFAULT_PROJECT, ...projects]

  return (
    <section>
      <h1>Projects</h1>
      <p className="muted">Projects scope applications and instances (RBAC object), and can restrict which git repositories their applications may use. The built-in <strong>default</strong> project is global and cannot be deleted.</p>

      <div className="row-toolbar">
        <span className="muted">{all.length} project{all.length === 1 ? '' : 's'}</span>
        <button onClick={openNew} disabled={!can('projects', 'create')}>＋ New project</button>
      </div>

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}

      <div className="cards-grid">
        {all.map((p) => (
          <ProjectCard
            key={p.id}
            project={p}
            onEdit={openEdit}
            onDelete={confirmDelete}
          />
        ))}
      </div>

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