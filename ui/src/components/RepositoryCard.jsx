import { Link } from 'react-router-dom'
import CopyButton from './CopyButton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

export default function RepositoryCard({ repo, onEdit, onDelete }) {
  const { can } = useAuth()

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
      </header>

      <div className="card-body">
        <div className="detail-row">
          <div className="detail">
            <label>Auth</label>
            <span className="value">
              {repo.ssh_key ? 'SSH key' : repo.username ? `${repo.username} / token` : <span className="muted">none</span>}
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
          <Link className="btn ghost small" to={`/?repo=${encodeURIComponent(repo.url)}`}>Applications</Link>
          <button className="btn ghost small" disabled={!can('repositories', 'update')} onClick={() => onEdit(repo)}>Edit</button>
          <button className="btn danger small" disabled={!can('repositories', 'delete')} onClick={() => onDelete(repo)}>Delete</button>
        </div>
      </footer>
    </article>
  )
}