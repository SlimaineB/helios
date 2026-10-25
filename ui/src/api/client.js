let basicCreds = null

function loadCreds() {
  if (typeof window === 'undefined') return null
  try {
    const stored = localStorage.getItem('anh_basic_creds')
    if (stored) return stored
  } catch {
    /* ignore */
  }
  return null
}

function saveCreds(creds) {
  if (typeof window === 'undefined') return
  try {
    if (creds) localStorage.setItem('anh_basic_creds', creds)
    else localStorage.removeItem('anh_basic_creds')
  } catch {
    /* ignore */
  }
}

basicCreds = loadCreds()

export function setBasic(user, pass) {
  basicCreds = btoa(`${user}:${pass}`)
  saveCreds(basicCreds)
}

export function clearBasic() {
  basicCreds = null
  saveCreds(null)
}

async function request(method, path, body) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
  }
  if (basicCreds) opts.headers['Authorization'] = `Basic ${basicCreds}`
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(path, opts)
  if (!res.ok) {
    let msg = `${method} ${path} failed: ${res.status}`
    try {
      const data = await res.json()
      if (data.error) msg = data.error
    } catch {
      /* not json */
    }
    throw new Error(msg)
  }
  if (res.status === 204) return null
  const text = await res.text()
  return text ? JSON.parse(text) : null
}

export const api = {
  me: () => request('GET', '/api/v1/auth/me'),
  realm: () => request('GET', '/api/v1/auth/realm'),
  logout: () => request('GET', '/api/v1/auth/logout'),

  instances: (query) => request('GET', `/api/v1/instances${query || ''}`),
  instance: (id) => request('GET', `/api/v1/instances/${id}`),
  createInstance: (name, project) => request('POST', '/api/v1/instances', { name, project }),
  deleteInstance: (id) => request('DELETE', `/api/v1/instances/${id}`),
  assign: (id, appId) => request('POST', `/api/v1/instances/${id}/assign`, { app_id: appId }),
  unassign: (id, appId) => request('DELETE', `/api/v1/instances/${id}/assign/${appId}`),
  sync: (id, appId) => request('POST', `/api/v1/instances/${id}/apply`, { app_id: appId }),
  check: (id, appId) => request('POST', `/api/v1/instances/${id}/check`, { app_id: appId }),
  runs: (id, query) => request('GET', `/api/v1/instances/${id}/runs${query || ''}`),
  appRuns: (id, query) => request('GET', `/api/v1/applications/${id}/runs${query || ''}`),
  runLog: (runId) => request('GET', `/api/v1/runs/${runId}/log`),

  setAutoHeal: (id, appId, enabled, inherit) =>
    request('PUT', `/api/v1/instances/${id}/assign/${appId}/autoheal`, { enabled, inherit }),
  setRevision: (id, appId, revision) =>
    request('PUT', `/api/v1/instances/${id}/assign/${appId}/revision`, { revision }),

  audit: (query) => request('GET', `/api/v1/audit${query || ''}`),

  syncWindows: () => request('GET', '/api/v1/sync-windows'),
  createSyncWindow: (w) => request('POST', '/api/v1/sync-windows', w),
  updateSyncWindow: (id, w) => request('PUT', `/api/v1/sync-windows/${id}`, w),
  deleteSyncWindow: (id) => request('DELETE', `/api/v1/sync-windows/${id}`),

  projects: () => request('GET', '/api/v1/projects'),
  createProject: (p) => request('POST', '/api/v1/projects', p),
  updateProject: (id, p) => request('PUT', `/api/v1/projects/${id}`, p),
  deleteProject: (id) => request('DELETE', `/api/v1/projects/${id}`),

  applications: () => request('GET', '/api/v1/applications'),
  createApplication: (app) => request('POST', '/api/v1/applications', app),
  updateApplication: (app) => request('PUT', `/api/v1/applications/${app.id}`, app),
  deleteApplication: (id) => request('DELETE', `/api/v1/applications/${id}`),

  appVariables: (appId) => request('GET', `/api/v1/applications/${appId}/variables`),
  saveAppVariables: (appId, items) => request('PUT', `/api/v1/applications/${appId}/variables`, { items }),
  assignmentVariables: (instId, appId) =>
    request('GET', `/api/v1/instances/${instId}/assign/${appId}/variables`),
  saveAssignmentVariables: (instId, appId, items) =>
    request('PUT', `/api/v1/instances/${instId}/assign/${appId}/variables`, { items }),

  repositories: () => request('GET', '/api/v1/repositories'),
  createRepository: (repo) => request('POST', '/api/v1/repositories', repo),
  updateRepository: (repo) => request('PUT', `/api/v1/repositories/${repo.id}`, repo),
  deleteRepository: (id) => request('DELETE', `/api/v1/repositories/${id}`),
}

export function stateLabel(state) {
  return { ok: 'Synced', drift: 'Drift', error: 'Error', unknown: 'Unknown' }[state] || state
}
// Projects are mandatory in the UI (like ArgoCD). The implicit global/default
// project is stored as "" by the API, so the forms use a non-empty sentinel.
export const DEFAULT_PROJECT = '__default__'
export const projectForUi = (p) => p || DEFAULT_PROJECT
export const projectForApi = (p) => (p === DEFAULT_PROJECT ? '' : p)
export const projectLabel = (p) => p || 'default (global)'
