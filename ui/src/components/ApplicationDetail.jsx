import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  ArrowLeft,
  ExternalLink,
  GitBranch,
  GitCommit,
  History,
  Pencil,
  Server,
  Settings2,
  Trash2,
  Variable,
} from 'lucide-react'
import Badge from './Badge.jsx'
import AutoHealChip from './AutoHealChip.jsx'
import HealthBadge from './HealthBadge.jsx'
import CopyButton from './CopyButton.jsx'
import QuickMenu from './QuickMenu.jsx'
import { api, projectLabel } from '../api/client.js'
import { toast } from '../components/Toast.jsx'

const shortRepo = (url = '') =>
  url.replace(/^https?:\/\//, '').replace(/^ssh:\/\//, '').replace(/\.git$/, '')

const appState = (assignment) => {
  if (assignment?.stuck) return 'stuck'
  const s = assignment?.latest_check?.status
  if (s === 'failed') return 'error'
  return s === 'ok' || s === 'drift' ? s : 'unknown'
}

const STATE_LABEL = {
  ok: 'Synced',
  drift: 'Drift',
  error: 'Failed',
  stuck: 'Stuck',
  unknown: 'No check',
}

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')

export default function ApplicationDetail({
  app,
  stats,
  repo,
  instances,
  instancesTotal = 0,
  availableInstances,
  availableTotal = 0,
  can,
  onEdit,
  onVars,
  onDelete,
  onHistory,
  onUnassign,
  onAssign,
}) {
  const [assignTo, setAssignTo] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [toggling, setToggling] = useState(false)
  const [autoheal, setAutoheal] = useState(app.autoheal)

  useEffect(() => { setAutoheal(app.autoheal) }, [app.id, app.autoheal])

  const toggleAppAutoHeal = async () => {
    if (!can('applications', 'update') || toggling) return
    setToggling(true)
    try {
      await api.setAppAutoHeal(app.id, !autoheal)
      setAutoheal(!autoheal)
    } catch (e) {
      toast.error(e.message || 'Failed to update auto-apply')
    } finally {
      setToggling(false)
    }
  }
  const s = stats || { instances: 0, drift: 0, error: 0, stuck: 0 }
  const unhealthy = s.error + s.stuck
  const health = unhealthy > 0
    ? 'failed'
    : s.instances === 0
      ? 'unknown'
      : s.drift > 0
        ? 'drift'
        : 'ok'
  const healthLabel = unhealthy > 0
    ? `${unhealthy} failed`
    : s.instances === 0
      ? 'Unassigned'
      : s.drift > 0
        ? `${s.drift} in drift`
        : 'Synced'

  const repoLabel = repo?.name && repo.name !== app.repo_url
    ? repo.name
    : shortRepo(app.repo_url)

  const assigned = (instances || [])
    .map((inst) => ({
      inst,
      assignment: (inst.apps || []).find(
        (a) => String(a.application?.id) === String(app.id),
      ),
    }))
    .filter(({ assignment }) => !!assignment)

  const available = availableInstances || []

  const assign = async () => {
    const inst = available.find((i) => i.id === assignTo)
    if (!inst || !onAssign) return
    setAssigning(true)
    try {
      await onAssign(inst)
      setAssignTo('')
    } finally {
      setAssigning(false)
    }
  }

  return (
    <section className="app-detail-page">
      <Link to="/applications" className="back-link">
        <ArrowLeft size={14} /> Back to applications
      </Link>

      <div className="detail-page-head">
        <div className="detail-page-title">
          <h1>{app.name}</h1>
          <div className="detail-chips">
            <HealthBadge state={health} label={healthLabel} size={16} />
            <span className="project-badge">{projectLabel(app.project)}</span>
            <span className="detail-chip"><Server size={13} /> {s.instances} instance{s.instances === 1 ? '' : 's'}</span>
            <span className="detail-chip"><GitBranch size={13} /> {app.branch || '—'}</span>
            {s.revision && (
              <span className="detail-chip"><GitCommit size={13} /> {s.revision.slice(0, 7)}</span>
            )}
          </div>
        </div>
        <div className="detail-head-actions">
          <Link className="btn ghost" to={`/instances?app=${app.id}`}>
            <Server size={15} /> Open fleet
          </Link>
          <button type="button" className="btn ghost" disabled={!can('applications', 'update')} onClick={() => onEdit(app)}>
            <Pencil size={15} /> Edit
          </button>
          <button type="button" className="btn ghost" disabled={!can('applications', 'update')} onClick={() => onVars(app)}>
            <Variable size={15} /> Variables
          </button>
          <button type="button" className="btn ghost" onClick={() => onHistory(app)}>
            <History size={15} /> History
          </button>
          <button type="button" className="btn danger" disabled={!can('applications', 'delete')} onClick={() => onDelete(app)}>
            <Trash2 size={15} /> Delete
          </button>
        </div>
      </div>

      <div className="host-setting-bar">
        <div className="host-setting-copy">
          <strong>Auto-apply on drift</strong>
          <span className="sub muted">Applies to every host unless the host disables auto-apply or sets a per-host override. Most restrictive wins.</span>
        </div>
        <AutoHealChip
          enabled={autoheal}
          scope="app"
          reason={autoheal
            ? 'Auto-apply is enabled for this application on every host (unless a host disables it)'
            : 'Auto-apply is disabled for this application on every host'}
        />
        <label className="switch" title={`Auto-apply on drift is ${autoheal ? 'enabled' : 'disabled'} for every host`}>
          <input
            type="checkbox"
            checked={autoheal}
            disabled={!can('applications', 'update') || toggling}
            onChange={() => toggleAppAutoHeal()}
          />
          <span className="slider" />
        </label>
      </div>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{s.instances}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{s.synced}</span><span className="stat-label">Synced</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{s.drift}</span><span className="stat-label">Drift</span></div>
        <div className="stat-card"><span className="stat-dot error" /><span className="stat-num">{unhealthy}</span><span className="stat-label">Failed</span></div>
      </div>

      <details className="detail-panel" open>
        <summary><Settings2 size={15} /> Configuration</summary>
        <div className="detail-panel-body">
          <div className="host-detail-grid app-config-grid">
            <div className="detail detail-wide">
              <label>Repository</label>
              <div className="value repo-line">
                <CopyButton value={app.repo_url} size="small" title="Copy repository URL" />
                <span className="repo-label">{repoLabel}</span>
              </div>
              <span className="cell-sub mono">{app.repo_url}</span>
            </div>
            <div className="detail">
              <label>Playbook path</label>
              <div className="value mono">{app.path || '—'}</div>
            </div>
            <div className="detail">
              <label>Branch</label>
              <div className="value mono">{app.branch || '—'}</div>
            </div>
            <div className="detail">
              <label>Check interval</label>
              <div className="value">{app.sync_interval ? `${app.sync_interval}s` : 'inherit (0)'}</div>
            </div>
            <div className="detail">
              <label>Latest revision</label>
              <div className="value">
                <span className="mono"><GitCommit size={12} /> {s.revision ? s.revision.slice(0, 7) : '—'}</span>
                {s.revisionMsg && <span className="cell-sub">{s.revisionMsg}</span>}
              </div>
            </div>
            <div className="detail">
              <label>Last activity</label>
              <div className="value">{fmt(s.last_at)}</div>
            </div>
            <div className="detail">
              <label>Application ID</label>
              <div className="value mono">
                <CopyButton value={String(app.id)} label="" size="small" title="Copy application ID" />{app.id}
              </div>
            </div>
          </div>

          {app.ignore_paths && (
            <div className="detail-subsection">
              <label>Ignore drift paths</label>
              <pre className="output">{app.ignore_paths}</pre>
            </div>
          )}
        </div>
      </details>

      <h2 className="detail-section-heading">
        <Server size={16} /> Assigned instances
        <span className="muted detail-count">{assigned.length}</span>
      </h2>

      {onAssign && available.length > 0 && (
        <div className="filters">
          <label htmlFor="assign-instance">Assign instance</label>
          <select
            id="assign-instance"
            value={assignTo}
            onChange={(e) => setAssignTo(e.target.value)}
          >
            <option value="">Choose an instance…</option>
            {available.map((inst) => (
              <option key={inst.id} value={inst.id}>
                {inst.name} · {projectLabel(inst.project)}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="btn"
            disabled={!assignTo || assigning || !can('instances', 'update')}
            onClick={assign}
          >
            {assigning ? 'Assigning…' : 'Assign'}
          </button>
          {availableTotal > available.length && (
            <span className="sub muted">Showing the first {available.length} of {availableTotal} choices.</span>
          )}
        </div>
      )}

      {assigned.length === 0 ? (
        <div className="empty">
          <p className="muted">This application is not assigned to any instance yet.</p>
        </div>
      ) : (
        <>
          <div className="table-wrap app-instances-table">
            <table className="grid">
              <thead>
                <tr>
                  <th>Instance</th>
                  <th style={{ width: '160px' }}>Project</th>
                  <th style={{ width: '120px' }}>State</th>
                  <th style={{ width: '190px' }}>Last check</th>
                  <th style={{ width: '56px' }} aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {assigned.map(({ inst, assignment }) => {
                  const state = appState(assignment)
                  return (
                    <tr key={inst.id}>
                      <td>
                        <div className="cell-title">
                          <Link className="entity-link" to={`/instances/${inst.id}`}>
                            <strong>{inst.name}</strong>
                          </Link>
                          <span className="cell-sub mono">{inst.id}</span>
                        </div>
                      </td>
                      <td><span className="project-badge">{projectLabel(inst.project)}</span></td>
                      <td><Badge state={state} label={STATE_LABEL[state]} small /></td>
                      <td className="muted">{fmt(assignment.last_check_at || assignment.latest_check?.finished_at)}</td>
                      <td>
                        <QuickMenu
                          label={`Actions for ${inst.name}`}
                          items={[
                            {
                              label: state === 'drift' ? 'Review drift' : 'Open instance',
                              icon: ExternalLink,
                              to: `/instances/${inst.id}`,
                            },
                            onUnassign ? { separator: true } : null,
                            onUnassign
                              ? {
                                  label: 'Unassign application',
                                  icon: Trash2,
                                  danger: true,
                                  disabled: !can('instances', 'update'),
                                  onClick: () => onUnassign(inst),
                                }
                              : null,
                          ]}
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          {instancesTotal > assigned.length && (
            <p className="table-note muted">
              Showing {assigned.length} of {instancesTotal} assigned instances. Open the Instances view to page through all of them.
            </p>
          )}
        </>
      )}
    </section>
  )
}
