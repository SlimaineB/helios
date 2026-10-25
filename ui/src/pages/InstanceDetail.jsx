import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import StatusIcon from '../components/StatusIcon.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import VariableEditor from '../components/VariableEditor.jsx'
import Pager from '../components/Pager.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import CopyButton from '../components/CopyButton.jsx'
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
  const [confirm, setConfirm] = useState(null)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      setInst(await api.instance(id))
      const d = await api.runs(id, `?limit=${PAGE_SIZE}&offset=${runsOff}`)
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
  const modalOpen = !!logRun || !!diffRun || !!varsApp || !!confirm
  useEffect(() => {
    refresh()
  }, [refresh])
  useEffect(() => {
    if (modalOpen) return
    const t = setInterval(() => { if (!document.hidden) refresh() }, 10000)
    return () => clearInterval(t)
  }, [refresh, modalOpen])

  const online = inst?.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000

  const toggleAutoHeal = async (app) => {
    try {
      await api.setAutoHeal(id, app.application.id, !app.autoheal)
      toast.success('Auto-heal updated')
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
      toast.success('Auto-heal re-armed')
      refresh()
    } catch (e) {
      toast.error(e.message)
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
        <span className={`dot ${online ? 'online' : 'offline'}`} />
      </h1>
      <div className="meta">
        <div><b>ID</b> <CopyButton value={String(inst.id)} label="" size="small" title="Copy ID" />{inst.id}</div>
        <div><b>Hostname</b> {inst.hostname || '—'}</div>
        <div><b>Platform</b> {inst.platform || '—'}</div>
        <div><b>Version</b> {inst.version || '—'}</div>
        <div><b>Last seen</b> {inst.last_seen ? new Date(inst.last_seen).toLocaleString() : 'never'}</div>
      </div>

      <h2>Applications</h2>
      {inst.apps.length === 0 && <SkeletonEmpty lines={2} />}
      {inst.apps.map((a) => {
        const pending = commands.pending[`${inst.id}:${a.application.id}`]
        return (
        <div className="card" key={a.application.id}>
          <div className="card-head">
            <strong>{a.application.name}</strong>
            {a.stuck && <Badge state="stuck" label="stuck" small />}
            {a.frozen && <Badge state="failed" label="sync frozen" small />}
            <StatusIcon state={a.latest_check?.status || 'unknown'} size={16} />
            <Badge state={a.latest_check?.status || 'unknown'} label={a.latest_check?.status || 'no check yet'} small />
            {a.stuck ? (
              <button className="small warn" title="re-enable autoheal once the playbook is fixed" disabled={!can('instances', 'update')} onClick={() => rearm(a)}>Rearm</button>
            ) : (
              <label className="switch" title="auto heal / re-apply on drift detection">
                <input type="checkbox" checked={a.autoheal} disabled={!can('instances', 'update')} onChange={() => toggleAutoHeal(a)} /><span className="slider" />
              </label>
            )}
            <span className="sub">{a.autoheal ? (a.autoheal_set ? 'auto-sync on (override)' : 'auto-sync on') : 'auto-sync off'}</span>
            {a.target_revision && (
              <>
                <Badge state="pinned" label={`pinned @${a.target_revision.slice(0, 9)}`} small />
                <button className="small" disabled={!can('instances', 'update')} onClick={() => trackBranch(a)} title="resume tracking the application branch">Track branch</button>
              </>
            )}
            <span className="mono muted"><CopyButton value={a.application.repo_url} label="" size="small" title="Copy repo URL" />{a.application.repo_url}</span>
            <span className="actions">
              <button className="small" disabled={!can('instances', 'update')} onClick={() => setVarsApp(a.application)}>Host vars</button>{' '}
              <button className="small warn" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'apply'} onClick={() => commands.run(inst.id, a.application.id, 'apply')}>{pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}Sync</button>{' '}
              <button className="small" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'check'} onClick={() => commands.run(inst.id, a.application.id, 'check')}>{pending?.action === 'check' && <span className="spinner xs" aria-hidden="true" />}Check</button>
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
            </div>
          )}
        </div>
        )
      })}

      <h2>Run history</h2>
      <table className="grid">
        <thead><tr><th>#</th><th>Mode</th><th>Status</th><th>Trigger</th><th>Commit</th><th>Summary</th><th>When</th><th></th></tr></thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id}>
              <td className="mono"><CopyButton value={String(r.id)} label="" size="small" title="Copy run ID" />{r.id}</td>
              <td>{r.mode}</td>
              <td><Badge state={r.status} label={r.status} small /></td>
              <td>{r.trigger}</td>
              <td className="commit-cell" title={r.commit_msg}><span className="mono"><CopyButton value={r.commit_after || ''} label="" size="small" title="Copy commit" />{r.commit_after?.slice(0, 9) || '—'}</span> {r.commit_msg && <span className="sub">{r.commit_msg}</span>}</td>
              <td className="mono">{Object.entries(r.summary || {}).map(([k, v]) => `${k}:${v}`).join(' ')}</td>
              <td className="muted">{new Date(r.finished_at).toLocaleString()}</td>
              <td>
                {r.mode === 'check' && <><button className="small" title="view the pre-apply diff" onClick={() => setDiffRun(r.id)}>Diff</button>{' '}</>}
                {r.commit_after && <><button className="small warn" disabled={!can('instances', 'update')} title="pin this assignment to this commit and re-apply" onClick={() => confirmRollback(r)}>Rollback</button>{' '}</>}
                <button className="small" onClick={() => setLogRun({ runId: r.id, view: 'text' })}>Logs</button>
              </td>
            </tr>
          ))}
          {runs.length === 0 && <tr><td colSpan={8} className="muted">No runs yet.</td></tr>}
        </tbody>
      </table>

      {logRun && <LogsModal runId={logRun.runId} initialView={logRun.view} onClose={() => setLogRun(null)} />}
      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
      {varsApp && <VariableEditor title={`Host variables · ${inst.name}`} subtitle={`Overrides for ${varsApp.name} on this host only (≈ host_vars); win over the app defaults.`} load={() => api.assignmentVariables(id, varsApp.id)} save={(items) => api.saveAssignmentVariables(id, varsApp.id, items)} onClose={() => setVarsApp(null)} />}
      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
      <Pager total={runsTotal} limit={PAGE_SIZE} offset={runsOff} onPage={setRunsOff} />
    </section>
  )
}