import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  ArrowLeft,
  ClipboardCheck,
  ExternalLink,
  GitBranch,
  GitCommit,
  History,
  Network,
  Pencil,
  Play,
  Server,
  Settings2,
  Trash2,
  Variable,
} from 'lucide-react'
import Badge from './Badge.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from './GitOpsBadge.jsx'
import AutoHealChip from './AutoHealChip.jsx'
import HealthBadge from './HealthBadge.jsx'
import CopyButton from './CopyButton.jsx'
import QuickMenu from './QuickMenu.jsx'
import { api, projectLabel } from '../api/client.js'
import { toast } from '../components/Toast.jsx'

const shortRepo = (url = '') =>
  url.replace(/^https?:\/\//, '').replace(/^ssh:\/\//, '').replace(/\.git$/, '')

const appState = (assignment) => {
  if (assignment?.stuck) return 'stuck'
  const s = assignment?.latest_check?.status
  if (s === 'failed') return 'error'
  return s === 'ok' || s === 'drift' ? s : 'unknown'
}

const STATE_LABEL = {
  ok: 'Synced',
  drift: 'Drift',
  error: 'Failed',
  stuck: 'Stuck',
  unknown: 'No check',
}

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')

const varsToText = (vars = {}) => Object.entries(vars || {})
  .map(([key, value]) => {
    const text = typeof value === 'string' ? value : JSON.stringify(value)
    return `${key}=${/\s/.test(text) ? JSON.stringify(text) : text}`
  })
  .join(' ')

const textToVars = (text = '') => {
  const out = {}
  const re = /([^\s=]+)=("[^"]*"|'[^']*'|\S+)/g
  let match
  while ((match = re.exec(text))) {
    let value = match[2]
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1)
    }
    out[match[1]] = value
  }
  return out
}

const toInventoryDrafts = (groups = []) => (groups || []).map((group) => ({
  name: group.name || '',
  varsText: varsToText(group.vars),
  childrenText: (group.children || []).join(', '),
  hosts: (group.hosts || []).map((host) => ({
    name: host.name || '',
    varsText: varsToText(host.vars),
  })),
}))

const toInventoryGroups = (drafts = []) => (drafts || []).map((group) => ({
  name: group.name.trim(),
  vars: textToVars(group.varsText),
  children: group.childrenText.split(',').map((c) => c.trim()).filter(Boolean),
  hosts: (group.hosts || []).map((host) => ({
    name: host.name.trim(),
    vars: textToVars(host.varsText),
  })),
}))

