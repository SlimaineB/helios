import CopyButton from './CopyButton.jsx'
import Badge from './Badge.jsx'
import AutoHealChip, { appAutoHealMeta, hostAutoHealOn } from './AutoHealChip.jsx'
import { Link } from 'react-router-dom'
import { api, stateLabel } from '../api/client.js'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const online = (inst) =>
  inst.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000

const fmtTime = (t) => (t ? new Date(t).toLocaleTimeString() : '—')

export default function InstancesCard({ instance, onAction, onHostAutoHeal, onAppAutoHeal, onDiff, onLogs, onVars }) {
  const { can } = useAuth()
  const isOnline = online(instance)
  const apps = instance.apps.length ? instance.apps : [null]
  const hostOn = hostAutoHealOn(instance)
  const hostControl = (
    <span className="host-actions">
      <AutoHealChip
        enabled={hostOn}
        scope="host"
        reason={hostOn ? 'Host auto-apply enabled' : 'No application on this host is applied automatically'}
      />
      <label className="switch" title={hostOn ? 'Turn off auto-apply for every app on this host' : 'Turn on auto-apply for this host'}>
        <input
          type="checkbox"
          checked={hostOn}
          disabled={!can('instances', 'update')}
          onChange={() => onHostAutoHeal?.(instance, !hostOn)}
        />
        <span className="slider" />
      </label>
    </span>
  )

  if (apps.length === 1 && !apps[0]) {
    return (
      <article className="instance-card">
        <header className="card-head">
          <div className="instance-main">
            <span className={`status-dot ${isOnline ? 'online' : 'offline'}`} aria-hidden="true" />
            <div className="instance-title">
              <Link to={`/instances/${instance.id}`} className="instance-link"><strong>{instance.name}</strong></Link>
            </div>
            <span className="instance-meta">
              <Badge
                state={instance.execution_mode === 'tunnel' ? 'tunnel' : 'agent'}
                label={instance.execution_mode === 'tunnel' ? 'bastion' : 'agent'}
                small
              />
              <CopyButton value={String(instance.id)} size="small" title="Copy instance ID" />
              {instance.id} · {instance.project || 'global'}
            </span>
          </div>
          <div className="card-actions">
            {hostControl}
            <span className="muted">Not assigned</span>
          </div>
        </header>
      </article>
    )
  }

  return (
    <article className="instance-card">
      <header className="card-head">
        <div className="instance-main">
          <span className={`status-dot ${isOnline ? 'online' : 'offline'}`} aria-hidden="true" />
          <div className="instance-title">
            <Link to={`/instances/${instance.id}`} className="instance-link"><strong>{instance.name}</strong></Link>
          </div>
          <span className="instance-meta">
            <Badge
              state={instance.execution_mode === 'tunnel' ? 'tunnel' : 'agent'}
              label={instance.execution_mode === 'tunnel' ? 'bastion' : 'agent'}
              small
            />
            <CopyButton value={String(instance.id)} size="small" title="Copy instance ID" />
            {instance.id} · {instance.project || 'global'}
          </span>
        </div>
        <div className="card-actions">
          {hostControl}
          <span className={`state-badge ${instance.state}`}>{stateLabel(instance.state)}</span>
        </div>
      </header>

      <div className="card-body">
        {apps.map((app, idx) => {
          const drifting = app?.latest_check?.status === 'drift'
          const ah = app ? appAutoHealMeta(instance, app) : null
          return (
          <div key={`${instance.id}-${app?.application.id || 'none'}`} className="app-row">
            {idx === 0 && (
              <div className="instance-info">
                <div className="detail">
                  <label>Last check</label>
                  <span>{fmtTime(instance.last_check_at)}</span>
                </div>
                <div className="detail">
                  <label>Apps assigned</label>
                  <span>{apps.filter(Boolean).length}</span>
                </div>
              </div>
            )}

            <div className="app-info">
              {!app ? (
                <div className="app-empty muted">— no application assigned —</div>
              ) : (
                <>
                  <div className="app-header">
                    <div className="app-main">
                      <Link className="entity-link app-name" to={`/?app=${app.application.id}`} title="Open application">
                        {app.application.name}
                      </Link>
                      {app.stuck && <Badge state="stuck" label="stuck" small />}
                    </div>
                    <div className="app-state">
                      <Badge state={app.latest_check?.status || 'unknown'} label={app.latest_check?.status || 'no check'} small />
                    </div>
                  </div>

                  {app.latest_check?.changed_tasks?.length && (
                    <div className="drift-tasks">
                      {app.latest_check.changed_tasks.map((t, i) => (
                        <span key={i} className="drift-chip" title={t.host ? `host: ${t.host}` : undefined}>{t.task}</span>
                      ))}
                    </div>
                  )}

                  <div className="app-meta">
                    <div className="meta-item">
                      <CopyButton value={app.latest_check?.commit_after || ''} size="small" title="Copy commit" />
                      <span className="mono">{app.latest_check?.commit_after?.slice(0, 7) || '—'}</span>
                      {app.latest_check?.commit_msg && <span className="commit-msg">{app.latest_check.commit_msg}</span>}
                    </div>
                    <div className="meta-item muted">Last check: {fmtTime(app.last_check_at || app.latest_check?.finished_at)}</div>
                  </div>

                  <div className="app-actions">
                    {app.stuck ? (
                      <button
                        className="btn warn small"
                        disabled={!can('instances', 'update') || app.autoheal}
                        onClick={() => onAction(instance.id, app.application.id, 'rearm')}
                        title="Resume auto-apply once the playbook is fixed"
                      >
                        Resume auto-apply
                      </button>
                    ) : (
                      <>
                        {ah.locked ? (
                          <button
                            type="button"
                            className="btn small"
                            disabled={ah.hostLocked ? !can('instances', 'update') : !can('applications', 'update')}
                            title={ah.reason}
                            onClick={() => (ah.hostLocked
                              ? onHostAutoHeal?.(instance, true)
                              : onAppAutoHeal?.(app.application.id))}
                          >
                            {ah.hostLocked ? 'Enable host' : 'Enable app'}
                          </button>
                        ) : (
                          <label className="switch" title="Auto-apply on drift for this application on this host">
                            <input
                              type="checkbox"
                              checked={app.autoheal}
                              disabled={!can('instances', 'update')}
                              onChange={() => onAction(instance.id, app.application.id, 'toggle-autoheal')}
                            />
                            <span className="slider" />
                          </label>
                        )}
                        <span className={`sub ${app.autoheal ? '' : 'muted'}`}>{ah.label}</span>
                      </>
                    )}
                    <button
                      className="btn small"
                      disabled={!can('commands', 'create')}
                      title="Apply the desired configuration now"
                      onClick={() => onAction(instance.id, app.application.id, 'apply')}
                    >
                      Apply now
                    </button>
                    <button
                      className="btn ghost small"
                      disabled={!can('commands', 'create')}
                      title="Check for drift without changing the host"
                      onClick={() => onAction(instance.id, app.application.id, 'check')}
                    >
                      Check now
                    </button>
                    {app.latest_check?.id && (
                      <>
                        {drifting && (
                          <button
                            className="btn small warn"
                            title="Review the latest drift diff"
                            onClick={() => onDiff(app.latest_check.id)}
                          >
                            Review drift
                          </button>
                        )}
                        <button className="btn ghost small" title="View the full latest run log" onClick={() => onLogs(app.latest_check.id)}>View logs</button>
                      </>
                    )}
                    <button className="btn ghost small" disabled={!can('instances', 'update')} onClick={() => onVars({ inst: instance, app })}>Vars</button>
                  </div>
                </>
              )}
            </div>
          </div>
          )
        })}
      </div>
    </article>
  )
}