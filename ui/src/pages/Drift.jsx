import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  CircleCheck, ClipboardCheck, History, Play, RefreshCw, Server, TriangleAlert, Pencil,
} from 'lucide-react'
import { api, instanceOnline, projectLabel } from '../api/client.js'
import Badge from '../components/Badge.jsx'
import AutoHealChip, { appAutoHealMeta } from '../components/AutoHealChip.jsx'
import DiffModal from '../components/DiffModal.jsx'
import QuickMenu from '../components/QuickMenu.jsx'
import SearchBox from '../components/SearchBox.jsx'
import TagList from '../components/TagList.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import Pager from '../components/Pager.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useCommandRunner } from '../hooks/useCommandRunner.js'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'
import { useUrlState } from '../hooks/useUrlState.js'

const PAGE_SIZE = 50

const fmt = (t) => (t ? new Date(t).toLocaleString() : '—')

export default function Drift() {
  const { can } = useAuth()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [list, setList, resetList] = useUrlState(
    { q: '', project: '', app: '', offset: 0, limit: PAGE_SIZE },
    { clamp: { limit: [1, 500], offset: [0, 1e9] } },
  )
  const { q: search, project: projectFilter, app: appFilter, offset: pageOff, limit: pageSize } = list
  const [diffRun, setDiffRun] = useState(null)
  const [busy, setBusy] = useState({})
  const loadedRef = useRef(false)
  const requestSeq = useRef(0)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    const seq = ++requestSeq.current
    try {
      const params = new URLSearchParams({ limit: String(pageSize), offset: String(pageOff), facets: '1' })
      if (search) params.set('q', search)
      if (projectFilter) params.set('project', projectFilter === '__global__' ? '__global__' : projectFilter)
      if (appFilter) params.set('app_id', appFilter)
      const next = await api.drift(`?${params}`)
      if (seq !== requestSeq.current) return
      const nextTotal = Number(next.total) || 0
      if (!(next.items || []).length && pageOff > 0 && nextTotal > 0) {
        setList({ offset: Math.max(0, pageOff - pageSize) })
        return
      }
      setData(next)
    } catch (e) {
      if (seq === requestSeq.current && !loadedRef.current) toast.error(e.message)
    } finally {
      if (seq === requestSeq.current) {
        loadedRef.current = true
        setLoading(false)
      }
    }
  }, [appFilter, pageOff, pageSize, projectFilter, search, setList])

  const commands = useCommandRunner(refresh)
  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 10000 })
  const rows = useMemo(() => {
    return (data?.items || []).map(({ instance, app }) => ({
      inst: instance,
      app,
      state: app.stuck ? 'stuck' : 'drift',
      at: app.latest_check?.finished_at || app.last_check_at,
    }))
  }, [data])

  const total = Number(data?.total) || 0
  const projectOptions = useMemo(() => data?.facets?.projects || [], [data])
  const appOptions = useMemo(
    () => [...(data?.facets?.apps || [])].sort((a, b) => a.name.localeCompare(b.name)),
    [data],
  )
  const stats = {
    assignments: data?.stats?.total || 0,
    instances: data?.stats?.instances || 0,
    apps: data?.stats?.apps || 0,
    autoheal: data?.stats?.autoheal || 0,
  }

  const toggleAutoHeal = async (inst, app) => {
    const key = `${inst.id}:${app.application.id}`
    setBusy((b) => ({ ...b, [key]: true }))
    try {
      await api.setAutoHeal(inst.id, app.application.id, !app.autoheal)
      toast.success(app.autoheal ? 'Auto-heal disabled for this host' : 'Auto-heal enabled for this host')
      refresh()
    } catch (e) {
      toast.error(e.message)
    } finally {
      setBusy((b) => ({ ...b, [key]: false }))
    }
  }

  if (loading && !data) {
    return (
      <section>
        <h1>Drift</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  return (
    <section className="drift-page">
      <div className="ov-head">
        <div>
          <h1><TriangleAlert size={24} /> Drift</h1>
          <p className="page-intro">Assignments that differ from the desired revision, with the next action one click away.</p>
        </div>
        <div className="ov-actions">
          <button type="button" className="btn ghost small" onClick={refresh} title="Refresh drift">
            <RefreshCw size={14} /> Refresh
          </button>
        </div>
      </div>

      <div className="stats">
        <div className="stat-card"><span className="stat-dot drift" /><span className="stat-num">{stats.assignments}</span><span className="stat-label">Drifted assignments</span></div>
        <div className="stat-card"><span className="stat-dot total" /><span className="stat-num">{stats.instances}</span><span className="stat-label">Instances</span></div>
        <div className="stat-card"><span className="stat-dot apps" /><span className="stat-num">{stats.apps}</span><span className="stat-label">Applications</span></div>
        <div className="stat-card"><span className="stat-dot ok" /><span className="stat-num">{stats.autoheal}</span><span className="stat-label">Auto-heal enabled</span></div>
      </div>

      <div className="filters">
        <SearchBox
          className="filter-search"
          placeholder="Search instance, region or application…"
          value={search}
          onSearch={(value) => setList({ q: value, offset: 0 })}
        />
        <select value={projectFilter} onChange={(e) => setList({ project: e.target.value, offset: 0 })} aria-label="Project">
          <option value="">All projects</option>
          <option value="__global__">Global</option>
          {projectOptions.filter(Boolean).map((p) => <option key={p} value={p}>{p}</option>)}
        </select>
        <select value={appFilter} onChange={(e) => setList({ app: e.target.value, offset: 0 })} aria-label="Application">
          <option value="">All applications</option>
          {appOptions.map((app) => <option key={app.id} value={String(app.id)}>{app.name}</option>)}
        </select>
        {(search || projectFilter || appFilter) && (
          <button type="button" className="ghost small" onClick={() => resetList(['q', 'project', 'app', 'offset'])}>Clear</button>
        )}
      </div>

      {rows.length === 0 ? (
        <div className="empty">
          <p className="muted"><CircleCheck size={16} /> {total || search || projectFilter || appFilter ? 'No drift matches these filters.' : 'No drift detected. Every assignment matches its desired revision.'}</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="grid summary-table drift-table">
            <thead>
              <tr>
                <th style={{ width: '100px' }}>State</th>
                <th>Instance</th>
                <th>Application</th>
                <th>Changed tasks</th>
                <th style={{ width: '180px' }}>Auto-heal</th>
                <th style={{ width: '150px' }}>Checked</th>
                <th style={{ width: '210px' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ inst, app, state, at }) => {
                const key = `${inst.id}:${app.application.id}`
                const pending = commands.pending[key]
                const ah = appAutoHealMeta(inst, app)
                const tasks = app.latest_check?.changed_tasks || []
                const menu = [
                  { label: 'Open instance', icon: Server, to: `/instances/${inst.id}` },
                  { label: 'Open application', icon: History, to: `/applications/${app.application.id}` },
                  { separator: true },
                  {
                    label: 'Check now',
                    icon: ClipboardCheck,
                    onClick: () => commands.run(inst.id, app.application.id, 'check'),
                    disabled: !can('commands', 'create') || !!pending,
                  },
                  {
                    label: 'Apply now',
                    icon: Play,
                    onClick: () => commands.run(inst.id, app.application.id, 'apply'),
                    disabled: !can('commands', 'create') || !!pending,
                  },
                  { separator: true },
                  {
                    label: app.autoheal ? 'Disable auto-heal' : 'Enable auto-heal',
                    icon: Pencil,
                    onClick: () => toggleAutoHeal(inst, app),
                    disabled: !can('instances', 'update') || !!busy[key] || ah.locked,
                    title: ah.reason,
                  },
                ]
                return (
                  <tr key={key}>
                    <td><Badge state={state} label={state === 'stuck' ? 'Stuck' : 'Drift'} small /></td>
                    <td>
                      <div className="cell-title">
                        <Link to={`/instances/${inst.id}`} className="entity-link"><strong>{inst.name}</strong></Link>
                        <span className="cell-sub">
                          <span>{projectLabel(inst.project)}</span>
                          {inst.region && <span>· {inst.region}</span>}
                          <TagList tags={inst.tags} />
                          {!instanceOnline(inst) && <span className="offline-text">· offline</span>}
                        </span>
                      </div>
                    </td>
                    <td>
                      <Link to={`/applications/${app.application.id}`} className="entity-link">{app.application.name}</Link>
                      {app.stuck && <div className="cell-sub drift-text">Auto-heal paused after repeated non-convergence</div>}
                    </td>
                    <td>
                      {tasks.length ? (
                        <div className="drift-tasks">
                          {tasks.slice(0, 3).map((t, i) => (
                            <span key={i} className="drift-chip" title={t.host ? `host: ${t.host}` : undefined}>{t.task}</span>
                          ))}
                          {tasks.length > 3 && <span className="drift-chip">+{tasks.length - 3}</span>}
                        </div>
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                    <td>
                      <div className="cell-title">
                        <AutoHealChip
                          enabled={!!app.autoheal}
                          scope="assignment"
                          label={app.autoheal ? 'on' : ah.label}
                          reason={ah.reason}
                        />
                        {!ah.locked ? (
                          <label className="switch" title="Auto-apply on drift for this application on this host">
                            <input
                              type="checkbox"
                              checked={!!app.autoheal}
                              disabled={!can('instances', 'update') || !!busy[key]}
                              onChange={() => toggleAutoHeal(inst, app)}
                            />
                            <span className="slider" />
                          </label>
                        ) : (
                          <span className="cell-sub">{ah.hostLocked ? 'Host auto-heal is off' : 'Application auto-heal is off'}</span>
                        )}
                      </div>
                    </td>
                    <td className="muted">{fmt(at)}</td>
                    <td>
                      <div className="row-actions">
                        {app.latest_check?.id && (
                          <button type="button" className="btn warn small" onClick={() => setDiffRun(app.latest_check.id)}>
                            Review diff
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn small"
                          disabled={!can('commands', 'create') || !!pending}
                          aria-busy={pending?.action === 'apply'}
                          onClick={() => commands.run(inst.id, app.application.id, 'apply')}
                        >
                          {pending?.action === 'apply' && <span className="spinner xs" aria-hidden="true" />}
                          Apply now
                        </button>
                        <QuickMenu label={`Actions for ${inst.name} / ${app.application.name}`} items={menu} />
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      <Pager
        total={total}
        limit={pageSize}
        offset={pageOff}
        onPage={(offset) => setList({ offset }, { push: true })}
        onLimit={(limit) => setList({ limit, offset: 0 })}
        label="assignments"
      />
      {diffRun && <DiffModal runId={diffRun} onClose={() => setDiffRun(null)} />}
    </section>
  )
}
