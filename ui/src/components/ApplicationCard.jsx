import { Link } from 'react-router-dom'
import { ClipboardCheck, GitCommit, Network, Pencil, Play, History, Trash2, Variable, ExternalLink } from 'lucide-react'
import CopyButton from './CopyButton.jsx'
import HealthBadge from './HealthBadge.jsx'
import AutoHealChip from './AutoHealChip.jsx'
import QuickMenu from './QuickMenu.jsx'
import { projectLabel } from '../api/client.js'

const EMPTY = { instances: 0, synced: 0, unknown: 0, drift: 0, error: 0, stuck: 0, autoheal: 0 }

const shortRepo = (url = '') => url.replace(/^https?:\/\//, '').replace(/^ssh:\/\//, '').replace(/\.git$/, '')

export default function ApplicationCard({ app, stats, repo, focused, can, onDetail, onEdit, onDelete, onVars, onHistory, onCheckAll, onApplyAll }) {
  const s = { ...EMPTY, ...(stats || {}) }
  const unhealthy = s.error + s.stuck
  const health = unhealthy > 0
    ? 'failed'
    : s.drift > 0
      ? 'drift'
      : s.instances === 0 || s.synced < s.instances
        ? 'unknown'
        : 'ok'
  const healthLabel = unhealthy > 0
    ? `${unhealthy} failed`
    : s.drift > 0
      ? `${s.drift} drifted`
      : s.instances === 0
        ? 'Unassigned'
        : s.synced === s.instances
          ? 'Synced'
          : `${s.synced}/${s.instances} synced`

  const canRun = s.instances > 0 && can('commands', 'create')
  const menu = [
    { label: 'View nodes', icon: Network, to: `/instances?app=${app.id}` },
    { label: 'Check all nodes', icon: ClipboardCheck, onClick: onCheckAll, disabled: !canRun },
    { label: 'Apply to all nodes', icon: Play, onClick: onApplyAll, disabled: !canRun },
    { label: 'History', icon: History, onClick: onHistory },
    { separator: true },
    { label: 'Edit', icon: Pencil, onClick: onEdit, disabled: !can('applications', 'update') },
    { label: 'Variables', icon: Variable, onClick: onVars, disabled: !can('applications', 'update') },
    { separator: true },
    { label: 'Delete', icon: Trash2, onClick: onDelete, disabled: !can('applications', 'delete'), danger: true },
  ]

  return (
    <article id={`app-${app.id}`} className={`app-card health-${health}${focused ? ' is-focused' : ''}`}>
      <header className="card-head">
        <div className="app-main">
          <div className="cell-title">
            <button
              type="button"
              className="app-name-link"
              onClick={() => onDetail(app)}
              title="Open application details"
            >
              <strong className="app-name">{app.name}</strong>
            </button>
            <span className="cell-sub">
              <CopyButton value={String(app.id)} size="small" title="Copy app ID" />
              <span className="mono">id {app.id}</span>
            </span>
          </div>
        </div>
        <div className="card-actions">
          <span className="project-badge">{projectLabel(app.project)}</span>
          <HealthBadge state={health} label={healthLabel} size={16} />
          <QuickMenu label={`Actions for ${app.name}`} items={menu} />
        </div>
      </header>

      <div className="card-body">
        <div className="kpi-row">
          <div className="kpi">
            <label>Nodes</label>
            <strong>{s.instances}</strong>
            <span>{s.synced} synced{s.unknown ? ` · ${s.unknown} unknown` : ''}</span>
          </div>
          <div className="kpi">
            <label>Drift</label>
            <strong className={s.drift ? 'drift-text' : ''}>{s.drift}</strong>
            <span>{unhealthy} failed</span>
          </div>
          <div className="kpi">
            <label>Auto-heal</label>
            <strong>{app.autoheal ? 'On' : 'Off'}</strong>
            <span>{s.autoheal}/{s.instances} hosts</span>
          </div>
        </div>

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
              <button
                type="button"
                className="rev-link"
                onClick={onHistory}
                title={s.revisionMsg || 'View revision history'}
              >
                <GitCommit size={12} />
                <span className="mono">{s.revision ? s.revision.slice(0, 7) : 'History'}</span>
              </button>
              <span className="mono rev-branch">{app.branch}</span>
            </div>
          </div>
        </div>
      </div>

      <footer className="card-foot">
        <div className="actions">
          <AutoHealChip
            enabled={!!app.autoheal}
            scope="app"
            label={app.autoheal ? `auto-heal · ${s.autoheal}/${s.instances} hosts` : 'auto-heal off'}
            reason={app.autoheal ? 'Drift is applied automatically on hosts where auto-heal is enabled' : 'Drift is detected but never applied automatically'}
          />
          <button type="button" className="btn ghost small" disabled={!canRun} onClick={onCheckAll} title="Check every node running this application">
            <ClipboardCheck size={13} /> Check
          </button>
          <button type="button" className="btn warn small" disabled={!canRun} onClick={onApplyAll} title="Apply this application to every node running it">
            <Play size={13} /> Apply
          </button>
          <Link
            className={`btn small${s.drift > 0 ? ' warn' : ''}`}
            to={`/instances?app=${app.id}`}
            title={s.drift > 0 ? 'Review nodes with drift' : 'View nodes running this application'}
          >
            {s.drift > 0 ? 'Review drift' : 'View nodes'}
          </Link>
          <button type="button" className="btn ghost small" onClick={() => onDetail(app)}>
            <ExternalLink size={13} /> Details
          </button>
        </div>
      </footer>
    </article>
  )
}
