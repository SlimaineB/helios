import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client.js'
import Modal from '../components/Modal.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import SyncWindowCard, { formatDays } from '../components/SyncWindowCard.jsx'

const DAYS = [
  ['mon', 'Mon'], ['tue', 'Tue'], ['wed', 'Wed'], ['thu', 'Thu'],
  ['fri', 'Fri'], ['sat', 'Sat'], ['sun', 'Sun'],
]

const PRESETS = [
  { key: 'weekdays', label: 'Weekdays 09–17', days: 'mon,tue,wed,thu,fri', start_min: 9 * 60, end_min: 17 * 60 },
  { key: 'weekends', label: 'Weekends', days: 'sat,sun', start_min: 0, end_min: 23 * 60 + 59 },
  { key: 'nights', label: 'Nights 22–06', days: '', start_min: 22 * 60, end_min: 6 * 60 },
  { key: 'always', label: '24/7', days: '', start_min: 0, end_min: 23 * 60 + 59 },
]

const toHM = (m) => {
  const n = Number.isFinite(m) ? m : 0
  return `${String(Math.floor(n / 60)).padStart(2, '0')}:${String(n % 60).padStart(2, '0')}`
}
const fromHM = (v) => {
  const [h, m] = String(v || '0:0').split(':').map(Number)
  return (h || 0) * 60 + (m || 0)
}

const empty = {
  name: '', kind: 'deny', app_id: 0, timezone: 'UTC',
  days: '', start_min: 9 * 60, end_min: 17 * 60, enabled: true,
}

