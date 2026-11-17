import { Link } from 'react-router-dom'
import { ClipboardCheck, Play, RefreshCw, Server } from 'lucide-react'
import Badge from './Badge.jsx'
import AutoHealChip, { hostAutoHealOn } from './AutoHealChip.jsx'
import QuickMenu from './QuickMenu.jsx'
import CopyButton from './CopyButton.jsx'
import TagList from './TagList.jsx'
import { stateLabel, instanceOnline } from '../api/client.js'
import { useAuth } from '../App.jsx'

const fmtTime = (t) => (t ? new Date(t).toLocaleTimeString() : '—')

export default function InstancesCard({ instance, onCheckAll, onApplyAll, onHostAutoHeal, selected, onSelect }) {
  const { can } = useAuth()
  const isOnline = instanceOnline(instance)
  const apps = instance.apps || []
  const counts = apps.reduce((acc, a) => {
    acc.total++
    if (a.stuck) acc.stuck++
    if (a.autoheal) acc.autoheal++
    const st = a.latest_check?.status
    if (st === 'ok') acc.synced++
    else if (st === 'drift') acc.drift++
    else if (st === 'failed' || st === 'error') acc.failed++
    else acc.unknown++
    return acc
  }, { total: 0, synced: 0, drift: 0, failed: 0, stuck: 0, unknown: 0, autoheal: 0 })
  const hostOn = hostAutoHealOn(instance)
  const hostState = !isOnline ? 'offline' : instance.state === 'ok' ? 'ok' : instance.state || 'unknown'
  const canRun = can('commands', 'create') && apps.length > 0

  const menu = [
    { label: 'Open details', icon: Server, to: `/instances/${instance.id}` },
    { separator: true },
    { label: 'Check all applications', icon: ClipboardCheck, onClick: () => onCheckAll?.(instance), disabled: !canRun },
    { label: 'Apply all applications', icon: Play, onClick: () => onApplyAll?.(instance), disabled: !canRun },
    { separator: true },
    {
      label: hostOn ? 'Disable host auto-heal' : 'Enable host auto-heal',
      icon: RefreshCw,
      onClick: () => onHostAutoHeal?.(instance, !hostOn),
      disabled: !can('instances', 'update'),
    },
  ]

  return (
    <article className={`instance-card${selected ? ' selected' : ''}`}>
      <header className="card-head">
        <input
          type="checkbox"
          className="card-select"
          checked={selected || false}
          onChange={() => onSelect?.(instance.id)}
          aria-label={`Select ${instance.name}`}
        />
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
            {instance.id} · {instance.project || 'global'}{instance.region ? ` · ${instance.region}` : ''}
            <TagList tags={instance.tags} />
          </span>
        </div>
        <div className="card-actions">
          <Badge state={hostState} label={stateLabel(hostState)} small />
          <QuickMenu label={`Actions for ${instance.name}`} items={menu} />
        </div>
      </header>

      <div className="card-body">
        <div className="kpi-row">
          <div className="kpi">
            <label>Server</label>
            <strong className={isOnline ? 'ok-text' : 'offline-text'}>{isOnline ? 'Online' : 'Offline'}</strong>
            <span>{stateLabel(hostState)}</span>
          </div>
          <div className="kpi">
            <label>Applications</label>
            <strong>{counts.total}</strong>
            <span>{counts.synced} synced{counts.unknown ? ` · ${counts.unknown} unknown` : ''}</span>
          </div>
          <div className="kpi">
            <label>Drift</label>
            <strong className={counts.drift ? 'drift-text' : ''}>{counts.drift}</strong>
            <span>{counts.failed + counts.stuck} failed</span>
          </div>
          <div className="kpi">
            <label>Auto-heal</label>
            <strong>{hostOn ? 'On' : 'Off'}</strong>
            <span>{counts.autoheal}/{counts.total} apps</span>
          </div>
        </div>
        <div className="instance-summary-line">
          <span className="muted">Last check {fmtTime(instance.last_check_at)}</span>
          {counts.drift > 0 && <span className="drift-text">{counts.drift} application{counts.drift === 1 ? '' : 's'} in drift</span>}
          {counts.total === 0 && <span className="muted">No application assigned</span>}
        </div>
      </div>

      <footer className="card-foot">
        <div className="actions">
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
          <Link className={`btn small${counts.drift ? ' warn' : ''}`} to={`/instances/${instance.id}`}>
            {counts.drift ? 'Review drift' : 'Open details'}
          </Link>
        </div>
      </footer>
    </article>
  )
}
