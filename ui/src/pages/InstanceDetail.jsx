import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import StatusIcon from '../components/StatusIcon.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import ActionModal from '../components/ActionModal.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import Pager from '../components/Pager.jsx'
import { useAuth } from '../App.jsx'

const PAGE_SIZE = 25

export default function InstanceDetail() {
  const { id } = useParams()
  const { can } = useAuth()
  const [inst, setInst] = useState(null)
  const [runs, setRuns] = useState([])
  const [error, setError] = useState('')
  const [logRun, setLogRun] = useState(null)
  const [diffRun, setDiffRun] = useState(null)
  const [actionModal, setActionModal] = useState(null)
  const [varsApp, setVarsApp] = useState(null)
  const [runsOff, setRunsOff] = useState(0)
  const [runsTotal, setRunsTotal] = useState(0)

  const refresh = useCallback(async () => {
    try {
      setError('')
      setInst(await api.instance(id))
      const d = await api.runs(id, `?limit=${PAGE_SIZE}&offset=${runsOff}`)
      setRuns(d.items)
      setRunsTotal(d.total)
      if (!d.items.length && d.total > 0 && runsOff > 0) setRunsOff(0)
    } catch (e) {
      setError(e.message)
    }
  }, [id, runsOff])

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 10000)
    return () => clearInterval(t)
  }, [refresh])

  const toggleAutoHeal = async (app) => {
    try {
      setError('')
      await api.setAutoHeal(id, app.application.id, !app.autoheal)
      refresh()
    } catch (e) {
      setError(e.message)
    }
  }

  // rearm clears the stuck flag server-side and lets autoheal try again.
  const rearm = async (app) => {
    try {
      setError('')
      await api.setAutoHeal(id, app.application.id, true)
      refresh()
    } catch (e) {
      setError(e.message)
    }
  }

  if (!inst) return <div className="muted">Loading…</div>
  const online =
    inst.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000

  return (
    <section>
      <h1>
        {inst.name} <Badge state={inst.state} label={inst.state} />
        <span className={`dot ${online ? 'online' : 'offline'}`} />
      </h1>
      <div className="meta">
        <div>
          <b>ID</b> {inst.id}
        </div>
        <div>
          <b>Hostname</b> {inst.hostname || '—'}
        </div>
        <div>
          <b>Platform</b> {inst.platform || '—'}
        </div>
        <div>
          <b>Version</b> {inst.version || '—'}
        </div>
        <div>
          <b>Last seen</b> {inst.last_seen ? new Date(inst.last_seen).toLocaleString() : 'never'}
        </div>
      </div>

      {error && <div className="error">{error}</div>}

      <h2>Applications</h2>
      {inst.apps.length === 0 && <p className="muted">No applications assigned.</p>}
      {inst.apps.map((a) => (
        <div className="card" key={a.application.id}>
          <div className="card-head">
            <strong>{a.application.name}</strong>
            {a.stuck && <Badge state="stuck" label="stuck" small />}
            <StatusIcon state={a.latest_check?.status || 'unknown'} size={16} />
            <Badge state={a.latest_check?.status || 'unknown'} label={a.latest_check?.status || 'no check yet'} small />
            {a.stuck ? (
              <button
                className="small warn"
                title="re-enable autoheal once the playbook is fixed"
                disabled={!can('instances', 'update')}
                onClick={() => rearm(a)}
              >
                Rearm
              </button>
            ) : (
              <label className="switch" title="auto heal / re-apply on drift detection">
                <input
                  type="checkbox"
                  checked={a.autoheal}
                  disabled={!can('instances', 'update')}
                  onChange={() => toggleAutoHeal(a)}
                />
                <span className="slider" />
              </label>
            )}
            <span className="sub">{a.autoheal ? (a.autoheal_set ? 'auto-sync on (override)' : 'auto-sync on') : 'auto-sync off'}</span>
            <span className="mono muted">{a.application.repo_url}</span>
            <span className="actions">
              <button
                className="small"
                disabled={!can('instances', 'update')}
                onClick={() => setVarsApp(a.application)}
              >
                Host vars
              </button>{' '}
              <button
                className="small warn"
                disabled={!can('commands', 'create')}
                onClick={() => setActionModal({ instName: inst.name, action: 'apply' })}
              >
                Sync
              </button>{' '}
              <button
                className="small"
                disabled={!can('commands', 'create')}
                onClick={() => setActionModal({ instName: inst.name, action: 'check' })}
              >
                Check
              </button>
            </span>
          </div>

          {a.latest_check && (
            <div className="drift">
              <div>
                Commit <code>{a.latest_check.commit_after?.slice(0, 10)}</code>
                {a.latest_check.commit_msg ? <em className="sub"> — {a.latest_check.commit_msg}</em> : null} · checked{' '}
                {new Date(a.latest_check.finished_at).toLocaleTimeString()}
                {a.last_check_at && (
                  <>
                    {' '}
                    · last check {new Date(a.last_check_at).toLocaleTimeString()}
                  </>
                )}
              </div>
              {a.latest_check.changed_tasks?.length > 0 && (
                <div className="changed-list">
                  {a.latest_check.changed_tasks.map((t, i) => (
                    <div key={i} className="changed-item">
                      ⤷ {t.task}
                      {t.host ? <span className="muted"> ({t.host})</span> : null}
                    </div>
                  ))}
                </div>
              )}
              {a.latest_check.output_tail && (
                <details>
                  <summary>latest output</summary>
                  <pre className="output">{a.latest_check.output_tail}</pre>
                </details>
              )}
            </div>
          )}
        </div>
      ))}

      <h2>Run history</h2>
      <table className="grid">
        <thead>
          <tr>
            <th>#</th>
            <th>Mode</th>
            <th>Status</th>
            <th>Trigger</th>
            <th>Commit</th>
            <th>Summary</th>
            <th>When</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id}>
              <td className="mono">{r.id}</td>
              <td>{r.mode}</td>
              <td>
                <Badge state={r.status} label={r.status} small />
              </td>
              <td>{r.trigger}</td>
              <td className="commit-cell" title={r.commit_msg}>
                      <span className="mono">{r.commit_after?.slice(0, 9) || '—'}</span>
                      {r.commit_msg && <span className="sub">{r.commit_msg}</span>}
                    </td>
              <td className="mono">
                {Object.entries(r.summary || {})
                  .map(([k, v]) => `${k}:${v}`)
                  .join(' ')}
              </td>
              <td className="muted">{new Date(r.finished_at).toLocaleString()}</td>
              <td>
                {r.mode === 'check' && (
                  <>
                    <button
                      className="small"
                      title="view the pre-apply diff"
                      onClick={() => setDiffRun(r.id)}
                    >
                      Diff
                    </button>{' '}
                  </>
                )}
                <button className="small" onClick={() => setLogRun({ runId: r.id, view: 'text' })}>
                  Logs
                </button>
              </td>
            </tr>
          ))}
          {runs.length === 0 && (
            <tr>
              <td colSpan={8} className="muted">
                No runs yet.
              </td>
            </tr>
          )}
        </tbody>
      </table>

      {logRun && (
        <LogsModal runId={logRun.runId} initialView={logRun.view} onClose={() => setLogRun(null)} />
      )}
      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
      {actionModal && (
        <ActionModal
          instName={actionModal.instName}
          action={actionModal.action}
          onClose={() => {
            setActionModal(null)
            refresh()
          }}
        />
      )}

      {varsApp && (
        <VariableEditor
          title={`Host variables · ${inst.name}`}
          subtitle={`Overrides for ${varsApp.name} on this host only (≈ host_vars); win over the app defaults.`}
          load={() => api.assignmentVariables(id, varsApp.id)}
          save={(items) => api.saveAssignmentVariables(id, varsApp.id, items)}
          onClose={() => setVarsApp(null)}
        />
      )}

      <Pager total={runsTotal} limit={PAGE_SIZE} offset={runsOff} onPage={setRunsOff} />
    </section>
  )
}