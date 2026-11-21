import { useCallback, useEffect, useMemo, useState } from 'react'
import { MapPin } from 'lucide-react'
import { api } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import { SkeletonTable } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const empty = { name: '', description: '' }

const fmtDate = (value) => (value ? new Date(value).toLocaleDateString() : '—')

export default function Regions() {
  const { can } = useAuth()
  const [regions, setRegions] = useState([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setRegions(await api.regions())
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return regions
    return regions.filter((region) =>
      `${region.name} ${region.description || ''}`.toLowerCase().includes(q),
    )
  }, [regions, search])

  const totalInstances = useMemo(
    () => regions.reduce((sum, region) => sum + (region.instances || 0), 0),
    [regions],
  )

  const openNew = () => { setEditing(null); setForm(empty); setFormError(''); setShowForm(true) }
  const openEdit = (region) => {
    setEditing(region)
    setForm({ name: region.name, description: region.description || '' })
    setFormError('')
    setShowForm(true)
  }
  const closeForm = () => { setShowForm(false); setEditing(null); setFormError('') }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true)
    setFormError('')
    try {
      const body = { name: form.name.trim(), description: form.description.trim() }
      if (editing) await api.updateRegion(editing.id, body)
      else await api.createRegion(body)
      toast.success(editing ? 'Region updated' : 'Region added')
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const askDelete = (region) => setConfirm({
    title: 'Delete region',
    message: region.instances
      ? `Delete "${region.name}"? ${region.instances} instance${region.instances === 1 ? '' : 's'} keep this value, but it will no longer be offered in the dropdown.`
      : `Delete "${region.name}"? It will no longer be offered when registering instances.`,
    variant: 'danger',
    onConfirm: () => doDelete(region),
  })

  const doDelete = async (region) => {
    try {
      await api.deleteRegion(region.id)
      toast.success('Region deleted')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  if (loading) {
    return <section><h1>Regions</h1><SkeletonTable rows={4} columns={4} /></section>
  }

  return (
    <section>
      <h1><MapPin size={22} /> Regions</h1>
      <p className="muted page-intro">
        Manage the locations offered when registering or editing instances. A region is a free-form
        label used by the world map and filters; renaming one updates every instance already using it.
      </p>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{regions.length}</span><span className="stat-label">Regions</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{totalInstances}</span><span className="stat-label">Assigned instances</span></div>
      </div>

      <div className="filters">
        <input
          type="search"
          className="filter-search"
          placeholder="Search region or description…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span className="muted">{filtered.length} of {regions.length}</span>
        <button type="button" className="filter-add" onClick={openNew} disabled={!can('regions', 'create')}>＋ New region</button>
      </div>

      {filtered.length === 0 ? (
        <div className="empty">
          <p className="muted">
            {regions.length === 0
              ? 'No regions yet. Add the locations your fleet runs in so they appear in the instance dropdown.'
              : 'No region matches this search.'}
          </p>
          {regions.length === 0 && can('regions', 'create') && (
            <button type="button" onClick={openNew}>Add the first region</button>
          )}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid summary-table">
            <thead>
              <tr>
                <th>Region</th>
                <th>Description</th>
                <th style={{ width: '130px' }}>Instances</th>
                <th style={{ width: '140px' }}>Created</th>
                <th style={{ width: '180px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((region) => (
                <tr key={region.id} className="summary-row">
                  <td>
                    <div className="cell-title">
                      <strong>{region.name}</strong>
                      <span className="cell-sub mono">region:{region.id}</span>
                    </div>
                  </td>
                  <td>{region.description ? region.description : <span className="muted">—</span>}</td>
                  <td>
                    {region.instances ? (
                      <span><strong className="mono">{region.instances}</strong> host{region.instances === 1 ? '' : 's'}</span>
                    ) : (
                      <span className="muted">none</span>
                    )}
                  </td>
                  <td className="muted">{fmtDate(region.created_at)}</td>
                  <td>
                    <div className="row-actions">
                      <button className="btn ghost small" disabled={!can('regions', 'update')} onClick={() => openEdit(region)}>Edit</button>
                      <button className="btn danger small" disabled={!can('regions', 'delete')} onClick={() => askDelete(region)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {confirm && (
        <ConfirmDialog
          open
          onConfirm={confirm.onConfirm}
          onCancel={() => setConfirm(null)}
          title={confirm.title}
          message={confirm.message}
          variant={confirm.variant}
        />
      )}

      {showForm && (
        <Modal title={editing ? `Edit ${editing.name}` : 'New region'} onClose={closeForm}>
          <form className="form-card" onSubmit={submit}>
            <label>
              Region name
              <input
                required
                autoFocus
                value={form.name}
                onChange={(e) => setForm((prev) => ({ ...prev, name: e.target.value }))}
                placeholder="eu-west-1, dc-paris-1, us-east…"
              />
              <span className="sub">Shown in the instance region dropdown, world map and filters.</span>
            </label>
            <label>
              Description <span className="muted">(optional)</span>
              <input
                value={form.description}
                onChange={(e) => setForm((prev) => ({ ...prev, description: e.target.value }))}
                placeholder="Paris primary datacenter"
              />
            </label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions">
              <button type="button" className="ghost" onClick={closeForm}>Cancel</button>
              <button type="submit" disabled={saving || !form.name.trim()}>
                {saving ? 'Saving…' : editing ? 'Save' : 'Add region'}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </section>
  )
}
