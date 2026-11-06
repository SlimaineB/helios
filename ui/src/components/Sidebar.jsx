import { useState, createContext, useContext, useEffect } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import {
  Server, Database, GitBranch, Layers, Clock,
  ChevronLeft, ChevronRight, FileText, Network, RefreshCw
} from 'lucide-react'

const SidebarContext = createContext(null)
export const useSidebar = () => useContext(SidebarContext)

export function SidebarProvider({ children }) {
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

  useEffect(() => {
    const handler = () => setMobileOpen(m => !m)
    window.addEventListener('sidebar:toggle', handler)
    return () => window.removeEventListener('sidebar:toggle', handler)
  }, [])

  return (
    <SidebarContext.Provider value={{ collapsed, setCollapsed, mobileOpen, setMobileOpen }}>
      {children}
    </SidebarContext.Provider>
  )
}

const NAV_ITEMS = [
  { path: '/', label: 'Applications', icon: Database, perm: 'applications.get' },
  { path: '/instances', label: 'Instances', icon: Server, perm: 'instances.get' },
  { path: '/topology', label: 'Topology', icon: Network, perm: 'instances.get' },
  { path: '/projects', label: 'Projects', icon: Layers, perm: 'projects.get' },
  { path: '/sync-windows', label: 'Sync windows', icon: Clock, perm: 'windows.get' },
  { path: '/repositories', label: 'Repositories', icon: GitBranch, perm: 'repositories.get' },
  { path: '/gitops', label: 'GitOps', icon: RefreshCw, perm: 'configrepos.get' },
  { path: '/audit', label: 'Audit log', icon: FileText, perm: 'audit.get' },
]

export default function Sidebar({ can }) {
  const { collapsed, setCollapsed, mobileOpen, setMobileOpen } = useSidebar()
  const location = useLocation()
  const isMobile = typeof window !== 'undefined' && window.innerWidth < 768

  useEffect(() => {
    const handleResize = () => {
      const mobile = window.innerWidth < 768
      if (!mobile) setMobileOpen(false)
    }
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [setMobileOpen])

  return (
    <>
      <aside className={`sidebar ${collapsed ? 'collapsed' : ''} ${mobileOpen ? 'mobile-open' : ''}`} role="navigation" aria-label="Main navigation">
        <div className="sidebar-header">
          <div className="brand">
            <span className="brand-mark">⌁</span>
            {!collapsed && (
              <span className="brand-text">
                <span className="brand-name">Helios</span>
                <span className="brand-tag">pull-based drift control</span>
              </span>
            )}
          </div>
          {!isMobile && (
            <button
              className="sidebar-toggle"
              onClick={() => setCollapsed(!collapsed)}
              aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
              aria-expanded={!collapsed}
            >
              {collapsed ? <ChevronRight size={18} /> : <ChevronLeft size={18} />}
            </button>
          )}
        </div>
        <nav className="sidebar-nav">
          <ul role="list">
            {NAV_ITEMS.map((item) => {
              if (item.perm) {
                const [resource, action] = item.perm.split('.')
                if (!can(resource, action)) return null
              }
              const active = item.path === '/'
                ? location.pathname === '/'
                : location.pathname === item.path || location.pathname.startsWith(item.path + '/')
              return (
                <li key={item.path}>
                  <NavLink
                    to={item.path}
                    className={`nav-item ${active ? 'active' : ''}`}
                    title={collapsed ? item.label : undefined}
                    aria-current={active ? 'page' : undefined}
                    onClick={() => setMobileOpen(false)}
                  >
                    <item.icon className="nav-icon" size={18} aria-hidden="true" />
                    {!collapsed && <span className="nav-label">{item.label}</span>}
                  </NavLink>
                </li>
              )
            })}
          </ul>
        </nav>
      </aside>
      {isMobile && <div className="sidebar-backdrop" onClick={() => setMobileOpen(false)} aria-hidden="true" />}
    </>
  )
}