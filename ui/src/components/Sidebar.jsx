import { useState, createContext, useContext, useEffect } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import HeliosMark from './HeliosMark.jsx'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { NAV_GROUPS } from '../nav.js'

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
            <HeliosMark />
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
          {NAV_GROUPS.map((group) => {
            const items = group.items.filter((item) => {
              const [resource, action] = item.perm.split('.')
              return can(resource, action)
            })
            if (!items.length) return null
            return (
              <div className="nav-group" key={group.id}>
                {!collapsed && <div className="nav-group-label">{group.label}</div>}
                <ul role="list">
                  {items.map((item) => {
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
              </div>
            )
          })}
        </nav>
      </aside>
      {mobileOpen && <div className="sidebar-backdrop" onClick={() => setMobileOpen(false)} aria-hidden="true" />}
    </>
  )
}