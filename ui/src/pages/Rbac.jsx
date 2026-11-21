import { useCallback, useEffect, useMemo, useState } from 'react'
import { KeyRound, RotateCcw, ShieldCheck, Trash2, UserPlus } from 'lucide-react'
import { api } from '../api/client.js'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import { SkeletonTable } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const emptyPolicy = { role: '', resource: 'applications', action: 'get', object: '*' }
const emptyBinding = { group: '', role: '' }

const fmt = (value) => {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? '—' : d.toLocaleString()
}

export default function Rbac() {
  const { can } = useAuth()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [policyForm, setPolicyForm] = useState(emptyPolicy)
  const [bindingForm, setBindingForm] = useState(emptyBinding)
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)

  const canUpdate = can('rbac', 'update')

  const refresh = useCallback(async () => {
    try {
      setData(await api.rbac())
      setError('')
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 30000 })

  const policies = data?.policies || []
  const bindings = data?.bindings || []
  const resources = data?.resources || []
  const actions = data?.actions || []

  const submitPolicy = async (e) => {
    e.preventDefault()
    if (!canUpdate) return
    setSaving(true)
    try {
      await api.createRBACPolicy(policyForm)
      toast.success('Policy added')
      setPolicyForm({ ...emptyPolicy, resource: policyForm.resource, action: policyForm.action })
      await refresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setSaving(false)
    }
  }

  const submitBinding = async (e) => {
    e.preventDefault()
    if (!canUpdate) return
    setSaving(true)
    try {
      await api.createRBACBinding(bindingForm)
      toast.success('Binding added')
      setBindingForm(emptyBinding)
      await refresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setSaving(false)
    }
  }

  const askDeletePolicy = (p) => setConfirm({
    title: 'Delete policy',
    message: `Delete ${p.role} → ${p.resource}.${p.action} on ${p.object}?`,
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.deleteRBACPolicy(p.id)
        toast.success('Policy deleted')
        await refresh()
      } catch (e) {
        toast.error(e.message)
      }
    },
  })

  const askDeleteBinding = (b) => setConfirm({
    title: 'Delete binding',
    message: `Unbind ${b.group} from ${b.role}?`,
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.deleteRBACBinding(b.id)
        toast.success('Binding deleted')
        await refresh()
      } catch (e) {
        toast.error(e.message)
      }
    },
  })

  const askReset = () => setConfirm({
    title: 'Reset RBAC to bootstrap file',
    message: 'Delete all database-managed policies and bindings, then reload the YAML file (or built-in roles only).',
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.resetRBAC()
        toast.success('RBAC reset to bootstrap')
        await refresh()
      } catch (e) {
        toast.error(e.message)
      }
    },
  })

  const sourceLabel = useMemo(() => ({
    db: 'database',
    file: 'bootstrap file',
    builtin: 'built-in only',
  }[data?.source] || data?.source || '—'), [data?.source])

  if (loading && !data) {
    return (
      <section>
        <h1><KeyRound size={22} /> RBAC</h1>
        <SkeletonTable rows={5} columns={5} />
      </section>
    )
  }

  return (
    <section>
      <h1><KeyRound size={22} /> RBAC</h1>
      <p className="page-intro">
        Policies and bindings are stored in the hub database and reloaded live. The YAML file is only a
        first-run bootstrap; edit rules here and they apply immediately to new requests.
      </p>

      {error && <div className="settings-note warn"><span>{error}</span></div>}
      {!canUpdate && (
        <div className="settings-note">
          <ShieldCheck size={15} />
          <span>Read-only. Ask an admin for <code>rbac.update</code> to edit policies.</span>
        </div>
      )}

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{policies.length}</span><span className="stat-label">Policies</span></div>
        <div className="stat-card"><span className="stat-dot apps" /><span className="stat-num">{bindings.length}</span><span className="stat-label">Bindings</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{sourceLabel}</span><span className="stat-label">Source</span></div>
        {canUpdate && data?.source === 'db' && (
          <button type="button" className="btn ghost" onClick={askReset}>
            <RotateCcw size={15} /> Reset to file
          </button>
        )}
      </div>

      <div className="rbac-grid">
        <article className="card">
          <div className="card-head">
            <div>
              <h3>Policies</h3>
              <span className="sub">Who can do what, on which resource and scope</span>
            </div>
          </div>
          <div className="card-body">
            {canUpdate && (
              <form className="rbac-form" onSubmit={submitPolicy}>
                <label>
                  Role
                  <input value={policyForm.role} onChange={(e) => setPolicyForm((p) => ({ ...p, role: e.target.value }))} placeholder="role:deployer" required />
                </label>
                <label>
                  Resource
                  <select value={policyForm.resource} onChange={(e) => setPolicyForm((p) => ({ ...p, resource: e.target.value }))}>
                    {resources.map((r) => <option key={r} value={r}>{r}</option>)}
                    <option value="*">*</option>
                  </select>
                </label>
                <label>
                  Action
                  <select value={policyForm.action} onChange={(e) => setPolicyForm((p) => ({ ...p, action: e.target.value }))}>
                    {actions.map((a) => <option key={a} value={a}>{a}</option>)}
                    <option value="*">*</option>
                  </select>
                </label>
                <label>
                  Scope
                  <input value={policyForm.object} onChange={(e) => setPolicyForm((p) => ({ ...p, object: e.target.value }))} placeholder="* or project" required />
                </label>
                <button type="submit" className="btn" disabled={saving || !policyForm.role.trim()}>
                  <UserPlus size={15} /> Add policy
                </button>
              </form>
            )}
            <div className="table-wrap rbac-table">
              <table className="grid summary-table">
                <thead>
                  <tr><th>Role</th><th>Resource</th><th>Action</th><th>Scope</th><th>Created</th><th style={{ width: '80px' }}>Actions</th></tr>
                </thead>
                <tbody>
                  {policies.map((p) => (
                    <tr key={p.id}>
                      <td className="mono">{p.role}</td>
                      <td className="mono">{p.resource}</td>
                      <td className="mono">{p.action}</td>
                      <td className="mono">{p.object}</td>
                      <td className="muted">{fmt(p.created_at)}</td>
                      <td>
                        {canUpdate && (
                          <button type="button" className="btn danger small" title="Delete policy" onClick={() => askDeletePolicy(p)}>
                            <Trash2 size={14} />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                  {policies.length === 0 && <tr><td colSpan={6} className="muted">No database policies yet. The bootstrap file or built-in roles apply.</td></tr>}
                </tbody>
              </table>
            </div>
          </div>
        </article>

        <article className="card">
          <div className="card-head">
            <div>
              <h3>Bindings</h3>
              <span className="sub">OIDC group/claim → role</span>
            </div>
          </div>
          <div className="card-body">
            {canUpdate && (
              <form className="rbac-form" onSubmit={submitBinding}>
                <label>
                  Group / claim
                  <input value={bindingForm.group} onChange={(e) => setBindingForm((b) => ({ ...b, group: e.target.value }))} placeholder="SRE or alice@example.com" required />
                </label>
                <label>
                  Role
                  <input value={bindingForm.role} onChange={(e) => setBindingForm((b) => ({ ...b, role: e.target.value }))} placeholder="role:admin" required />
                </label>
                <button type="submit" className="btn" disabled={saving || !bindingForm.group.trim() || !bindingForm.role.trim()}>
                  <UserPlus size={15} /> Add binding
                </button>
              </form>
            )}
            <div className="table-wrap rbac-table">
              <table className="grid summary-table">
                <thead>
                  <tr><th>Group</th><th>Role</th><th>Created</th><th style={{ width: '80px' }}>Actions</th></tr>
                </thead>
                <tbody>
                  {bindings.map((b) => (
                    <tr key={b.id}>
                      <td className="mono">{b.group}</td>
                      <td className="mono">{b.role}</td>
                      <td className="muted">{fmt(b.created_at)}</td>
                      <td>
                        {canUpdate && (
                          <button type="button" className="btn danger small" title="Delete binding" onClick={() => askDeleteBinding(b)}>
                            <Trash2 size={14} />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                  {bindings.length === 0 && <tr><td colSpan={4} className="muted">No bindings. Built-in roles still exist; the local Basic admin is always role:admin.</td></tr>}
                </tbody>
              </table>
            </div>
          </div>
        </article>
      </div>

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
    </section>
  )
}
