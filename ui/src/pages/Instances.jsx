import { useCallback, useEffect, useState } from 'react'
import { api, stateLabel } from '../api/client.js'
import { Link } from 'react-router-dom'
import Badge from '../components/Badge.jsx'
import StatusIcon from '../components/StatusIcon.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import ActionModal from '../components/ActionModal.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import Modal from '../components/Modal.jsx'
import Pager from '../components/Pager.jsx'
import { useAuth } from '../App.jsx'

const PAGE_SIZE = 20

const online = (inst) =>
  inst.last_seen && Date.now() - new Date(inst.last_seen).getTime() < 7 * 60 * 1000

const fmtTime = (t) => (t ? new Date(t).toLocaleTimeString() : '—')

export default function Instances() {
  const { can } = useAuth()
  const [instances, setInstances] = useState([])
  const [error, setError] = useState('')
  const [showReg, setShowReg] = useState(false)
  const [regName, setRegName] = useState('')
  const [regResult, setRegResult] = useState(null)
  const [regError, setRegError] = useState('')
  const [busy, setBusy] = useState({})
  const [runModal, setRunModal] = useState(null)
  const [diffModal, setDiffModal] = useState(null)
  const [actionModal, setActionModal] = useState(null)
  const [varsApp, setVarsApp] = useState(null)
  const [pageOff, setPageOff] = useState(0)
  const [total, setTotal] = useState(0)

  const refresh = useCallback(async () => {
    try {
      setError('')
      const d = await api.instances(`?limit=${PAGE_SIZE}&offset=${pageOff}`)
      setInstances(d.items)
      setTotal(d.total)
      if (!d.items.length && d.total > 0 && pageOff > 0) setPageOff(0)
    } catch (e) {
      setError(e.message)
    }
  }, [pageOff])

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 10000)
    return () => clearInterval(t)
  }, [refresh])

  const addInstance = async (e) => {
    e.preventDefault()
    setRegError('')
    try {
      const res = await api.createInstance(regName.trim())
      setRegResult(res)
      setRegName('')
      refresh()
    } catch (err) {
      setRegError(err.message)
    }
  }

  const closeReg = () => {
    setShowReg(false)
    setRegResult(null)
    setRegName('')
    setRegError('')
  }

  const act = async (key, fn, ...args) => {
    setBusy((b) => ({ ...b, [key]: true }))
    try {
      setError('')
      await fn(...args)
    } catch (e) {
      setError(e.message)
    } finally {
      setBusy((b) => ({ ...b, [key]: false }))
      refresh()
    }
  }

  const toggleAutoHeal = async (inst, app) => {
    await act(`${inst.id}:${app.application.id}:heal`, api.setAutoHeal, inst.id, app.application.id, !app.autoheal)
  }

  // rearm re-enables autoheal for a stuck assignment: the server clears the
  // stuck flag and gives the heal loop a fresh chance to converge.
  const rearm = async (inst, app) => {
    await act(`${inst.id}:${app.application.id}:heal`, api.setAutoHeal, inst.id, app.application.id, true)
  }

  const appKey = (inst, app, suffix) => `${inst.id}:${app.application.id}:${suffix}`

  const stats = {
    total: instances.length,
    ok: instances.filter((i) => i.state === 'ok').length,
    drift: instances.filter((i) => i.state === 'drift').length,
    offline: instances.filter((i) => !online(i)).length,
  }

  return (
    <section>
      <h1>Instances</h1>

      <div className="stats">
        <div className="stat-card">
          <span className="stat-dot total" />
          <span className="stat-num">{stats.total}</span>
          <span className="stat-label">Instances</span>
        </div>
        <div className="stat-card">
          <span className="stat-dot ok" />
          <span className="stat-num">{stats.ok}</span>
          <span className="stat-label">Synced</span>
        </div>
        <div className="stat-card">
          <span className="stat-dot drift" />
          <span className="stat-num">{stats.drift}</span>
          <span className="stat-label">In drift</span>
        </div>
        <div className="stat-card">
          <span className="stat-dot offline" />
          <span className="stat-num">{stats.offline}</span>
          <span className="stat-label">Offline</span>
        </div>
        <div className="stats-note">
          Poll every 5s · pull-based, agents reach out to the server
        </div>
      </div>

      <div className="row-toolbar">
        <span className={`muted ${total ? '' : 'hidden'}`}>
          {total} instance{total === 1 ? '' : 's'}
        </span>
        <button onClick={() => setShowReg(true)} disabled={!can('instances', 'create')}>
          ＋ Register instance
        </button>
      </div>

      {error && <div className="error">{error}</div>}

      {showReg && (
        <Modal title="Register a new agent" onClose={closeReg}>
          {regResult ? (
            <div className="form-card">
              <p>
                <strong>{regResult.instance.name}</strong> registered. The bootstrap token is only
                shown once — put it in the agent&apos;s config:
              </p>
              <pre className="token-pre">
                server: {`${location.origin || location.protocol + '//' + location.host}`}
                {'\n'}
                token: {regResult.token}
                {'\n'}
                instance_id: {regResult.instance.id}
              </pre>
              <div>
                <button onClick={closeReg}>Done</button>{' '}
                <button
                  className="ghost"
                  onClick={() => {
                    setRegResult(null)
                    setRegError('')
                  }}
                >
                  Register another
                </button>
              </div>
            </div>
          ) : (
            <form className="form-card" onSubmit={addInstance}>
              <label>
                Instance name
                <input
                  autoFocus
                  value={regName}
                  onChange={(e) => setRegName(e.target.value)}
                  placeholder="e.g. web-prod-01"
                />
              </label>
              {regError && <div className="error">{regError}</div>}
              <button type="submit" disabled={!regName.trim()}>
                Create
              </button>
            </form>
          )}
        </Modal>
      )}

      <div className="table-wrap">
        <table className="grid">
        <thead>
          <tr>
            <th>Instance</th>
            <th>App</th>
            <th>State</th>
            <th>Drifting components</th>
            <th>Commit</th>
            <th>Last check</th>
            <th>Auto-sync</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {instances.map((inst) => {
            const apps = inst.apps.length ? inst.apps : [null]
            return apps.map((app, idx) => (
              <tr key={`${inst.id}-${app?.application.id || 'none'}`}>
                {idx === 0 && (
                  <>
                    <td rowSpan={apps.length}>
                      <Link to={`/instances/${inst.id}`}>
                        <span className={`dot ${online(inst) ? 'online' : 'offline'}`} />
                        {inst.name}
                      </Link>
                      <div className="sub">{inst.id}</div>
                    </td>
                  </>
                )}
                {!app ? (
                  <>
                    <td className="muted">—</td>
                    <td>
                      <Badge state={inst.state} label={stateLabel(inst.state)} />
                    </td>
                    <td className="muted">—</td>
                    <td className="muted">—</td>
                    <td className="muted">—</td>
                    <td>
                      <span className="muted">not assigned</span>
                    </td>
                    <td className="muted">—</td>
                  </>
                ) : (
                  <>
                    <td>
                      {app.application.name}
                      {app.stuck && (
                        <Badge
                          state="stuck"
                          label="stuck"
                          small
                        />
                      )}
                    </td>
                    <td>
                      <span className="state-cell">
                        <StatusIcon state={app.latest_check?.status || 'unknown'} />
                        <Badge state={app.latest_check?.status || 'unknown'} label={app.latest_check?.status || 'no check'} small />
                      </span>
                    </td>
                    <td>
                      {app.latest_check?.changed_tasks?.length ? (
                        <div className="drift-list">
                          {app.latest_check.changed_tasks.map((t, i) => (
                            <div key={i} className="drift-chip" title={t.host ? `host: ${t.host}` : undefined}>
                              {t.task}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                    <td className="commit-cell" title={app.latest_check?.commit_msg}>
                      <span className="mono">{app.latest_check?.commit_after?.slice(0, 7) || '—'}</span>
                      {app.latest_check?.commit_msg && (
                        <span className="sub">{app.latest_check.commit_msg}</span>
                      )}
                    </td>
                    <td className="muted">{fmtTime(app.last_check_at || app.latest_check?.finished_at)}</td>
                    <td>
                      {app.stuck ? (
                        <span className="row small">
                          <button
                            className="small warn"
                            title="re-enable autoheal once the playbook is fixed"
                            disabled={!can('instances', 'update')}
                            onClick={() => rearm(inst, app)}
                          >
                            Rearm
                          </button>
                          <span className="muted">blocked · no convergence</span>
                        </span>
                      ) : (
                        <label className="switch" title="auto heal / re-apply on drift detection">
                          <input
                            type="checkbox"
                            checked={app.autoheal}
                            disabled={
                              busy[`${inst.id}:${app.application.id}:heal`] ||
                              !can('instances', 'update')
                            }
                            onChange={() => toggleAutoHeal(inst, app)}
                          />
                          <span className="slider" />
                        </label>
                      )}
                      <span className={`sub ${app.autoheal ? '' : 'muted'}`}>
                        {busy[`${inst.id}:${app.application.id}:heal`]
                          ? '…'
                          : app.autoheal
                            ? (app.autoheal_set ? 'on (override)' : 'on')
                            : 'off'}
                      </span>
                    </td>
                    <td>
                      <button
                        className="small"
                        title="host-specific variables for this app (≈ host_vars)"
                        disabled={!can('instances', 'update')}
                        onClick={() => setVarsApp({ inst, app })}
                      >
                        Vars
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
                      </button>{' '}
                      {app.latest_check?.id && (
                        <>
                          <button
                            className="small"
                            title="view what a run would change (pre-apply diff)"
                            onClick={() => setDiffModal(app.latest_check.id)}
                          >
                            Diff
                          </button>{' '}
                          <button
                            className="small"
                            title="full run log"
                            onClick={() => setRunModal({ runId: app.latest_check.id, view: 'text' })}
                          >
                            Logs
                          </button>
                        </>
                      )}
                    </td>
                  </>
                )}
              </tr>
            ))
          })}
          {instances.length === 0 && (
            <tr>
              <td colSpan={8} className="muted">
                No instances yet. Register one above.
              </td>
            </tr>
          )}
        </tbody>
      </table>
      </div>

      {runModal && (
        <LogsModal
          runId={runModal.runId}
          initialView={runModal.view}
          onClose={() => setRunModal(null)}
        />
      )}
      {diffModal && <DiffModal runId={diffModal} onClose={() => setDiffModal(null)} />}
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
          title={`Host variables · ${varsApp.inst.name}`}
          subtitle={`Overrides for ${varsApp.app.application.name} on this host only (≈ host_vars); win over the app defaults.`}
          load={() => api.assignmentVariables(varsApp.inst.id, varsApp.app.application.id)}
          save={(items) => api.saveAssignmentVariables(varsApp.inst.id, varsApp.app.application.id, items)}
          onClose={() => setVarsApp(null)}
        />
      )}

      <Pager total={total} limit={PAGE_SIZE} offset={pageOff} onPage={setPageOff} />
    </section>
  )
}