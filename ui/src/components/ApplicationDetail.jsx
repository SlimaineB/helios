import { useState } from 'react'
import { Link } from 'react-router-dom'
import { GitCommit } from 'lucide-react'
import Modal from './Modal.jsx'
import Badge from './Badge.jsx'
import HealthBadge from './HealthBadge.jsx'
import CopyButton from './CopyButton.jsx'
import { projectLabel } from '../api/client.js'

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
  error: 'Error',
  stuck: 'Stuck',
  unknown: 'No check',
}

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')

export default function ApplicationDetail({
  app,
  stats,
  repo,
  instances,
  can,
  onClose,
  onEdit,
  onVars,
  onDelete,
  onHistory,
  onUnassign,
  onAssign,
}) {
  const [assignTo, setAssignTo] = useState('')
  const [assigning, setAssigning] = useState(false)
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
    ? `${unhealthy} unhealthy`
    : s.instances === 0
      ? 'Unassigned'
      : s.drift > 0
        ? `${s.drift} in drift`
        : 'Healthy'

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

  const assignedIds = new Set(assigned.map(({ inst }) => inst.id))
  const available = (instances || []).filter((inst) =>
    (inst.project || '') === (app.project || '') && !assignedIds.has(inst.id)
  )

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
    <Modal title={`Application · ${app.name}`} onClose={onClose} wide>
      <div className="app-detail-head">
        <div>
          <strong className="app-detail-name">{app.name}</strong>
          <div className="app-detail-sub">
            <span className="project-badge">{projectLabel(app.project)}</span>
            <CopyButton value={String(app.id)} size="small" title="Copy application ID" />
            <span className="mono">id {app.id}</span>
            {app.autoheal && <span className="project-badge">auto-heal</span>}
          </div>
        </div>
        <HealthBadge state={health} label={healthLabel} size={16} />
      </div>

      <div className="app-detail-actions">
        <Link className="btn small" to={`/instances?app=${app.id}`}>Instances</Link>
        <button type="button" className="btn ghost small" disabled={!can('applications', 'update')} onClick={() => onEdit(app)}>Edit</button>
        <button type="button" className="btn ghost small" disabled={!can('applications', 'update')} onClick={() => onVars(app)}>Variables</button>
        <button type="button" className="btn ghost small" onClick={() => onHistory(app)}>History</button>
        <button type="button" className="btn danger small" disabled={!can('applications', 'delete')} onClick={() => onDelete(app)}>Delete</button>
      </div>

      <div className="app-detail-grid">
        <div className="detail">
          <label>Repository</label>
          <div className="value repo-line">
            <CopyButton value={app.repo_url} size="small" title="Copy repository URL" />
            <span className="repo-label">{repoLabel}</span>
          </div>
          <span className="cell-sub mono">{app.repo_url}</span>
        </div>
        <div className="detail">
          <label>Revision</label>
          <div className="value mono">{app.branch || '—'}</div>
        </div>
        <div className="detail">
          <label>Playbook path</label>
          <div className="value mono">{app.path || '—'}</div>
        </div>
        <div className="detail">
          <label>Sync interval</label>
          <div className="value">{app.sync_interval ? `${app.sync_interval}s` : 'inherit (0)'}</div>
        </div>
        <div className="detail">
          <label>Auto-heal</label>
          <div className="value">{app.autoheal ? 'on' : 'off'}</div>
        </div>
        <div className="detail">
          <label>Latest revision</label>
          <div className="value">
            <span className="mono">
              <GitCommit size={12} /> {s.revision ? s.revision.slice(0, 7) : '—'}
            </span>
            {s.revisionMsg && <span className="cell-sub">{s.revisionMsg}</span>}
          </div>
        </div>
      </div>

      {app.ignore_paths && (
        <div className="app-detail-ignore">
          <label>Ignore drift paths</label>
          <pre className="output">{app.ignore_paths}</pre>
        </div>
      )}

      <h3>Assigned instances</h3>
      {onAssign && (
        <div className="app-assign">
          {available.length > 0 ? (
            <>
              <select
                value={assignTo}
                onChange={(e) => setAssignTo(e.target.value)}
                aria-label="Assign to instance"
              >
                <option value="">Assign to instance…</option>
                {available.map((inst) => (
                  <option key={inst.id} value={inst.id}>
                    {inst.name} · {projectLabel(inst.project)}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="btn small"
                disabled={!assignTo || assigning || !can('instances', 'update')}
                onClick={assign}
              >
                {assigning ? 'Assigning…' : 'Assign'}
              </button>
            </>
          ) : (
            <span className="muted">No unassigned instance in this project.</span>
          )}
        </div>
      )}
      {assigned.length === 0 ? (
        <p className="muted">This application is not assigned to any instance yet.</p>
      ) : (
        <div className="table-wrap">
          <table className="grid">
            <thead>
              <tr>
                <th>Instance</th>
                <th style={{ width: '130px' }}>Project</th>
                <th style={{ width: '120px' }}>State</th>
                <th style={{ width: '190px' }}>Last check</th>
                <th style={{ width: '190px' }}>Actions</th>
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
                      <div className="app-actions">
                        <Link className="btn ghost small" to={`/instances/${inst.id}`}>Open</Link>
                        {onUnassign && (
                          <button
                            type="button"
                            className="btn danger small"
                            disabled={!can('instances', 'update')}
                            onClick={() => onUnassign(inst)}
                            title="Remove this application from this instance only"
                          >
                            Unassign
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </Modal>
  )
}
