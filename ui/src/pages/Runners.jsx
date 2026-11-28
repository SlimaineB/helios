import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Activity,
  AlertTriangle,
  Clock,
  KeyRound,
  Server,
  Settings2,
  ShieldCheck,
  Terminal,
} from 'lucide-react'
import { api, projectLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import Modal from '../components/Modal.jsx'
import { SkeletonTable } from '../components/Skeleton.jsx'
import TagList, { formatTags, parseTags } from '../components/TagList.jsx'
import { toast } from '../components/Toast.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'
import { useAuth } from '../App.jsx'

const fmt = (value) => {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? '—' : d.toLocaleString()
}

const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`

const JOB_STATE = { queued: 'queued', running: 'running', ok: 'ok', drift: 'drift', failed: 'failed' }

const emptyRunner = { id: '', tags: '', max_parallel: 2 }

export default function Runners() {
  const { can } = useAuth()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState(emptyRunner)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    try {
      setData(await api.runners())
      setError('')
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 15000 })

  const openNew = () => {
    setEditing(null)
    setForm(emptyRunner)
    setFormError('')
    setShowForm(true)
  }

  const openEdit = (runner) => {
    setEditing(runner)
    setForm({
      id: runner.id,
      tags: formatTags(runner.tags),
      max_parallel: runner.max_parallel || 2,
    })
    setFormError('')
    setShowForm(true)
  }

  const closeForm = () => {
    setShowForm(false)
    setEditing(null)
    setFormError('')
  }

  const submit = async (e) => {
    e.preventDefault()
    setSaving(true)
    setFormError('')
    try {
      const body = {
        tags: parseTags(form.tags),
        max_parallel: Number(form.max_parallel) || 2,
      }
      if (editing) await api.updateRunner(editing.id, body)
      else await api.createRunner({ ...body, id: form.id.trim() })
      toast.success(editing ? 'Runner updated' : 'Runner added')
      closeForm()
      refresh()
    } catch (err) {
      setFormError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const askDelete = (runner) => setConfirm({
    title: 'Delete runner',
    message: `Delete "${runner.id}"? Stop its helios-runner process first, otherwise it will register again on the next heartbeat. Any running job is requeued for another runner.`,
    variant: 'danger',
    onConfirm: () => doDelete(runner),
  })

  const doDelete = async (runner) => {
    try {
      await api.deleteRunner(runner.id)
      toast.success('Runner deleted')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  if (loading && !data) {
    return (
      <section>
        <h1><Terminal size={22} /> Runners</h1>
        <SkeletonTable rows={3} columns={5} />
      </section>
    )
  }

  const summary = data?.summary || {}
  const runners = data?.runners || []
  const jobs = data?.jobs || []
  const enabled = !!data?.enabled

  return (
    <section>
      <h1><Terminal size={22} /> Runners</h1>
      <p className="page-intro">
        Stateless execution workers for <strong>runner-mode applications</strong>. Each runner
        heartbeats with a set of tags, claims a matching job, runs Ansible against the application
        inventory and reports the result. Nothing is installed on the managed hosts. Agent-mode hosts
        run Ansible locally and do not use a runner.
      </p>

      {!enabled && (
        <div className="settings-note warn">
          <AlertTriangle size={15} />
          <span>
            Runner API disabled. Set <code>HELIOS_RUNNER_TOKEN</code> on the hub, then start{' '}
            <code>helios-runner</code> with the same token.
          </span>
        </div>
      )}
      {error && <div className="settings-note warn"><AlertTriangle size={15} /><span>{error}</span></div>}

      <div className="stats">
        <div className="stat-card">
          <span className={`stat-dot ${enabled ? 'ok' : 'error'}`} />
          <span className="stat-num">{enabled ? 'On' : 'Off'}</span>
          <span className="stat-label">Runner API</span>
        </div>
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{summary.runners || 0}</span><span className="stat-label">Runners</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{summary.online || 0}</span><span className="stat-label">Online</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{summary.queued || 0}</span><span className="stat-label">Queued</span></div>
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{summary.running || 0}</span><span className="stat-label">Running</span></div>
      </div>

      <div className="runner-grid">
        <article className="card runner-card">
          <div className="card-head">
            <div>
              <h3><Terminal size={16} /> Runner worker</h3>
              <span className="sub">Portable process or container with Ansible and SSH access</span>
            </div>
            <span className={`state-badge ${enabled ? 'enabled' : 'disabled'}`}>
              {enabled ? 'enabled' : 'disabled'}
            </span>
          </div>
          <div className="card-body">
            <div className="runner-facts">
              <div className="detail">
                <label>Authentication</label>
                <div className="value"><KeyRound size={13} /> shared bearer token</div>
              </div>
              <div className="detail">
                <label>Connection</label>
                <div className="value">Ansible over SSH</div>
              </div>
              <div className="detail">
                <label>Work dispatch</label>
                <div className="value">tag-matched jobs from the hub</div>
              </div>
              <div className="detail">
                <label>State</label>
                <div className="value"><ShieldCheck size={13} /> stateless; repo mirrors are disposable</div>
              </div>
            </div>
            <p className="sub runner-copy">
              Runners poll <code>/api/v1/runner/jobs/claim</code>, run the job and post its result.
              A crashed runner stops heartbeating; its running job is requeued automatically after
              30 minutes so another runner can retry it.
            </p>
          </div>
        </article>

        <article className="card runner-card">
          <div className="card-head">
            <div>
              <h3><ShieldCheck size={16} /> Execution modes</h3>
              <span className="sub">Who runs Ansible, and where</span>
            </div>
          </div>
          <div className="card-body runner-modes">
            <div className="runner-mode">
              <span className="runner-mode-icon agent"><Server size={17} /></span>
              <div>
                <strong>Agent</strong>
                <p>The Helios agent runs <code>ansible-playbook</code> on the managed host. Ansible must be installed there. No runner involved.</p>
              </div>
              <span className="runner-mode-tag">no runner</span>
            </div>
            <div className="runner-mode">
              <span className="runner-mode-icon ssh"><Terminal size={17} /></span>
              <div>
                <strong>Runner</strong>
                <p>A tagged runner runs Ansible against the application inventory. Nothing is installed on the target hosts; they only need SSH access.</p>
              </div>
              <span className="runner-mode-tag accent">runner</span>
            </div>
          </div>
        </article>
      </div>

      <h2 className="detail-section-heading">
        <Activity size={16} /> Fleet
        <span className="muted detail-count">{runners.length}</span>
      </h2>

      <div className="filters">
        <span className="muted">
          {runners.length} registered · {summary.online || 0} online
        </span>
        <button type="button" className="filter-add" onClick={openNew} disabled={!can('instances', 'create')}>
          ＋ New runner
        </button>
      </div>

      {runners.length === 0 ? (
        <div className="empty">
          <p className="muted">No runner has heartbeated yet. Start <code>helios-runner</code> with the hub token.</p>
          <Link className="btn ghost small" to="/applications">Open applications</Link>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid">
            <thead>
              <tr>
                <th>Runner</th>
                <th style={{ width: '240px' }}>Tags</th>
                <th style={{ width: '110px' }}>Max parallel</th>
                <th style={{ width: '240px' }}>Runtime</th>
                <th style={{ width: '130px' }}>State</th>
                <th style={{ width: '200px' }}>Last seen</th>
                <th style={{ width: '170px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {runners.map((runner) => (
                <tr key={runner.id}>
                  <td><span className="mono"><strong>{runner.id}</strong></span></td>
                  <td>
                    {(runner.tags || []).length ? <TagList tags={runner.tags} /> : <span className="muted">any</span>}
                  </td>
                  <td className="mono">{runner.max_parallel || 2}</td>
                  <td className="runtime">
                    {(() => {
                      const rt = runner.runtime || {}
                      const parts = []
                      if (rt.ansible) parts.push(<span key="a" className="mono">Ansible {rt.ansible}</span>)
                      if (rt.python) parts.push(<span key="p" className="mono muted">· Python {rt.python}</span>)
                      if (!parts.length) return <span className="muted">—</span>
                      return (
                        <>
                          <div style={{ lineHeight: '1.4' }}>{parts}</div>
                          {rt.collections?.length && (
                            <div className="muted" style={{ fontSize: '11px', marginTop: '3px' }} title={rt.collections.map((c) => `${c.name} ${c.version}`).join(', ')}>
                              {rt.collections.length} collections
                            </div>
                          )}
                        </>
                      )
                    })()}
                  </td>
                  <td><Badge state={runner.online ? 'ok' : 'offline'} label={runner.online ? 'online' : 'offline'} small /></td>
                  <td className="muted">{fmt(runner.last_seen)}</td>
                  <td>
                    <div className="row-actions">
                      <button className="btn ghost small" disabled={!can('instances', 'update')} onClick={() => openEdit(runner)}>Edit</button>
                      <button className="btn danger small" disabled={!can('instances', 'delete')} onClick={() => askDelete(runner)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h2 className="detail-section-heading">
        <Clock size={16} /> Recent jobs
        <span className="muted detail-count">{jobs.length}</span>
      </h2>

      {jobs.length === 0 ? (
        <div className="empty"><p className="muted">No job yet. Run a runner-mode application from the Applications page.</p></div>
      ) : (
        <div className="table-wrap">
          <table className="grid">
            <thead>
              <tr>
                <th>Application</th>
                <th style={{ width: '110px' }}>Mode</th>
                <th style={{ width: '110px' }}>Status</th>
                <th style={{ width: '180px' }}>Runner</th>
                <th style={{ width: '200px' }}>Created</th>
                <th style={{ width: '200px' }}>Finished</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.id}>
                  <td>
                    <div className="cell-title">
                      <Link className="entity-link" to={`/applications/${job.app_id}`}><strong>{job.app_name || `app ${job.app_id}`}</strong></Link>
                      <span className="cell-sub">{projectLabel(job.project)} · job {job.id}</span>
                      {job.error && <span className="cell-sub error">{job.error}</span>}
                    </div>
                  </td>
                  <td><span className="mono">{job.mode}</span></td>
                  <td><Badge state={JOB_STATE[job.status] || 'unknown'} label={job.status} small /></td>
                  <td className="mono muted">{job.runner_id || '—'}</td>
                  <td className="muted">{fmt(job.created_at)}</td>
                  <td className="muted">{fmt(job.finished_at)}</td>
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
        <Modal title={editing ? `Edit ${editing.id}` : 'New runner'} onClose={closeForm}>
          <form className="form-card" onSubmit={submit}>
            <label>
              Runner ID
              <input
                required
                autoFocus={!editing}
                disabled={!!editing}
                value={form.id}
                onChange={(e) => setForm((prev) => ({ ...prev, id: e.target.value }))}
                placeholder="runner-eu-1"
              />
              <span className="sub">Must match <code>HELIOS_RUNNER_ID</code> on the runner process.</span>
            </label>
            <label>
              Tags <span className="muted">(comma separated)</span>
              <input
                value={form.tags}
                onChange={(e) => setForm((prev) => ({ ...prev, tags: e.target.value }))}
                placeholder="eu, prod, web"
              />
              <span className="sub">A runner receives only jobs whose required tags it advertises.</span>
            </label>
            <label>
              Max parallel jobs
              <input
                type="number"
                min="1"
                max="64"
                required
                value={form.max_parallel}
                onChange={(e) => setForm((prev) => ({ ...prev, max_parallel: e.target.value }))}
              />
              <span className="sub">Hub-side claim limit. The runner process also caps itself with <code>HELIOS_RUNNER_MAX_CONCURRENT</code>.</span>
            </label>
            {formError && <div className="error">{formError}</div>}
            <div className="form-actions">
              <button type="button" className="ghost" onClick={closeForm}>Cancel</button>
              <button type="submit" disabled={saving || !form.id.trim()}>
                {saving ? 'Saving…' : editing ? 'Save' : 'Add runner'}
              </button>
            </div>
          </form>
        </Modal>
      )}

      <details className="detail-panel">
        <summary><Settings2 size={15} /> Runner configuration</summary>
        <div className="detail-panel-body">
          <p className="sub">
            Hub and runner must share the same runner token. Mount the SSH identity used by the
            application inventories.
          </p>
          <pre className="output">{`# Hub
HELIOS_RUNNER_TOKEN=...

# Runner
HELIOS_SERVER=https://hub.example.com
HELIOS_RUNNER_TOKEN=...
HELIOS_RUNNER_TAGS=eu,prod,linux
HELIOS_RUNNER_SSH_USER=ansible
HELIOS_RUNNER_SSH_KEY=/etc/helios/runner_id
# optional: parallel playbooks (default 2)
HELIOS_RUNNER_MAX_CONCURRENT=2
# optional: stable identity (otherwise generated in the state dir)
HELIOS_RUNNER_ID=runner-eu-1

helios-runner`}</pre>
          <p className="sub runner-config-note">
            <Clock size={13} /> Heartbeat and claim intervals default to 10s. Scale by adding runner
            processes or pods; tags keep jobs on the right network segment.
          </p>
        </div>
      </details>
    </section>
  )
}
