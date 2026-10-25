import { Link } from 'react-router-dom'
import CopyButton from './CopyButton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

export default function ProjectCard({ project, onEdit, onDelete }) {
  const { can } = useAuth()

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
      </header>

      <div className="card-body">
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
            to={`/?project=${encodeURIComponent(project.system ? '__global__' : project.name)}`}
          >
            Applications
          </Link>
          {!project.system && (
            <>
              <button className="btn ghost small" disabled={!can('projects', 'update')} onClick={() => onEdit(project)}>Edit</button>
              <button className="btn danger small" disabled={!can('projects', 'delete')} onClick={() => onDelete(project)}>Delete</button>
            </>
          )}
        </div>
      </footer>
    </article>
  )
}