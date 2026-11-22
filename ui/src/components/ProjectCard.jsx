import { Link } from 'react-router-dom'
import CopyButton from './CopyButton.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from './GitOpsBadge.jsx'
import TagList from './TagList.jsx'
import { useAuth } from '../App.jsx'

export default function ProjectCard({ project, summary, onEdit, onDelete }) {
  const { can } = useAuth()
  const readOnly = !project.system && managedByGitOps(project)

  return (
    <article className="project-card">
      <header className="card-head">
        <div className="project-main">
          <strong>
            {!project.system && <CopyButton value={project.name} size="small" title="Copy project name" />}
            {project.name}
          </strong>
          <span className="project-desc">
            {project.system ? 'Built-in global project — cannot be renamed or deleted' : 'RBAC object scope for applications and instances'}
          </span>
        </div>
        {project.system && <span className="project-badge">system</span>}
        {!project.system && <GitOpsBadge obj={project} />}
      </header>

      <div className="card-body">
        <div className="project-metrics">
          <span><b>{summary?.apps ?? 0}</b> applications</span>
          <span><b>{summary?.instances ?? 0}</b> instances</span>
          <span><b>{summary?.online ?? 0}</b> online</span>
          {summary?.states?.has?.('drift') && <span className="drift-text">drift detected</span>}
          {summary?.states?.has?.('error') && <span className="error-text">failures</span>}
        </div>
        {summary?.tags?.length > 0 && (
          <div className="project-tags"><TagList tags={summary.tags} /></div>
        )}
        <div className="detail">
          <label>Allowed repositories</label>
          <div className="value mono">
            {project.repo_patterns ? (
              <>
                <CopyButton value={project.repo_patterns} size="small" title="Copy patterns" />
                <pre className="patterns">{project.repo_patterns}</pre>
              </>
            ) : (
              <span className="muted">any repository (no restriction)</span>
            )}
          </div>
        </div>
      </div>

      <footer className="card-foot">
        <div className="actions">
          <Link
            className="btn ghost small"
            to={`/instances?project=${encodeURIComponent(project.system ? '__global__' : project.name)}`}
          >
            Instances
          </Link>
          <Link
            className="btn ghost small"
            to={`/applications?project=${encodeURIComponent(project.system ? '__global__' : project.name)}`}
          >
            Applications
          </Link>
          {!project.system && (
            <>
              <button className="btn ghost small" disabled={readOnly || !can('projects', 'update')} title={managedTitle(project)} onClick={() => onEdit(project)}>Edit</button>
              <button className="btn danger small" disabled={readOnly || !can('projects', 'delete')} title={managedTitle(project)} onClick={() => onDelete(project)}>Delete</button>
            </>
          )}
        </div>
      </footer>
    </article>
  )
}