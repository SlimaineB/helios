import { useState, useEffect, createContext, useContext } from 'react'
import { NavLink, Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import Instances from './pages/Instances.jsx'
import InstanceDetail from './pages/InstanceDetail.jsx'
import Topology from './pages/Topology.jsx'
import Applications from './pages/Applications.jsx'
import Repositories from './pages/Repositories.jsx'
import SyncWindows from './pages/SyncWindows.jsx'
import Audit from './pages/Audit.jsx'
import Projects from './pages/Projects.jsx'
import ConfigRepos from './pages/ConfigRepos.jsx'
import { api, setBasic, clearBasic } from './api/client.js'
import ToastContainer from './components/Toast.jsx'
import CommandPalette from './components/CommandPalette.jsx'
import Sidebar, { SidebarProvider } from './components/Sidebar.jsx'
import ErrorBoundary from './components/ErrorBoundary.jsx'
import { Menu } from 'lucide-react'

const Auth = createContext(null)
export const useAuth = () => useContext(Auth)

export default function App() {
  const [state, setState] = useState(null)
  const [realm, setRealm] = useState({ oidc: false, basic_admin: false })
  const [cmdOpen, setCmdOpen] = useState(false)
  const navigate = useNavigate()

  const loadMe = async (setBasicCreds) => {
    try {
      const me = await api.me()
      setState({ me })
      return true
    } catch (err) {
      if (setBasicCreds) {
        setBasicCreds()
        const me = await api.me()
        setState({ me })
        return true
      }
      setState({ loggedOut: true })
      return false
    }
  }

  useEffect(() => {
    ;(async () => {
      try {
        setRealm(await api.realm())
      } catch {
        /* realm is optional */
      }
      await loadMe(null)
    })()
  }, [])

  const can = (resource, action) =>
    !!state?.me?.permissions?.includes(`${resource}.${action}`)

  const logout = async () => {
    try {
      if (realm.oidc) await api.logout()
    } catch {
      /* ignore */
    }
    clearBasic()
    setState({ loggedOut: true })
  }

  useEffect(() => {
    const handler = (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setCmdOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  if (state === null) {
    return (
      <div className="center-screen muted">
        Connecting to the control plane…
      </div>
    )
  }

  if (state.loggedOut) {
    return <LoginScreen realm={realm} onDone={loadMe} />
  }

  return (
    <SidebarProvider>
      <div className="app">
        <Auth.Provider value={{ me: state.me, can }}>
          <ToastContainer />
          <CommandPalette open={cmdOpen} onClose={() => setCmdOpen(false)} can={can} navigate={navigate} />
          <Sidebar can={can} />
          <main className="content">
            <header className="topbar">
              <button className="mobile-menu-btn" onClick={() => {
                window.dispatchEvent(new CustomEvent('sidebar:toggle'))
              }} aria-label="Toggle menu">
                <Menu size={20} />
              </button>
              <div className="user">
                <span className="user-chip" title={state.me?.email || ''}>
                  {state.me?.name || 'operator'}
                </span>
                {state.me?.roles?.length > 0 && (
                  <span className="user-roles">{state.me.roles.join(', ')}</span>
                )}
                <button className="ghost small" onClick={logout}>
                  Sign out
                </button>
              </div>
            </header>
            <ErrorBoundary>
              <Routes>
                <Route path="/" element={<Applications />} />
                <Route path="/instances" element={<Instances />} />
                <Route path="/instances/:id" element={<InstanceDetail />} />
                <Route path="/topology" element={<Topology />} />
                <Route path="/applications" element={<Navigate to="/" replace />} />
                <Route path="/projects" element={<Projects />} />
                <Route path="/sync-windows" element={<SyncWindows />} />
                <Route path="/repositories" element={<Repositories />} />
                <Route path="/gitops" element={<ConfigRepos />} />
                <Route path="/audit" element={<Audit />} />
              </Routes>
            </ErrorBoundary>
          </main>
        </Auth.Provider>
      </div>
    </SidebarProvider>
  )
}

function LoginScreen({ realm, onDone }) {
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(false)

  const submitBasic = async (e) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      try {
        await api.logout()
      } catch {
        /* ignore */
      }
      setBasic(user, pass)
      const ok = await onDone(() => clearBasic())
      if (!ok) throw new Error('invalid credentials')
    } catch (err) {
      clearBasic()
      setError(err.message || 'invalid credentials')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <div className="login-card">
        <div className="brand login-brand">
          <span className="brand-mark">⌁</span>
          <span className="brand-text">
            <span className="brand-name">Helios</span>
            <span className="brand-tag">operator sign-in</span>
          </span>
        </div>
        {realm.oidc ? (
          <>
            <a className="btn primary" href="/api/v1/auth/login">
              Sign in with SSO
            </a>
            {realm.basic_admin && (
              <div className="login-or">
                or use the local admin account
              </div>
            )}
          </>
        ) : null}
        {realm.basic_admin && (
          <form className="login-form" onSubmit={submitBasic}>
            <label>
              Username
              <input value={user} onChange={(e) => setUser(e.target.value)} autoComplete="username" />
            </label>
            <label>
              Password
              <input
                type="password"
                value={pass}
                onChange={(e) => setPass(e.target.value)}
                autoComplete="current-password"
              />
            </label>
            {error && <div className="err">{error}</div>}
            <button className="btn primary" type="submit" disabled={busy || !user || !pass}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        )}
        {!realm.oidc && !realm.basic_admin && (
          <p className="muted">No operator authentication is configured on this server.</p>
        )}
      </div>
    </div>
  )
}