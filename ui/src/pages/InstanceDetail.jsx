import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import AutoHealChip, { appAutoHealMeta, hostAutoHealOn } from '../components/AutoHealChip.jsx'
import StatusIcon from '../components/StatusIcon.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import VariableEditor from '../components/VariableEditor.jsx'
import Pager from '../components/Pager.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
import TunnelDiagram from '../components/TunnelDiagram.jsx'
import { SkeletonCard, SkeletonTable, SkeletonEmpty } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const PAGE_SIZE = 25

export default function InstanceDetail() {
  const { id } = useParams()
  const { can } = useAuth()
  const [inst, setInst] = useState(null)
  const [runs, setRuns] = useState([])
  const [loading, setLoading] = useState(true)
  const loadedRef = useRef(false)
  const [logRun, setLogRun] = useState(null)
  const [diffRun, setDiffRun] = useState(null)
  const [varsApp, setVarsApp] = useState(null)
  const [runsOff, setRunsOff] = useState(0)
  const [runsTotal, setRunsTotal] = useState(0)
  const [appFilter, setAppFilter] = useState('')
  const [confirm, setConfirm] = useState(null)

  const filteredRuns = useMemo(
    () => runs.filter((r) =>
      !appFilter || (appNamesById.get(r.app_id) || '').toLowerCase().includes(appFilter.toLowerCase())
    ),
    [runs, appFilter, appNamesById]
  )

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      setInst(await api.instance(id))
      const d = await api.runs(id, `?limit=${PAGE_SIZE}&offset=${runsOff}${appFilter ? `&app_id=${appFilter}` : ''}`)
      setRuns(d.items)
      setRunsTotal(d.total)
      if (!d.items.length && d.total > 0 && runsOff > 0) setRunsOff(0)
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [id, runsOff])

  const commands = useCommandRunner(refresh)
  const appNamesById = useMemo(
    () => new Map((inst?.apps || []).map((a) => [a.application.id, a.application.name])),
    [inst]
  )
  const modalOpen = !!logRun || !!diffRun || !!varsApp || !!confirm
  useEffect(() => {
    refresh()
  }, [refresh])
  useEffect(() => {
    if (modalOpen) return
    const t = setInterval(() => { if (!document.hidden) refresh() }, 10000)
    return () => clearInterval(t)
  }, [refresh, modalOpen])

  useEffect(() => {
    setRunsOff(0)
  }, [appFilter])

  const online = inst?.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000
  const isTunnel = inst?.execution_mode === 'tunnel'
  const tunnelCommand = isTunnel && inst?.tunnel_gateway && inst?.tunnel_port
    ? [
        'ssh -N -T \\',
        '  -o ExitOnForwardFailure=yes \\',
        '  -o ServerAliveInterval=30 \\',
        '  -o ServerAliveCountMax=3 \\',
        `  -R 127.0.0.1:${inst.tunnel_port}:127.0.0.1:22 \\`,
        `  ${inst.tunnel_gateway}`,
      ].join('\n')
    : ''

  const toggleAutoHeal = async (app) => {
    try {
      await api.setAutoHeal(id, app.application.id, !app.autoheal)
      toast.success('Auto-apply updated')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const toggleHostAutoHeal = async (enabled) => {
    try {
      await api.setInstanceAutoHeal(id, enabled)
      toast.success(enabled ? 'Host auto-apply enabled' : 'Host auto-apply disabled')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const rollback = async (run) => {
    try {
      await api.setRevision(id, run.app_id, run.commit_after)
      toast.success('Rollback queued')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  const trackBranch = async (app) => {
    try {
      await api.setRevision(id, app.application.id, '')
      toast.success('Now tracking branch')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const rearm = async (app) => {
    try {
      await api.setAutoHeal(id, app.application.id, true)
      toast.success('Auto-apply resumed')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  const confirmUnassign = (app) => setConfirm({
    title: 'Unassign application',
    message: `Unassign "${app.application.name}" from "${inst.name}"? The application stays on other instances.`,
    onConfirm: () => unassign(app),
    variant: 'danger'
  })
  const unassign = async (app) => {
    try {
      await api.unassign(inst.id, app.application.id)
      toast.success('Application unassigned')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setConfirm(null)
    }
  }

  const confirmRollback = (run) => setConfirm({
    title: 'Rollback to commit',
    message: `Pin assignment "${run.app_id}" to commit ${run.commit_after?.slice(0, 9)} and re-apply?`,
    onConfirm: () => rollback(run),
    variant: 'warn'
  })

  if (!inst) {
    return (
      <section>
        <Link to="/instances" className="back-link">← Instances</Link>
        <h1>Instance Detail</h1>
        <SkeletonCard />
      </section>
    )
  }

  if (loading) {
    return (
      <section>
        <h1>{inst.name} <Badge state={inst.state} label={inst.state} /></h1>
        <div className="meta"><SkeletonTable rows={5} columns={1} /></div>
        <h2>Applications</h2>
        <SkeletonCard />
        <h2>Run history</h2>
        <SkeletonTable rows={5} columns={8} />
      </section>
    )
  }

  return (
    <section>
      <Link to="/instances" className="back-link">← Instances</Link>
      <h1>
        {inst.name} <Badge state={inst.state} label={inst.state} />
        {isTunnel && <Badge state="tunnel" label="tunnel" small />}
        <span className={`dot ${online ? 'online' : 'offline'}`} />
      </h1>
      <div className="meta">
        <div><b>ID</b> <CopyButton value={String(inst.id)} label="" size="small" title="Copy ID" />{inst.id}</div>
        <div><b>Hostname</b> {inst.hostname || '—'}</div>
        <div><b>Platform</b> {inst.platform || '—'}</div>
        <div><b>Version</b> {inst.version || '—'}</div>
        <div><b>Mode</b> {isTunnel ? 'tunnel · reverse SSH' : 'agent'}</div>
        {isTunnel && <div><b>Bastion</b> <span className="mono">{inst.tunnel_gateway || '—'}</span></div>}
        {isTunnel && <div><b>Tunnel port</b> <span className="mono">{inst.tunnel_port || '—'}</span></div>}
        {isTunnel && <div><b>Last tunnel</b> {inst.last_tunnel_at ? new Date(inst.last_tunnel_at).toLocaleString() : 'never'}</div>}
        <div><b>Last seen</b> {inst.last_seen ? new Date(inst.last_seen).toLocaleString() : 'never'}</div>
      </div>

      <div className="card host-autoheal-card">
        <div className="card-head">
          <strong>Host auto-apply</strong>
          <AutoHealChip
            enabled={hostAutoHealOn(inst)}
            scope="host"
            reason={hostAutoHealOn(inst)
              ? 'Every application on this host may apply drift automatically'
              : 'Drift is still detected but nothing is applied automatically on this host'}
          />
          <label className="switch" title={hostAutoHealOn(inst) ? 'Turn off auto-apply for every app on this host' : 'Turn on auto-apply for this host'}>
            <input
              type="checkbox"
              checked={hostAutoHealOn(inst)}
              disabled={!can('instances', 'update')}
              onChange={() => toggleHostAutoHeal(!hostAutoHealOn(inst))}
            />
            <span className="slider" />
          </label>
          <span className="sub muted">
            Master switch for this host. Per-application switches below still apply (most restrictive wins).
          </span>
        </div>
      </div>

      {isTunnel && (
        <div className="card">
          <div className="card-head">
            <strong>Reverse SSH tunnel</strong>
            <Badge state="tunnel" label="tunnel" small />
          </div>
          <p className="sub">
            A lightweight Helios tunnel agent can keep this reverse tunnel open and report health without Ansible. You can also run the SSH command manually; the runner on the bastion executes Ansible through the tunnel.
          </p>
          <TunnelDiagram port={inst.tunnel_port} gateway={inst.tunnel_gateway} />
          {tunnelCommand ? (
            <>
              <p><strong>Keep this tunnel running on the host</strong> <CopyButton value={tunnelCommand} title="Copy tunnel command" /></p>
              <pre className="token-pre">{tunnelCommand}</pre>
              <p className="sub">Use a dedicated SSH key restricted to port forwarding. The bastion runner connects to 127.0.0.1:{inst.tunnel_port}.</p>
            </>
          ) : (
            <p className="error">Tunnel gateway is not configured on the hub (HELIOS_TUNNEL_GATEWAY).</p>
          )}
        </div>
      )}

      <h2>Applications</h2>
      {inst.apps.length === 0 && <SkeletonEmpty lines={2} />}
      {inst.apps.map((a) => {
        const pending = commands.pending[`${inst.id}:${a.application.id}`]
        const ah = appAutoHealMeta(inst, a)
        return (
        <div className="card" key={a.application.id}>
          <div className="card-head">
            <strong>{a.application.name}</strong>
            {a.stuck && <Badge state="stuck" label="stuck" small />}
            {a.frozen && <Badge state="failed" label="auto-apply paused" small />}
            <StatusIcon state={a.latest_check?.status || 'unknown'} size={16} />
            <Badge state={a.latest_check?.status || 'unknown'} label={a.latest_check?.status || 'no check yet'} small />
            {a.stuck ? (
              <button className="small warn" title="Resume auto-apply once the playbook is fixed" disabled={!can('instances', 'update')} onClick={() => rearm(a)}>Resume auto-apply</button>
            ) : (
              <label className="switch" title={ah.locked ? ah.reason : 'Auto-apply on drift for this application on this host'}>
                <input type="checkbox" checked={a.autoheal} disabled={!can('instances', 'update') || ah.locked} onChange={() => toggleAutoHeal(a)} /><span className="slider" />
              </label>
            )}
            <span className="sub">{ah.label}</span>
            {a.target_revision && (
              <>
                <Badge state="pinned" label={`pinned @${a.target_revision.slice(0, 9)}`} small />
                <button className="small" disabled={!can('instances', 'update')} onClick={() => trackBranch(a)} title="resume tracking the application branch">Track branch</button>
              </>
            )}
            <span className="mono muted"><CopyButton value={a.application.repo_url} label="" size="small" title="Copy repo URL" />{a.application.repo_url}</span>
            <span className="actions">
              <button className="small" disabled={!can('instances', 'update')} onClick={() => setVarsApp(a.application)}>Host vars</button>{' '}
              <button className="small warn" title="Apply the desired configuration now" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'apply'} onClick={() => commands.run(inst.id, a.application.id, 'apply')}>{pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}Apply now</button>{' '}
              <button className="small" title="Check for drift without changing the host" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'check'} onClick={() => commands.run(inst.id, a.application.id, 'check')}>{pending?.action === 'check' && <span className="spinner xs" aria-hidden="true" />}Check now</button>{' '}
              <button className="small danger" disabled={!can('instances', 'update')} onClick={() => confirmUnassign(a)} title="Remove this application from this instance only">Unassign</button>
            </span>
          </div>

          {a.latest_check && (
            <div className="drift">
              <div>Commit <code><CopyButton value={a.latest_check.commit_after || ''} label="" size="small" title="Copy commit" />{a.latest_check.commit_after?.slice(0, 10)}</code> {a.latest_check.commit_msg ? <em className="sub"> — {a.latest_check.commit_msg}</em> : null} · checked {new Date(a.latest_check.finished_at).toLocaleTimeString()} {a.last_check_at && <> · last check {new Date(a.last_check_at).toLocaleTimeString()}</>}</div>
              {a.latest_check.changed_tasks?.length > 0 && (
                <div className="changed-list">
                  {a.latest_check.changed_tasks.map((t, i) => (
                    <div key={i} className="changed-item">⤷ {t.task} {t.host ? <span className="muted"> ({t.host})</span> : null}</div>
                  ))}
                </div>
              )}
              {a.latest_check.output_tail && <details><summary>latest output</summary><pre className="output">{a.latest_check.output_tail}</pre></details>}
              <div className="app-actions">
                {a.latest_check.status === 'drift' && (
                  <button
                    className="btn small warn"
                    title="Review the latest drift diff"
                    onClick={() => setDiffRun(a.latest_check.id)}
                  >
                    Review drift
                  </button>
                )}
                <button
                  className="btn ghost small"
                  title="View the full latest run log"
                  onClick={() => setLogRun({ runId: a.latest_check.id, view: 'text' })}
                >
                  View logs
                </button>
              </div>
            </div>
          )}
        </div>
        )
      })}

      {inst && inst.apps.length && (
        <div className="filters" style={{ marginBottom: '12px' }}>
          <label>Application</label>
          <input
            className="filter-search"
            value={appFilter}
            onChange={(e) => setAppFilter(e.target.value)}
            placeholder="Filter by application…"
            title="Filter runs by application name"
          />
        </div>
      )}
      <h2>Run history</h2>
      <table className="grid">
        <thead><tr><th>#</th><th>Mode</th><th>Status</th><th style={{ width: '140px' }}>Application</th><th>Trigger</th><th>Commit</th><th>Summary</th><th>When</th><th></th></tr></thead>
        <tbody>
          {filteredRuns.map((r) => (
            <tr key={r.id}>
              <td className="mono"><CopyButton value={String(r.id)} label="" size="small" title="Copy run ID" />{r.id}</td>
              <td>{r.mode === 'apply' ? 'Apply' : 'Check'}</td>
              <td><Badge state={r.status} label={r.status} small /></td>
              <td className="mono">{appNamesById.get(r.app_id) || `#${r.app_id}`}</td>
              <td>{r.trigger}</td>
              <td className="commit-cell" title={r.commit_msg}><span className="mono"><CopyButton value={r.commit_after || ''} label="" size="small" title="Copy commit" />{r.commit_after?.slice(0, 9) || '—'}</span> {r.commit_msg && <span className="sub">{r.commit_msg}</span>}</td>
              <td className="mono">{Object.entries(r.summary || {}).map(([k, v]) => `${k}:${v}`).join(' ')}</td>
              <td className="muted">{new Date(r.finished_at).toLocaleString()}</td>
              <td>
                {r.mode === 'check' && r.status === 'drift' && <><button className="small warn" title="Review this drift diff" onClick={() => setDiffRun(r.id)}>Review drift</button>{' '}</>}
                {r.commit_after && <><button className="small warn" disabled={!can('instances', 'update')} title="pin this assignment to this commit and re-apply" onClick={() => confirmRollback(r)}>Rollback</button>{' '}</>}
                <button className="small" title="View the full run log" onClick={() => setLogRun({ runId: r.id, view: 'text' })}>View logs</button>
              </td>
            </tr>
          ))}
          {runs.length === 0 && <tr><td colSpan={9} className="muted">No runs yet.</td></tr>}
        </tbody>
      </table>

      {logRun && <LogsModal runId={logRun.runId} initialView={logRun.view} onClose={() => setLogRun(null)} />}
      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
      {varsApp && <VariableEditor title={`Host variables · ${inst.name}`} subtitle={`Overrides for ${varsApp.name} on this host only (≈ host_vars); win over the app defaults.`} load={() => api.assignmentVariables(id, varsApp.id)} save={(items) => api.saveAssignmentVariables(id, varsApp.id, items)} onClose={() => setVarsApp(null)} />}
      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
      <Pager total={appFilter ? filteredRuns.length : runsTotal} limit={PAGE_SIZE} offset={runsOff} onPage={setRunsOff} />
    </section>
  )
}