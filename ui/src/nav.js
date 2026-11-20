import {
  Clock, Database, FileText, Gauge, GitBranch, Layers,
  RefreshCw, Server, Settings as SettingsIcon, TriangleAlert,
} from 'lucide-react'

export const NAV_GROUPS = [
  {
    id: 'fleet',
    label: 'Fleet',
    items: [
      { path: '/', label: 'Overview', icon: Gauge, perm: 'instances.get' },
      { path: '/instances', label: 'Instances', icon: Server, perm: 'instances.get' },
      { path: '/drift', label: 'Drift', icon: TriangleAlert, perm: 'instances.get' },
    ],
  },
  {
    id: 'automation',
    label: 'Automation',
    items: [
      { path: '/applications', label: 'Applications', icon: Database, perm: 'applications.get' },
      { path: '/projects', label: 'Projects', icon: Layers, perm: 'projects.get' },
      { path: '/gitops', label: 'GitOps', icon: RefreshCw, perm: 'configrepos.get' },
      { path: '/sync-windows', label: 'Sync windows', icon: Clock, perm: 'windows.get' },
      { path: '/repositories', label: 'Repositories', icon: GitBranch, perm: 'repositories.get' },
    ],
  },
  {
    id: 'system',
    label: 'System',
    items: [
      { path: '/audit', label: 'Audit log', icon: FileText, perm: 'audit.get' },
      { path: '/settings', label: 'Settings', icon: SettingsIcon, perm: 'settings.get' },
    ],
  },
]

export const NAV_ITEMS = NAV_GROUPS.flatMap((group) =>
  group.items.map((item) => ({ ...item, group: group.label }))
)
