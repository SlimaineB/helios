import { Link } from 'react-router-dom'
import { GitCommit } from 'lucide-react'
import CopyButton from './CopyButton.jsx'
import HealthBadge from './HealthBadge.jsx'
import { projectLabel } from '../api/client.js'

const EMPTY = { instances: 0, drift: 0, error: 0, stuck: 0 }

const shortRepo = (url = '') => url.replace(/^https?:\/\//, '').replace(/^ssh:\/\//, '').replace(/\.git$/, '')

export default function ApplicationCard({ app, stats, repo, can, onEdit, onDelete, onVars, onHistory }) {
  const s = stats || EMPTY
  const unhealthy = s.error + s.stuck
  const health = unhealthy > 0 ? 'failed' : s.instances === 0 ? 'unknown' : s.drift > 0 ? 'drift' : 'ok'
  const healthLabel = unhealthy > 0
    ? `${unhealthy} unhealthy`
    : s.instances === 0
      ? 'Unassigned'
      : s.drift > 0
        ? `${s.drift} in drift`
        : 'Healthy'

  return (
    <article className={`app-card health-${health}`}>
      <header className="card-head">
        <div className="app-main">
          <div className="cell-title">
            <Link className="entity-link" to={`/instances?app=${app.id}`} title="View instances">
              <strong className="app-name">{app.name}</strong>
            </Link>
            <span className="cell-sub">
              <CopyButton value={String(app.id)} size="small" title="Copy app ID" />
              <span className="mono">id {app.id}</span>
              {app.autoheal && <span className="project-badge">auto-heal</span>}
            </span>
          </div>
        </div>
        <div className="card-actions">
          <span className="project-badge">{projectLabel(app.project)}</span>
          <HealthBadge state={health} label={healthLabel} size={16} />
        </div>
      </header>

      <div className="card-body">
        <div className="detail-row">
          <div className="detail">
            <label>Repository</label>
            <div className="value repo-line" title={app.repo_url}>
              <CopyButton value={app.repo_url} size="small" title="Copy repository URL" />
              <span className="repo-label">{repo?.name || shortRepo(app.repo_url)}</span>
            </div>
          </div>
          <div className="detail">
            <label>Revision</label>
            <div className="value">
              <span className="mono">{app.branch}</span>
              <button
                type="button"
                className="rev-link"
                onClick={onHistory}
                title={s.revisionMsg || 'View revision history'}
              >
                <GitCommit size={12} />
                <span className="mono">{s.revision ? s.revision.slice(0, 7) : 'History'}</span>
              </button>
            </div>
          </div>
        </div>

        <div className="detail-row">
          <div className="detail">
            <label>Instances</label>
            <div className="value">
              <Link
                to={`/instances?app=${app.id}`}
                className={`instance-count ${s.instances ? '' : 'empty-link'}`}
                title="View instances running this application"
              >
                {s.instances} instance{s.instances === 1 ? '' : 's'}
              </Link>
              {s.drift > 0 && <span className="cell-sub drift-text">{s.drift} in drift</span>}
            </div>
          </div>
          <div className="detail">
            <label>Sync interval</label>
            <span className="value">{app.sync_interval ? `${app.sync_interval}s` : 'inherit (0)'}</span>
          </div>
        </div>
      </div>

      <footer className="card-foot">
        <div className="actions">
          <Link className="btn small" to={`/instances?app=${app.id}`}>Instances</Link>
          <button className="btn ghost small" disabled={!can('applications', 'update')} onClick={() => onEdit(app)}>Edit</button>
          <button className="btn ghost small" disabled={!can('applications', 'update')} onClick={() => onVars(app)}>Vars</button>
          <button className="btn danger small" disabled={!can('applications', 'delete')} onClick={() => onDelete(app)}>Delete</button>
        </div>
      </footer>
    </article>
  )
}
