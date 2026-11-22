import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, ClipboardCheck, Clock, Filter, MapPin, Pencil, Play, Server, Trash2 } from 'lucide-react'
import { api, instanceOnline, projectLabel, stateLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import GitOpsBadge, { managedByGitOps, managedTitle } from '../components/GitOpsBadge.jsx'
import AutoHealChip, { appAutoHealMeta, hostAutoHealOn } from '../components/AutoHealChip.jsx'
import StatusIcon from '../components/StatusIcon.jsx'
import LogsModal from '../components/LogsModal.jsx'
import DiffModal from '../components/DiffModal.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'
import VariableEditor from '../components/VariableEditor.jsx'
import Pager from '../components/Pager.jsx'
import RegionPicker from '../components/RegionPicker.jsx'
import QuickMenu from '../components/QuickMenu.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import Modal from '../components/Modal.jsx'
import CopyButton, { copyText } from '../components/CopyButton.jsx'
import TagList, { formatTags, parseTags } from '../components/TagList.jsx'
import TunnelDiagram from '../components/TunnelDiagram.jsx'
import { SkeletonCard, SkeletonTable } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'

const PAGE_SIZE = 25

export default function InstanceDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
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
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [regionValue, setRegionValue] = useState('')
  const [tagsValue, setTagsValue] = useState('')
  const [renaming, setRenaming] = useState(false)
  const [regions, setRegions] = useState([])
  const [tab, setTab] = useState('apps')
  const tabsRef = useRef(null)

  const appNamesById = useMemo(
    () => new Map((inst?.apps || []).map((a) => [a.application.id, a.application.name])),
    [inst]
  )
  const managedByAppId = useMemo(
    () => new Map((inst?.apps || []).filter((a) => a.managed_by).map((a) => [a.application.id, a.managed_by])),
    [inst]
  )
  const appFilterName = appNamesById.get(Number(appFilter))

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
  }, [id, runsOff, appFilter])

  const commands = useCommandRunner(refresh)
  const modalOpen = !!logRun || !!diffRun || !!varsApp || !!confirm
  useEffect(() => {
    refresh()
  }, [refresh])
  useLiveRefresh(refresh, { interval: 30000, enabled: !modalOpen })

  useEffect(() => {
    api.regions().then(setRegions).catch(() => setRegions([]))
  }, [id])

  useEffect(() => {
    setRunsOff(0)
  }, [appFilter])

  const online = instanceOnline(inst)
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

  const openRename = () => {
    setRenameValue(inst.name)
    setRegionValue(inst.region || '')
    setTagsValue(formatTags(inst.tags))
    setRenameOpen(true)
  }
  const saveRename = async (e) => {
    e.preventDefault()
    const name = renameValue.trim()
    if (!name) return
    setRenaming(true)
    try {
      await api.updateInstance(inst.id, name, inst.project, regionValue.trim(), parseTags(tagsValue))
      toast.success('Instance updated')
      setRenameOpen(false)
      refresh()
    } catch (err) {
      toast.error(err.message)
    } finally {
      setRenaming(false)
    }
  }
  const askDelete = () => setConfirm({
    title: 'Delete instance',
    message: `Delete "${inst.name}"? Its applications are unassigned and its run history is removed. The agent will stop working until it is registered again.`,
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.deleteInstance(inst.id)
        toast.success('Instance deleted')
        navigate('/instances')
      } catch (err) {
        toast.error(err.message)
      }
    },
  })

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
        <Link to="/instances" className="back-link"><ArrowLeft size={14} /> Back to instances</Link>
        <h1>Instance Detail</h1>
        {loading ? <SkeletonCard /> : <div className="empty"><p className="muted">Instance not found.</p></div>}
      </section>
    )
  }

  if (loading) {
    return (
      <section>
        <Link to="/instances" className="back-link"><ArrowLeft size={14} /> Back to instances</Link>
        <h1>{inst.name} <Badge state={inst.state} label={stateLabel(inst.state)} /></h1>
        <div className="meta"><SkeletonTable rows={5} columns={1} /></div>
        <h2>Applications</h2>
        <SkeletonCard />
        <h2>Run history</h2>
        <SkeletonTable rows={5} columns={8} />
      </section>
    )
  }

  const anyPending = Object.keys(commands.pending).some((key) => key.startsWith(`${inst.id}:`))
  const runAll = (action) => {
    for (const app of inst.apps || []) commands.run(inst.id, app.application.id, action)
  }
  const filterAppLogs = (app) => {
    setAppFilter(String(app.application.id))
    setTab('history')
    requestAnimationFrame(() => {
      tabsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    })
  }

  return (
    <section>
      <Link to="/instances" className="back-link"><ArrowLeft size={14} /> Back to instances</Link>

      <div className="detail-page-head">
        <div className="detail-page-title">
          <h1>{inst.name}</h1>
          <div className="detail-chips">
            <Badge state={inst.state} label={stateLabel(inst.state)} />
            {isTunnel && <Badge state="tunnel" label="tunnel" small />}
            <span className={`detail-chip ${online ? 'online' : 'offline'}`}>
              <span className={`dot ${online ? 'online' : 'offline'}`} />
              {online ? 'Online' : 'Offline'}
            </span>
            <span className="project-badge">{projectLabel(inst.project)}</span>
            {inst.region && <span className="detail-chip"><MapPin size={13} /> {inst.region}</span>}
            <span className="detail-chip"><Clock size={13} /> {inst.last_seen ? `seen ${new Date(inst.last_seen).toLocaleString()}` : 'never seen'}</span>
            {inst.apps.length > 0 && (
              <span className="detail-chip">{inst.apps.length} application{inst.apps.length === 1 ? '' : 's'}</span>
            )}
          </div>
          {inst.tags?.length > 0 && <TagList tags={inst.tags} className="detail-tags" />}
        </div>
        <div className="detail-head-actions">
          {inst.apps.length > 0 && can('commands', 'create') && (
            <>
              <button type="button" className="btn ghost" disabled={anyPending} onClick={() => runAll('check')}>
                <ClipboardCheck size={15} /> Check all
              </button>
              <button type="button" className="btn" disabled={anyPending} onClick={() => runAll('apply')}>
                <Play size={15} /> Apply all
              </button>
            </>
          )}
          {can('instances', 'update') && (
            <button type="button" className="btn ghost" onClick={openRename}>
              <Pencil size={15} /> Edit
            </button>
          )}
          {can('instances', 'delete') && (
            <button type="button" className="btn danger" onClick={askDelete}>
              <Trash2 size={15} /> Delete
            </button>
          )}
        </div>
      </div>

      <div className="host-setting-bar">
        <div className="host-setting-copy">
          <strong>Host auto-apply</strong>
          <span className="sub muted">Master switch for this host. Per-application switches below still apply (most restrictive wins).</span>
        </div>
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
      </div>

      <details className="detail-panel">
        <summary><Server size={15} /> Host details</summary>
        <div className="detail-panel-body">
          <div className="host-detail-grid">
            <div className="detail"><label>ID</label><div className="value mono"><CopyButton value={String(inst.id)} label="" size="small" title="Copy ID" />{inst.id}</div></div>
            <div className="detail"><label>Hostname</label><div className="value">{inst.hostname || '—'}</div></div>
            <div className="detail"><label>Platform</label><div className="value">{inst.platform || '—'}</div></div>
            <div className="detail"><label>Version</label><div className="value">{inst.version || '—'}</div></div>
            <div className="detail"><label>Mode</label><div className="value">{isTunnel ? 'tunnel · reverse SSH' : 'agent'}</div></div>
            {inst.host_info?.os && <div className="detail"><label>OS</label><div className="value">{inst.host_info.os}</div></div>}
            {inst.host_info?.arch && <div className="detail"><label>Arch</label><div className="value">{inst.host_info.arch}</div></div>}
            {inst.host_info?.cpu_count > 0 && <div className="detail"><label>CPU</label><div className="value">{inst.host_info.cpu_count} vCPU</div></div>}
            {inst.host_info?.memory_mb > 0 && <div className="detail"><label>Memory</label><div className="value">{inst.host_info.memory_mb} MB</div></div>}
            {inst.host_info?.python && <div className="detail"><label>Python</label><div className="value">{inst.host_info.python}</div></div>}
            {inst.host_info?.ansible && <div className="detail"><label>Ansible</label><div className="value">{inst.host_info.ansible}</div></div>}
          </div>

          {isTunnel && (
            <div className="detail-subsection">
              <div className="detail-section-head">
                <strong>Reverse SSH tunnel</strong>
                <Badge state="tunnel" label="tunnel" small />
              </div>
              <div className="host-detail-grid">
                <div className="detail"><label>Bastion</label><div className="value mono">{inst.tunnel_gateway || '—'}</div></div>
                <div className="detail"><label>Tunnel port</label><div className="value mono">{inst.tunnel_port || '—'}</div></div>
                <div className="detail"><label>Last tunnel</label><div className="value">{inst.last_tunnel_at ? new Date(inst.last_tunnel_at).toLocaleString() : 'never'}</div></div>
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
        </div>
      </details>

      <div className="seg detail-tabs" role="tablist" aria-label="Instance detail sections" ref={tabsRef}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'apps'}
          className={'seg-btn' + (tab === 'apps' ? ' active' : '')}
          onClick={() => setTab('apps')}
        >
          Applications <span className="detail-tab-count">{inst.apps.length}</span>
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'history'}
          className={'seg-btn' + (tab === 'history' ? ' active' : '')}
          onClick={() => setTab('history')}
        >
          Run history <span className="detail-tab-count">{runsTotal}</span>
        </button>
      </div>

      {tab === 'apps' ? (
        <>
      {inst.apps.length === 0 && (
        <div className="empty"><p className="muted">No applications assigned.</p></div>
      )}
      {inst.apps.map((a) => {
        const pending = commands.pending[`${inst.id}:${a.application.id}`]
        const ah = appAutoHealMeta(inst, a)
        const readOnly = managedByGitOps(a)
        return (
        <div className="card" key={a.application.id}>
          <div className="card-head">
            <strong>{a.application.name}</strong>
            <GitOpsBadge obj={a} />
            {a.stuck && <Badge state="stuck" label="stuck" small />}
            {a.frozen && <Badge state="failed" label="auto-apply paused" small />}
            <StatusIcon state={a.latest_check?.status || 'unknown'} size={16} />
            <Badge state={a.latest_check?.status || 'unknown'} label={a.latest_check?.status || 'no check yet'} small />
            {a.stuck ? (
              <button className="small warn" title="Resume auto-apply once the playbook is fixed" disabled={readOnly || !can('instances', 'update')} onClick={() => rearm(a)}>Resume auto-apply</button>
            ) : (
              <label className="switch" title={readOnly ? managedTitle(a) : ah.locked ? ah.reason : 'Auto-apply on drift for this application on this host'}>
                <input type="checkbox" checked={a.autoheal} disabled={readOnly || !can('instances', 'update') || ah.locked} onChange={() => toggleAutoHeal(a)} /><span className="slider" />
              </label>
            )}
            <span className="sub">{ah.label}</span>
            {a.target_revision && (
              <>
                <Badge state="pinned" label={`pinned @${a.target_revision.slice(0, 9)}`} small />
                <button className="small" disabled={readOnly || !can('instances', 'update')} title={readOnly ? managedTitle(a) : 'resume tracking the application branch'} onClick={() => trackBranch(a)}>Track branch</button>
              </>
            )}
            <span className="mono muted"><CopyButton value={a.application.repo_url} label="" size="small" title="Copy repo URL" />{a.application.repo_url}</span>
            <span className="actions">
              <button className="small" title="Filter run history to this application" onClick={() => filterAppLogs(a)}><Filter size={13} /> Filter logs</button>{' '}
              <button className="small warn" title="Apply the desired configuration now" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'apply'} onClick={() => commands.run(inst.id, a.application.id, 'apply')}>{pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}Apply now</button>{' '}
              <button className="small" title="Check for drift without changing the host" disabled={!can('commands', 'create') || !!pending} aria-busy={pending?.action === 'check'} onClick={() => commands.run(inst.id, a.application.id, 'check')}>{pending?.action === 'check' && <span className="spinner xs" aria-hidden="true" />}Check now</button>{' '}
              <QuickMenu
                label={`More actions for ${a.application.name}`}
                items={[
                  { label: 'Host variables', onClick: () => setVarsApp(a.application), disabled: readOnly || !can('instances', 'update'), title: managedTitle(a) },
                  { label: 'Unassign application', onClick: () => confirmUnassign(a), disabled: readOnly || !can('instances', 'update'), danger: true, title: managedTitle(a) },
                ]}
              />
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

        </>
      ) : (
        <>
          {inst.apps.length > 0 && (
            <div className="filters" style={{ marginBottom: '12px' }}>
              <label htmlFor="history-app-filter">Application</label>
              <select
                id="history-app-filter"
                value={appFilter}
                onChange={(e) => setAppFilter(e.target.value)}
                title="Filter run history by application"
              >
                <option value="">All applications</option>
                {inst.apps.map((a) => (
                  <option key={a.application.id} value={String(a.application.id)}>{a.application.name}</option>
                ))}
                {appFilter && !appFilterName && <option value={appFilter}>Application #{appFilter}</option>}
              </select>
              {appFilterName && (
                <button type="button" className="ghost small" onClick={() => setAppFilter('')}>Clear filter</button>
              )}
            </div>
          )}
          <div className="table-wrap">
            <table className="grid run-table">
              <thead><tr><th>#</th><th>Mode</th><th>Status</th><th style={{ width: '140px' }}>Application</th><th>Trigger</th><th>Commit</th><th>Summary</th><th>When</th><th style={{ width: '56px' }} aria-label="Actions" /></tr></thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id}>
                    <td className="mono run-id">#{r.id}</td>
                    <td>{r.mode === 'apply' ? 'Apply' : 'Check'}</td>
                    <td><Badge state={r.status} label={r.status} small /></td>
                    <td className="mono">{appNamesById.get(r.app_id) || `#${r.app_id}`}</td>
                    <td>{r.trigger}</td>
                    <td className="mono run-commit" title={[r.commit_after, r.commit_msg].filter(Boolean).join(' — ')}>{r.commit_after?.slice(0, 9) || '—'}</td>
                    <td className="mono run-summary" title={Object.entries(r.summary || {}).map(([k, v]) => `${k}:${v}`).join(' ')}>{Object.entries(r.summary || {}).map(([k, v]) => `${k}:${v}`).join(' ')}</td>
                    <td className="muted run-when">{new Date(r.finished_at).toLocaleString()}</td>
                    <td>
                      <QuickMenu
                        label={`Actions for run ${r.id}`}
                        items={[
                          {
                            label: 'Copy run ID',
                            onClick: async () => {
                              const ok = await copyText(String(r.id))
                              if (ok) toast.success('Copied run ID', 1500)
                              else toast.error('Failed to copy', 2000)
                            },
                          },
                          { separator: true },
                          r.mode === 'check' && r.status === 'drift'
                            ? { label: 'Review drift', onClick: () => setDiffRun(r.id) }
                            : null,
                          { label: 'View logs', onClick: () => setLogRun({ runId: r.id, view: 'text' }) },
                          r.commit_after ? { separator: true } : null,
                          r.commit_after
                            ? {
                                label: 'Rollback to this commit',
                                onClick: () => confirmRollback(r),
                                disabled: managedByAppId.has(r.app_id) || !can('instances', 'update'),
                                danger: true,
                                title: managedByAppId.has(r.app_id) ? `Managed by GitOps config repo "${managedByAppId.get(r.app_id)}". Edit the manifest in Git.` : '',
                              }
                            : null,
                        ]}
                      />
                    </td>
                  </tr>
                ))}
                {runs.length === 0 && <tr><td colSpan={9} className="muted">{appFilter ? 'No runs for this application.' : 'No runs yet.'}</td></tr>}
              </tbody>
            </table>
          </div>
          <Pager total={runsTotal} limit={PAGE_SIZE} offset={runsOff} onPage={setRunsOff} />
        </>
      )}

      {logRun && <LogsModal runId={logRun.runId} initialView={logRun.view} onClose={() => setLogRun(null)} />}
      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
      {varsApp && <VariableEditor title={`Host variables · ${inst.name}`} subtitle={`Overrides for ${varsApp.name} on this host only (≈ host_vars); win over the app defaults.`} load={() => api.assignmentVariables(id, varsApp.id)} save={(items) => api.saveAssignmentVariables(id, varsApp.id, items)} onClose={() => setVarsApp(null)} />}
      {renameOpen && (
        <Modal title="Edit instance" onClose={() => setRenameOpen(false)}>
          <form className="form-card" onSubmit={saveRename}>
            <label>Instance name
              <input autoFocus value={renameValue} onChange={(e) => setRenameValue(e.target.value)} />
            </label>
            <div className="form-field">
              <span className="form-field-label">Region</span>
              <RegionPicker
                value={regionValue}
                onChange={setRegionValue}
                regions={regions}
                onRegionCreated={(region) => setRegions((prev) =>
                  [...prev.filter((r) => r.id !== region.id), region].sort((a, b) => a.name.localeCompare(b.name)))}
              />
              <span className="sub">Managed from the Regions page or created here.</span>
            </div>
            <label>Tags
              <input value={tagsValue} onChange={(e) => setTagsValue(e.target.value)} placeholder="e.g. prod, team-a" />
              <span className="sub">Comma-separated labels for search and grouping.</span>
            </label>
            <div className="form-actions">
              <button type="button" className="ghost" onClick={() => setRenameOpen(false)}>Cancel</button>
              <button type="submit" disabled={renaming || !renameValue.trim()}>Save</button>
            </div>
          </form>
        </Modal>
      )}
      {confirm && <ConfirmDialog open onConfirm={confirm.onConfirm} onCancel={() => setConfirm(null)} title={confirm.title} message={confirm.message} variant={confirm.variant} />}
    </section>
  )
}