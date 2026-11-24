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
import { SkeletonTable } from '../components/Skeleton.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const fmt = (value) => {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? '—' : d.toLocaleString()
}

const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`

export default function Runners() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

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
  useLiveRefresh(refresh, { interval: 30000 })

  if (loading && !data) {
    return (
      <section>
        <h1><Terminal size={22} /> Runners</h1>
        <SkeletonTable rows={3} columns={5} />
      </section>
    )
  }

  const summary = data?.summary || {}
  const instances = data?.instances || []
  const enabled = !!data?.enabled

  return (
    <section>
      <h1><Terminal size={22} /> Runners</h1>
      <p className="page-intro">
        Execution backend for <strong>SSH instances</strong>. A persistent{' '}
        <code>helios-runner</code> runs Ansible over SSH against registered hosts, using the
        inventory from each application repository. Agent-mode hosts run Ansible locally and do not
        use a runner.
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
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{summary.instances || 0}</span><span className="stat-label">SSH instances</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{summary.online || 0}</span><span className="stat-label">Online</span></div>
      </div>

      <div className="runner-grid">
        <article className="card runner-card">
          <div className="card-head">
            <div>
              <h3><Terminal size={16} /> SSH runner</h3>
              <span className="sub">Long-running process next to the managed hosts</span>
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
                <label>Inventory</label>
                <div className="value">from the application repository</div>
              </div>
              <div className="detail">
                <label>SSH keys</label>
                <div className="value"><ShieldCheck size={13} /> managed and encrypted by the hub</div>
              </div>
            </div>
            <p className="sub runner-copy">
              The runner polls <code>/api/v1/runner/instances</code>, leases each SSH instance and
              starts one Ansible loop per host. It stays registered across checks and reconnects
              automatically.
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
                <strong>SSH · runner</strong>
                <p>A runner runs Ansible over SSH against the host, using the repository inventory and the instance address as <code>--limit</code>. Nothing is installed on the host.</p>
              </div>
              <span className="runner-mode-tag accent">runner</span>
            </div>
          </div>
        </article>
      </div>

      <h2 className="detail-section-heading">
        <Activity size={16} /> SSH instances
        <span className="muted detail-count">{instances.length}</span>
      </h2>

      {instances.length === 0 ? (
        <div className="empty">
          <p className="muted">
            No SSH instances. Runners are idle; every current host uses agent mode and runs Ansible locally.
          </p>
          <Link className="btn ghost small" to="/instances">Open instances</Link>
        </div>
      ) : (
        <>
          <div className="table-wrap runner-instances-table">
            <table className="grid">
              <thead>
                <tr>
                  <th>Instance</th>
                  <th style={{ width: '160px' }}>Project</th>
                  <th style={{ width: '130px' }}>State</th>
                  <th style={{ width: '220px' }}>Inventory host</th>
                  <th style={{ width: '200px' }}>Last seen</th>
                  <th style={{ width: '100px' }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {instances.map((inst) => (
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
                    <td><Badge state={inst.state} label={inst.state} small /></td>
                    <td className="mono">{inst.inventory_host || '—'}</td>
                    <td className="muted">{fmt(inst.last_seen)}</td>
                    <td><Link className="btn ghost small" to={`/instances/${inst.id}`}>Open</Link></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="table-note muted">
            {plural(summary.online || 0, 'instance')} online · {plural(summary.instances || 0, 'host')} managed over SSH
          </p>
        </>
      )}

      <details className="detail-panel">
        <summary><Settings2 size={15} /> Runner configuration</summary>
        <div className="detail-panel-body">
          <p className="sub">
            Hub and runner must share the same runner token. The runner also needs an SSH identity for
            the managed hosts.
          </p>
          <pre className="output">{`# Hub
HELIOS_RUNNER_TOKEN=...

# Runner
HELIOS_SERVER=https://hub.example.com
HELIOS_RUNNER_TOKEN=...
HELIOS_RUNNER_SSH_USER=ansible
HELIOS_RUNNER_SSH_KEY=/etc/helios/runner_id

helios-runner`}</pre>
          <p className="sub runner-config-note">
            <Clock size={13} /> Discovery and polling intervals default to 15s and 30s. On Kubernetes,
            refreshing the runner pod is optional: it is not required for agent-mode hosts.
          </p>
        </div>
      </details>
    </section>
  )
}