export default function ApplicationDetail({
  app,
  stats,
  repo,
  instances,
  instancesTotal = 0,
  availableInstances,
  availableTotal = 0,
  can,
  onEdit,
  onVars,
  onDelete,
  onHistory,
  onUnassign,
  onAssign,
}) {
  const [assignTo, setAssignTo] = useState('')
  const [assigning, setAssigning] = useState(false)
  const [toggling, setToggling] = useState(false)
  const [autoheal, setAutoheal] = useState(app.autoheal)
  const [inventory, setInventory] = useState(null)
  const [inventoryError, setInventoryError] = useState('')
  const [inventoryLoading, setInventoryLoading] = useState(false)
  const [draftGroups, setDraftGroups] = useState([])
  const [newGroup, setNewGroup] = useState('')
  const [savingInventory, setSavingInventory] = useState(false)
  const readOnly = managedByGitOps(app)

  useEffect(() => { setAutoheal(app.autoheal) }, [app.id, app.autoheal])

  useEffect(() => {
    let cancelled = false
    setInventory(null)
    setInventoryError('')
    if (!app.inventory_path && app.inventory_source !== 'hub') {
      setInventoryLoading(false)
      return () => { cancelled = true }
    }
    setInventoryLoading(true)
    api.applicationInventory(app.id)
      .then((data) => { if (!cancelled) setInventory(data) })
      .catch((e) => { if (!cancelled) setInventoryError(e.message || 'Failed to load inventory') })
      .finally(() => { if (!cancelled) setInventoryLoading(false) })
    return () => { cancelled = true }
  }, [app.id, app.inventory_path, app.inventory_source])

  useEffect(() => {
    setDraftGroups(toInventoryDrafts(inventory?.groups))
  }, [inventory])

  const toggleAppAutoHeal = async () => {
    if (readOnly || !can('applications', 'update') || toggling) return
    setToggling(true)
    try {
      await api.setAppAutoHeal(app.id, !autoheal)
      setAutoheal(!autoheal)
    } catch (e) {
      toast.error(e.message || 'Failed to update auto-apply')
    } finally {
      setToggling(false)
    }
  }
  const updateGroup = (index, patch) => {
    setDraftGroups((groups) => groups.map((group, i) => (i === index ? { ...group, ...patch } : group)))
  }
  const removeGroup = (index) => {
    setDraftGroups((groups) => groups.filter((_, i) => i !== index))
  }
  const addGroup = () => {
    const name = newGroup.trim()
    if (!name) return
    if (draftGroups.some((group) => group.name === name)) {
      toast.error('Group already exists')
      return
    }
    setDraftGroups((groups) => [...groups, { name, varsText: '', childrenText: '', hosts: [] }])
    setNewGroup('')
  }
  const updateHost = (groupIndex, hostIndex, patch) => {
    setDraftGroups((groups) => groups.map((group, gi) => (
      gi === groupIndex
        ? { ...group, hosts: group.hosts.map((host, hi) => (hi === hostIndex ? { ...host, ...patch } : host)) }
        : group
    )))
  }
  const addHost = (groupIndex) => {
    setDraftGroups((groups) => groups.map((group, gi) => (
      gi === groupIndex ? { ...group, hosts: [...group.hosts, { name: '', varsText: '' }] } : group
    )))
  }
  const removeHost = (groupIndex, hostIndex) => {
    setDraftGroups((groups) => groups.map((group, gi) => (
      gi === groupIndex ? { ...group, hosts: group.hosts.filter((_, hi) => hi !== hostIndex) } : group
    )))
  }
  const saveInventory = async () => {
    if (savingInventory) return
    setSavingInventory(true)
    try {
      const data = await api.saveApplicationInventory(app.id, toInventoryGroups(draftGroups))
      setInventory(data)
      toast.success('Inventory saved')
    } catch (e) {
      toast.error(e.message || 'Failed to save inventory')
    } finally {
      setSavingInventory(false)
    }
  }

  const s = stats || { instances: 0, drift: 0, error: 0, stuck: 0 }
  const isRunner = app.execution_mode === 'runner'
  const unhealthy = s.error + s.stuck
  const health = unhealthy > 0
    ? 'failed'
    : s.instances === 0
      ? 'unknown'
      : s.drift > 0
        ? 'drift'
        : 'ok'
  const healthLabel = isRunner
    ? 'Runner mode'
    : unhealthy > 0
      ? `${unhealthy} failed`
      : s.instances === 0
        ? 'Unassigned'
        : s.drift > 0
          ? `${s.drift} in drift`
          : 'Synced'

  const repoLabel = repo?.name && repo.name !== app.repo_url
    ? repo.name
    : shortRepo(app.repo_url)

  const assigned = (instances || [])
    .map((inst) => ({
      inst,
      assignment: (inst.apps || []).find(
        (a) => String(a.application?.id) === String(app.id),
      ),
    }))
    .filter(({ assignment }) => !!assignment)

  const available = availableInstances || []
  const inventoryHostCount = (inventory?.groups || [])
    .reduce((total, group) => total + (group.hosts?.length || 0), 0)

  const runJob = async (mode) => {
    try {
      await api.runApplication(app.id, mode)
      toast.success(mode === 'apply' ? 'Apply queued on the runner fleet' : 'Check queued on the runner fleet')
    } catch (e) {
      toast.error(e.message)
    }
  }

  const assign = async () => {
    const inst = available.find((i) => i.id === assignTo)
    if (!inst || !onAssign) return
    setAssigning(true)
    try {
      await onAssign(inst)
      setAssignTo('')
    } finally {
      setAssigning(false)
    }
  }

  return (
    <section className="app-detail-page">
      <Link to="/applications" className="back-link">
        <ArrowLeft size={14} /> Back to applications
      </Link>

      <div className="detail-page-head">
        <div className="detail-page-title">
          <h1>{app.name}</h1>
          <div className="detail-chips">
            <HealthBadge state={health} label={healthLabel} size={16} />
            <span className="project-badge">{projectLabel(app.project)}</span>
            <GitOpsBadge obj={app} />
            {isRunner
              ? <span className="runner-mode-tag accent">runner</span>
              : <span className="detail-chip"><Server size={13} /> {s.instances} instance{s.instances === 1 ? '' : 's'}</span>}
            <span className="detail-chip"><GitBranch size={13} /> {app.branch || '—'}</span>
            {s.revision && (
              <span className="detail-chip"><GitCommit size={13} /> {s.revision.slice(0, 7)}</span>
            )}
          </div>
        </div>
        <div className="detail-head-actions">
          {isRunner && can('commands', 'create') && (
            <>
              <button type="button" className="btn" onClick={() => runJob('apply')}>
                <Play size={15} /> Apply
              </button>
              <button type="button" className="btn ghost" onClick={() => runJob('check')}>
                <ClipboardCheck size={15} /> Check
              </button>
            </>
          )}
          <Link className="btn ghost" to={isRunner ? '/runners' : `/instances?app=${app.id}`}>
            <Server size={15} /> Open fleet
          </Link>
          <button type="button" className="btn ghost" disabled={readOnly || !can('applications', 'update')} title={managedTitle(app)} onClick={() => onEdit(app)}>
            <Pencil size={15} /> Edit
          </button>
          <button type="button" className="btn ghost" disabled={readOnly || !can('applications', 'update')} title={managedTitle(app)} onClick={() => onVars(app)}>
            <Variable size={15} /> Variables
          </button>
          <button type="button" className="btn ghost" onClick={() => onHistory(app)}>
            <History size={15} /> History
          </button>
          <button type="button" className="btn danger" disabled={readOnly || !can('applications', 'delete')} title={managedTitle(app)} onClick={() => onDelete(app)}>
            <Trash2 size={15} /> Delete
          </button>
        </div>
      </div>

      {isRunner && (
        <div className="settings-note">
          <Server size={15} />
          <span>
            Runner mode: jobs are dispatched to the tagged bastion fleet. Targets are only in the
            inventory; no agent is installed on them.
            {(app.runner_tags || []).length ? ` Tags: ${app.runner_tags.join(', ')}.` : ' Any runner may claim the job.'}
          </span>
        </div>
      )}

      {!isRunner && (
      <div className="host-setting-bar">
        <div className="host-setting-copy">
          <strong>Auto-apply on drift</strong>
          <span className="sub muted">Applies to every host unless the host disables auto-apply or sets a per-host override. Most restrictive wins.</span>
        </div>
        <AutoHealChip
          enabled={autoheal}
          scope="app"
          reason={autoheal
            ? 'Auto-apply is enabled for this application on every host (unless a host disables it)'
            : 'Auto-apply is disabled for this application on every host'}
        />
        <label className="switch" title={readOnly ? managedTitle(app) : `Auto-apply on drift is ${autoheal ? 'enabled' : 'disabled'} for every host`}>
          <input
            type="checkbox"
            checked={autoheal}
            disabled={readOnly || !can('applications', 'update') || toggling}
            onChange={() => toggleAppAutoHeal()}
          />
          <span className="slider" />
        </label>
      </div>
      )}

      {!isRunner && (
      <div className="stats">
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{s.instances}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{s.synced}</span><span className="stat-label">Synced</span></div>
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{s.drift}</span><span className="stat-label">Drift</span></div>
        <div className="stat-card"><span className="stat-dot error" /><span className="stat-num">{unhealthy}</span><span className="stat-label">Failed</span></div>
      </div>
      )}

      <details className="detail-panel" open>
        <summary><Settings2 size={15} /> Configuration</summary>
        <div className="detail-panel-body">
          <div className="host-detail-grid app-config-grid">
            <div className="detail detail-wide">
              <label>Repository</label>
              <div className="value repo-line">
                <CopyButton value={app.repo_url} size="small" title="Copy repository URL" />
                <span className="repo-label">{repoLabel}</span>
              </div>
              <span className="cell-sub mono">{app.repo_url}</span>
            </div>
            <div className="detail">
              <label>Playbook path</label>
              <div className="value mono">{app.path || '—'}</div>
            </div>
            <div className="detail">
              <label>Execution</label>
              <div className="value">{isRunner ? 'runner fleet' : 'agent on each host'}</div>
            </div>
            <div className="detail">
              <label>Runner tags</label>
              <div className="value mono">{isRunner ? ((app.runner_tags || []).join(', ') || 'any') : '—'}</div>
            </div>
            <div className="detail">
              <label>Inventory</label>
              <div className="value mono">{app.inventory_source === 'hub' ? 'Helios-managed' : (app.inventory_path || '—')}</div>
            </div>
            <div className="detail">
              <label>Branch</label>
              <div className="value mono">{app.branch || '—'}</div>
            </div>
            {!isRunner && (
            <div className="detail">
              <label>Check interval</label>
              <div className="value">{app.sync_interval ? `${app.sync_interval}s` : 'inherit (0)'}</div>
            </div>
            )}
            <div className="detail">
              <label>Latest revision</label>
              <div className="value">
                <span className="mono"><GitCommit size={12} /> {s.revision ? s.revision.slice(0, 7) : '—'}</span>
                {s.revisionMsg && <span className="cell-sub">{s.revisionMsg}</span>}
              </div>
            </div>
            <div className="detail">
              <label>Last activity</label>
              <div className="value">{fmt(s.last_at)}</div>
            </div>
            <div className="detail">
              <label>Application ID</label>
              <div className="value mono">
                <CopyButton value={String(app.id)} label="" size="small" title="Copy application ID" />{app.id}
              </div>
            </div>
          </div>

          {app.ignore_paths && (
            <div className="detail-subsection">
              <label>Ignore drift paths</label>
              <pre className="output">{app.ignore_paths}</pre>
            </div>
          )}
        </div>
      </details>

      {(app.inventory_path || app.inventory_source === 'hub') && (
      <details className="detail-panel">
        <summary>
          <Network size={15} /> Inventory
          {inventoryHostCount > 0 && <span className="muted detail-count">{inventoryHostCount} hosts</span>}
        </summary>
        <div className="detail-panel-body">
          {inventoryLoading && <p className="muted">Loading inventory…</p>}
          {inventoryError && <div className="error">{inventoryError}</div>}
          {inventory && (
            <>
              <div className="inventory-meta">
                <span className="mono">
                  {inventory.inventory_source === 'hub' ? 'Helios-managed' : (inventory.source || 'no inventory_path set')}
                </span>
                {inventory.commit && <span className="muted mono">{inventory.commit.slice(0, 7)}</span>}
              </div>
              {(inventory.warnings || []).map((warning) => (
                <div className="inventory-warning" key={warning}>
                  <AlertTriangle size={14} /> <span>{warning}</span>
                </div>
              ))}
              {inventory.inventory_source === 'hub' && inventory.editable ? (
                <div className="inventory-editor">
                  {draftGroups.length === 0 && <p className="muted">No groups yet. Add one to start.</p>}
                  {draftGroups.map((group, gi) => (
                    <div className="inventory-group inventory-group-edit" key={gi}>
                      <div className="inventory-group-head">
                        <input value={group.name} onChange={(e) => updateGroup(gi, { name: e.target.value })} placeholder="group name" aria-label="Group name" />
                        <button type="button" className="btn ghost small" onClick={() => removeGroup(gi)}>Remove group</button>
                      </div>
                      <div className="two">
                        <label>
                          Children (comma separated)
                          <input value={group.childrenText} onChange={(e) => updateGroup(gi, { childrenText: e.target.value })} placeholder="base, dbservers" />
                        </label>
                        <label>
                          Group vars (key=value)
                          <input value={group.varsText} onChange={(e) => updateGroup(gi, { varsText: e.target.value })} placeholder="ansible_user=deploy" />
                        </label>
                      </div>
                      <div className="table-wrap">
                        <table className="grid">
                          <thead>
                            <tr>
                              <th>Host</th>
                              <th>Vars (key=value)</th>
                              <th style={{ width: '40px' }} />
                            </tr>
                          </thead>
                          <tbody>
                            {group.hosts.map((host, hi) => (
                              <tr key={hi}>
                                <td><input value={host.name} onChange={(e) => updateHost(gi, hi, { name: e.target.value })} placeholder="web-1" aria-label="Host name" /></td>
                                <td><input value={host.varsText} onChange={(e) => updateHost(gi, hi, { varsText: e.target.value })} placeholder="ansible_host=10.0.0.11 ansible_user=deploy" aria-label="Host vars" /></td>
                                <td>
                                  <button type="button" className="btn ghost small" title="Remove host" onClick={() => removeHost(gi, hi)}>
                                    <Trash2 size={13} />
                                  </button>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                      <button type="button" className="btn ghost small" onClick={() => addHost(gi)}>Add host</button>
                    </div>
                  ))}
                  <div className="inventory-add-group">
                    <input value={newGroup} onChange={(e) => setNewGroup(e.target.value)} placeholder="new group name" />
                    <button type="button" className="btn ghost" onClick={addGroup}>Add group</button>
                  </div>
                  <div className="inventory-editor-actions">
                    <button type="button" className="btn" onClick={saveInventory} disabled={savingInventory}>
                      {savingInventory ? 'Saving…' : 'Save inventory'}
                    </button>
                  </div>
                </div>
              ) : (
                (inventory.groups || []).length === 0 ? (
                  !inventoryLoading && !(inventory.warnings || []).length && (
                    <p className="muted">No hosts parsed from this inventory.</p>
                  )
                ) : (
                  inventory.groups.map((group) => (
                    <div className="inventory-group" key={group.name}>
                      <h4>
                        {group.name}
                        <span className="muted">{group.hosts?.length || 0} hosts</span>
                      </h4>
                      {(group.hosts || []).length > 0 && (
                        <div className="table-wrap">
                          <table className="grid">
                            <thead>
                              <tr>
                                <th>Host</th>
                                <th>Connection</th>
                              </tr>
                            </thead>
                            <tbody>
                              {group.hosts.map((host) => {
                                const vars = host.vars || {}
                                const connection = [
                                  vars.ansible_host,
                                  vars.ansible_user,
                                  vars.ansible_port ? `:${vars.ansible_port}` : '',
                                ].filter(Boolean).join(' · ')
                                return (
                                  <tr key={host.name}>
                                    <td><span className="mono">{host.name}</span></td>
                                    <td className="muted mono">{connection || '—'}</td>
                                  </tr>
                                )
                              })}
                            </tbody>
                          </table>
                        </div>
                      )}
                      {(group.children || []).length > 0 && (
                        <p className="sub muted">Children: {group.children.join(', ')}</p>
                      )}
                    </div>
                  ))
                )
              )}
            </>
          )}
        </div>
      </details>
      )}

      {!isRunner && (
      <>
      <h2 className="detail-section-heading">
        <Server size={16} /> Assigned instances
        <span className="muted detail-count">{assigned.length}</span>
      </h2>

      {onAssign && available.length > 0 && (
        <div className="filters">
          <label htmlFor="assign-instance">Assign instance</label>
          <select
            id="assign-instance"
            value={assignTo}
            disabled={readOnly}
            title={managedTitle(app)}
            onChange={(e) => setAssignTo(e.target.value)}
          >
            <option value="">Choose an instance…</option>
            {available.map((inst) => (
              <option key={inst.id} value={inst.id}>
                {inst.name} · {projectLabel(inst.project)}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="btn"
            disabled={!assignTo || assigning || readOnly || !can('instances', 'update')}
            title={managedTitle(app)}
            onClick={assign}
          >
            {assigning ? 'Assigning…' : 'Assign'}
          </button>
          {availableTotal > available.length && (
            <span className="sub muted">Showing the first {available.length} of {availableTotal} choices.</span>
          )}
        </div>
      )}

      {assigned.length === 0 ? (
        <div className="empty">
          <p className="muted">This application is not assigned to any instance yet.</p>
        </div>
      ) : (
        <>
          <div className="table-wrap app-instances-table">
            <table className="grid">
              <thead>
                <tr>
                  <th>Instance</th>
                  <th style={{ width: '160px' }}>Project</th>
                  <th style={{ width: '120px' }}>State</th>
                  <th style={{ width: '190px' }}>Last check</th>
                  <th style={{ width: '56px' }} aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {assigned.map(({ inst, assignment }) => {
                  const state = appState(assignment)
                  const assignmentReadOnly = readOnly || managedByGitOps(assignment)
                  return (
                    <tr key={inst.id}>
                      <td>
                        <div className="cell-title">
                          <Link className="entity-link" to={`/instances/${inst.id}`}>
                            <strong>{inst.name}</strong>
                          </Link>
                          <span className="cell-sub mono">{inst.id}</span>
                          <GitOpsBadge obj={assignment} />
                        </div>
                      </td>
                      <td><span className="project-badge">{projectLabel(inst.project)}</span></td>
                      <td><Badge state={state} label={STATE_LABEL[state]} small /></td>
                      <td className="muted">{fmt(assignment.last_check_at || assignment.latest_check?.finished_at)}</td>
                      <td>
                        <QuickMenu
                          label={`Actions for ${inst.name}`}
                          items={[
                            {
                              label: state === 'drift' ? 'Review drift' : 'Open instance',
                              icon: ExternalLink,
                              to: `/instances/${inst.id}`,
                            },
                            onUnassign ? { separator: true } : null,
                            onUnassign
                              ? {
                                  label: 'Unassign application',
                                  icon: Trash2,
                                  danger: true,
                                  disabled: assignmentReadOnly || !can('instances', 'update'),
                                  title: managedTitle(assignment) || managedTitle(app),
                                  onClick: () => onUnassign(inst),
                                }
                              : null,
                          ]}
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
          {instancesTotal > assigned.length && (
            <p className="table-note muted">
              Showing {assigned.length} of {instancesTotal} assigned instances. Open the Instances view to page through all of them.
            </p>
          )}
        </>
      )}
      </>
      )}

      {isRunner && (
        <div className="empty">
          <p className="muted">
            This application is dispatched as jobs to the runner fleet. Track runners and recent jobs on the
            <Link className="entity-link" to="/runners"> Runners page</Link>.
          </p>
        </div>
      )}

    </section>
  )
}