export default function SyncWindows() {
  const { can } = useAuth()
  const [windows, setWindows] = useState([])
  const [apps, setApps] = useState([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(empty)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    try { setLoading(true); const [w, a] = await Promise.all([api.syncWindows(), api.applications()]); setWindows(w); setApps(a) }
    catch (e) { toast.error(e.message) }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { refresh() }, [refresh])

  const openNew = () => { setEditing(null); setForm({ ...empty }); setFormError(''); setShowForm(true) }
  const openEdit = (w) => {
    setEditing(w)
    setForm({ ...empty, ...w, timezone: w.timezone || 'UTC', days: w.days || '' })
    setFormError(''); setShowForm(true)
  }
  const closeForm = () => { setShowForm(false); setEditing(null); setFormError('') }

  const toggleDay = (d) => {
    const set = new Set(form.days.split(',').filter(Boolean))
    if (set.has(d)) set.delete(d); else set.add(d)
    setForm((p) => ({ ...p, days: [...set].join(',') }))
  }
  const applyPreset = (p) => {
    setForm((prev) => ({ ...prev, days: p.days, start_min: p.start_min, end_min: p.end_min }))
  }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true); setFormError('')
    try {
      const body = { ...form, app_id: Number(form.app_id) || 0, start_min: fromHM(toHM(form.start_min)), end_min: fromHM(toHM(form.end_min)) }
      if (editing) await api.updateSyncWindow(editing.id, body)
      else await api.createSyncWindow(body)
      toast.success(editing ? 'Sync window updated' : 'Sync window created')
      closeForm(); refresh()
    } catch (err) { setFormError(err.message) }
    finally { setSaving(false) }
  }

  const confirmDelete = (w) => setConfirm({ title: 'Delete sync window', message: `Delete "${w.name}"?`, onConfirm: () => doDelete(w), variant: 'danger' })
  const doDelete = async (w) => { try { await api.deleteSyncWindow(w.id); toast.success('Sync window deleted'); refresh() } catch (e) { toast.error(e.message) } finally { setConfirm(null) } }

  const appName = (id) => (id ? apps.find((a) => a.id === id)?.name || `app ${id}` : 'All applications')

  if (loading) return <section><h1>Sync windows</h1><div className="skeleton-grid">{[1, 2, 3, 4].map(i => <SkeletonCard key={i} />)}</div></section>

  const dayLabel = formatDays(form.days)
  const preview = form.kind === 'deny'
    ? `Syncs are blocked ${dayLabel === 'Every day' ? 'every day' : dayLabel} between ${toHM(form.start_min)} and ${toHM(form.end_min)} (${form.timezone || 'UTC'}).`
    : `Syncs are allowed only ${dayLabel === 'Every day' ? 'every day' : dayLabel} between ${toHM(form.start_min)} and ${toHM(form.end_min)} (${form.timezone || 'UTC'}); blocked outside.`

  return (
    <section>
      <h1>Sync windows</h1>
      <p className="muted page-intro">
        Control when deployments can run. A <strong>block</strong> window freezes syncs while it is active;
        an <strong>allow</strong> window only permits syncs during it. Checks and drift detection always run.
      </p>

      <div className="row-toolbar">
        <span className="muted">{windows.length} window{windows.length === 1 ? '' : 's'}</span>
        <button onClick={openNew} disabled={!can('windows', 'create')}>＋ New window</button>
      </div>

      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}

      {windows.length === 0 ? (
        <div className="empty"><p className="muted">No sync windows. Deployments are allowed at any time.</p></div>
      ) : (
        <div className="cards-grid">
          {windows.map((w) => (
            <SyncWindowCard
              key={w.id}
              window={w}
              appName={appName(w.app_id)}
              onEdit={openEdit}
              onDelete={confirmDelete}
            />
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? `Edit ${editing.name}` : 'New sync window'} onClose={closeForm}>
          <form className="form-card" onSubmit={submit}>
            <label>Name<input required autoFocus value={form.name} onChange={(e) => setForm((p) => ({ ...p, name: e.target.value }))} placeholder="maintenance, business hours…" /></label>

            <div className="field-label">What should it do?</div>
            <div className="choice-grid">
              <label className={`choice ${form.kind === 'deny' ? 'selected' : ''}`}>
                <input type="radio" name="kind" checked={form.kind === 'deny'} onChange={() => setForm((p) => ({ ...p, kind: 'deny' }))} />
                <span>
                  <strong>Block syncs during the window</strong>
                  <span className="sub">Freezes deploys inside the time range (e.g. maintenance).</span>
                </span>
              </label>
              <label className={`choice ${form.kind === 'allow' ? 'selected' : ''}`}>
                <input type="radio" name="kind" checked={form.kind === 'allow'} onChange={() => setForm((p) => ({ ...p, kind: 'allow' }))} />
                <span>
                  <strong>Allow syncs only during the window</strong>
                  <span className="sub">Freezes deploys outside the time range (e.g. business hours).</span>
                </span>
              </label>
            </div>

            <div className="field-label">When?</div>
            <div className="preset-row">
              {PRESETS.map((p) => (
                <button key={p.key} type="button" className="ghost small" onClick={() => applyPreset(p)}>{p.label}</button>
              ))}
            </div>

            <div className="day-picker" role="group" aria-label="Days">
              {DAYS.map(([v, label]) => {
                const on = form.days.split(',').includes(v)
                return (
                  <button
                    key={v}
                    type="button"
                    className={`day${on ? ' on' : ''}`}
                    aria-pressed={on}
                    onClick={() => toggleDay(v)}
                  >
                    {label}
                  </button>
                )
              })}
            </div>
            <span className="sub day-hint">{form.days ? `Selected: ${formatDays(form.days)}` : 'No day selected = every day'}</span>

            <div className="two">
              <label>Start<input type="time" required value={toHM(form.start_min)} onChange={(e) => setForm((p) => ({ ...p, start_min: fromHM(e.target.value) }))} /></label>
              <label>End<input type="time" required value={toHM(form.end_min)} onChange={(e) => setForm((p) => ({ ...p, end_min: fromHM(e.target.value) }))} /></label>
            </div>
            <label>
              Timezone
              <input list="tz-list" value={form.timezone} onChange={(e) => setForm((p) => ({ ...p, timezone: e.target.value }))} placeholder="UTC, Europe/Paris…" />
              <datalist id="tz-list">
                <option value="UTC" />
                <option value="Europe/Paris" />
                <option value="Europe/London" />
                <option value="Europe/Berlin" />
                <option value="America/New_York" />
                <option value="America/Los_Angeles" />
                <option value="Asia/Singapore" />
                <option value="Asia/Tokyo" />
              </datalist>
              <span className="sub">Overnight ranges are supported (start after end).</span>
            </label>

            <label>
              Scope
              <select value={form.app_id} onChange={(e) => setForm((p) => ({ ...p, app_id: Number(e.target.value) }))}>
                <option value={0}>All applications</option>
                {apps.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </label>

            <label className="row">
              <input type="checkbox" checked={form.enabled} onChange={(e) => setForm((p) => ({ ...p, enabled: e.target.checked }))} />{' '}Enabled
            </label>

            <div className={`window-preview ${form.kind}`}>
              <strong>Summary</strong>
              <span>{preview}</span>
            </div>

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
