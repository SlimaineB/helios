import { Link } from 'react-router-dom'
import { PlugZap } from 'lucide-react'
import CopyButton from './CopyButton.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from './GitOpsBadge.jsx'
import { useAuth } from '../App.jsx'
import { projectLabel } from '../api/client.js'

export default function RepositoryCard({ repo, onEdit, onDelete, onCheck, checking }) {
  const { can } = useAuth()
  const readOnly = managedByGitOps(repo)

  return (
    <article className="repo-card">
      <header className="card-head">
        <div className="repo-main">
          <strong>{repo.name || repo.url}</strong>
          <div className="repo-meta">
            <CopyButton value={repo.url} size="small" title="Copy repository URL" />
            {repo.url}
          </div>
        </div>
        <GitOpsBadge obj={repo} />
      </header>

      <div className="card-body">
        <div className="detail-row">
          <div className="detail">
            <label>Project</label>
            <span className="value">{projectLabel(repo.project)}</span>
          </div>
          <div className="detail">
            <label>Auth</label>
            <span className="value">
              {repo.ssh_key_set
                ? 'SSH key'
                : repo.password_set
                  ? `${repo.username || 'token'} / token`
                  : repo.username || <span className="muted">none</span>}
            </span>
          </div>
          <div className="detail">
            <label>Insecure</label>
            <span className="value">{repo.insecure_skip_verify ? 'yes' : 'no'}</span>
          </div>
        </div>
      </div>

      <footer className="card-foot">
        <div className="actions">
          <Link className="btn ghost small" to={`/applications?repo=${encodeURIComponent(repo.url)}`}>Applications</Link>
          <button className="btn ghost small" disabled={checking} onClick={() => onCheck(repo)} title="Test Git connection">
            <PlugZap size={13} /> {checking ? 'Testing…' : 'Test'}
          </button>
          <button className="btn ghost small" disabled={readOnly || !can('repositories', 'update')} title={managedTitle(repo)} onClick={() => onEdit(repo)}>Edit</button>
          <button className="btn danger small" disabled={readOnly || !can('repositories', 'delete')} title={managedTitle(repo)} onClick={() => onDelete(repo)}>Delete</button>
        </div>
      </footer>
    </article>
  )
}