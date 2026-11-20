import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api/client.js'
import ApplicationDetail from '../components/ApplicationDetail.jsx'
import ApplicationHistory from '../components/ApplicationHistory.jsx'
import VariableEditor from '../components/VariableEditor.jsx'
import ConfirmDialog from '../components/ConfirmDialog.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { useAuth } from '../App.jsx'
import { toast } from '../components/Toast.jsx'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

const DETAIL_INSTANCE_LIMIT = 500
const ASSIGN_CHOICE_LIMIT = 200

export default function ApplicationDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { can } = useAuth()
  const [app, setApp] = useState(null)
  const [stats, setStats] = useState(null)
  const [repo, setRepo] = useState(null)
  const [instances, setInstances] = useState([])
  const [instancesTotal, setInstancesTotal] = useState(0)
  const [available, setAvailable] = useState([])
  const [availableTotal, setAvailableTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [varsOpen, setVarsOpen] = useState(false)
  const [confirm, setConfirm] = useState(null)
  const loadedRef = useRef(false)

  const refresh = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      const current = await api.application(id)
      const project = current.project
        ? `project=${encodeURIComponent(current.project)}`
        : 'project=__global__'
      const [statData, repos, page, choices] = await Promise.all([
        api.applicationStats().catch(() => ({ items: [] })),
        api.repositories().catch(() => []),
        api.instances(`?app_id=${current.id}&limit=${DETAIL_INSTANCE_LIMIT}`),
        api.instances(`?${project}&not_app_id=${current.id}&limit=${ASSIGN_CHOICE_LIMIT}&compact=1`),
      ])
      setApp(current)
      setStats((statData.items || []).find((item) => item.id === current.id) || null)
      setRepo((repos || []).find((item) =>
        item.url === current.repo_url && (item.project || '') === (current.project || '')) || null)
      setInstances(page.items || [])
      setInstancesTotal(Number(page.total) || 0)
      setAvailable(choices.items || [])
      setAvailableTotal(Number(choices.total) || 0)
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [id])

  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 30000, enabled: !historyOpen && !varsOpen && !confirm })

  const edit = () => navigate(`/applications?edit=${app.id}`)

  const remove = () => setConfirm({
    title: 'Delete application',
    message: `Delete "${app.name}"? This cannot be undone.`,
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.deleteApplication(app.id)
        toast.success('Application deleted')
        navigate('/applications')
      } catch (e) {
        toast.error(e.message)
      }
    },
  })

  const unassign = (inst) => setConfirm({
    title: 'Unassign application',
    message: `Unassign "${app.name}" from "${inst.name}"? The application stays on other instances.`,
    variant: 'danger',
    onConfirm: async () => {
      setConfirm(null)
      try {
        await api.unassign(inst.id, app.id)
        toast.success('Application unassigned')
        refresh()
      } catch (e) {
        toast.error(e.message)
      }
    },
  })

  const assign = async (inst) => {
    try {
      await api.assign(inst.id, app.id)
      toast.success('Application assigned')
      refresh()
    } catch (e) {
      toast.error(e.message)
    }
  }

  if (!app) {
    return (
      <section>
        <Link to="/applications" className="back-link">← Applications</Link>
        <h1>Application Detail</h1>
        {loading ? <SkeletonCard /> : <p className="muted">Application not found.</p>}
      </section>
    )
  }

  return (
    <section>
      <Link to="/applications" className="back-link">← Applications</Link>
      <ApplicationDetail
        app={app}
        stats={stats}
        repo={repo}
        instances={instances}
        instancesTotal={instancesTotal}
        availableInstances={available}
        availableTotal={availableTotal}
        can={can}
        onEdit={edit}
        onVars={() => setVarsOpen(true)}
        onHistory={() => setHistoryOpen(true)}
        onDelete={remove}
        onUnassign={unassign}
        onAssign={assign}
      />
      {historyOpen && <ApplicationHistory app={app} onClose={() => setHistoryOpen(false)} />}
      {varsOpen && (
        <VariableEditor
          title={`Variables · ${app.name}`}
          subtitle="Application defaults, shared by every assigned host (≈ group_vars). Per-host overrides live on the instance."
          load={() => api.appVariables(app.id)}
          save={(items) => api.saveAppVariables(app.id, items)}
          onClose={() => setVarsOpen(false)}
        />
      )}
      {confirm && (
        <ConfirmDialog
          open
          onConfirm={confirm.onConfirm}
          onCancel={() => setConfirm(null)}
          title={confirm.title}
          message={confirm.message}
          variant={confirm.variant}
        />
      )}
    </section>
  )
}
