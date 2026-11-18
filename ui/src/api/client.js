let basicCreds = null
const credsKey = 'helios_basic_creds'
const legacyCredsKey = 'anh_basic_creds'

function loadCreds() {
  if (typeof window === 'undefined') return null
  try {
    const stored = sessionStorage.getItem(credsKey)
    if (stored) return stored
    const legacy = localStorage.getItem(legacyCredsKey)
    if (legacy) {
      sessionStorage.setItem(credsKey, legacy)
      localStorage.removeItem(legacyCredsKey)
      return legacy
    }
  } catch {
    /* ignore */
  }
  return null
}

function saveCreds(creds) {
  if (typeof window === 'undefined') return
  try {
    if (creds) sessionStorage.setItem(credsKey, creds)
    else sessionStorage.removeItem(credsKey)
    localStorage.removeItem(legacyCredsKey)
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

  downloads: () => request('GET', '/api/v1/downloads'),
  overview: () => request('GET', '/api/v1/overview'),
  drift: (query) => request('GET', `/api/v1/drift${query || ''}`),
  instances: (query) => request('GET', `/api/v1/instances${query || ''}`),
  instance: (id) => request('GET', `/api/v1/instances/${id}`),
  createInstance: (name, project, executionMode = 'agent', region = '', tags = []) => request('POST', '/api/v1/instances', { name, project, region, tags, execution_mode: executionMode }),
  updateInstance: (id, name, project, region = '', tags = []) => request('PUT', `/api/v1/instances/${id}`, { name, project, region, tags }),
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
  setInstanceAutoHeal: (id, enabled) =>
    request('PUT', `/api/v1/instances/${id}/autoheal`, { enabled }),
  setAppAutoHeal: (id, enabled) =>
    request('PUT', `/api/v1/applications/${id}/autoheal`, { enabled }),

  setRevision: (id, appId, revision) =>
    request('PUT', `/api/v1/instances/${id}/assign/${appId}/revision`, { revision }),

  audit: (query) => request('GET', `/api/v1/audit${query || ''}`),
  settings: () => request('GET', '/api/v1/settings'),
  updateScanning: (scanning) => request('PUT', '/api/v1/settings/scanning', scanning),
  testWebhook: () => request('POST', '/api/v1/settings/webhook/test'),
  bulkCommands: (action, instanceIds, appId) =>
    request('POST', '/api/v1/bulk/commands', { action, instance_ids: instanceIds, app_id: appId || 0 }),

  syncWindows: () => request('GET', '/api/v1/sync-windows'),
  createSyncWindow: (w) => request('POST', '/api/v1/sync-windows', w),
  updateSyncWindow: (id, w) => request('PUT', `/api/v1/sync-windows/${id}`, w),
  deleteSyncWindow: (id) => request('DELETE', `/api/v1/sync-windows/${id}`),

  projects: () => request('GET', '/api/v1/projects'),
  createProject: (p) => request('POST', '/api/v1/projects', p),
  updateProject: (id, p) => request('PUT', `/api/v1/projects/${id}`, p),
  deleteProject: (id) => request('DELETE', `/api/v1/projects/${id}`),

  applications: () => request('GET', '/api/v1/applications'),
  applicationStats: () => request('GET', '/api/v1/application-stats'),
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

  configRepos: () => request('GET', '/api/v1/config-repos'),
  createConfigRepo: (repo) => request('POST', '/api/v1/config-repos', repo),
  updateConfigRepo: (id, repo) => request('PUT', `/api/v1/config-repos/${id}`, repo),
  deleteConfigRepo: (id) => request('DELETE', `/api/v1/config-repos/${id}`),
  syncConfigRepo: (id) => request('POST', `/api/v1/config-repos/${id}/sync`),
}

export function stateLabel(state) {
  return {
    ok: 'Synced',
    drift: 'Drift',
    error: 'Failed',
    offline: 'Offline',
    unknown: 'Unknown',
  }[state] || state
}

// Detect a silent agent. The server sends `online` (and an `offline` state)
// computed from the poll interval the agent reports; fall back to the same
// formula for older servers that do not expose it.
export function instanceOnline(inst, now = Date.now()) {
  if (!inst) return false
  if (typeof inst.online === 'boolean') return inst.online
  if (!inst.last_seen) return false
  const poll = Number(inst.poll_interval) || 0
  const grace = poll > 0
    ? Math.min(Math.max(poll * 3000, 30000), 15 * 60 * 1000)
    : 7 * 60 * 1000
  return now - new Date(inst.last_seen).getTime() <= grace
}
// Projects are mandatory in the UI (like ArgoCD). The implicit global/default
// project is stored as "" by the API, so the forms use a non-empty sentinel.
export const DEFAULT_PROJECT = '__default__'
export const projectForUi = (p) => p || DEFAULT_PROJECT
export const projectForApi = (p) => (p === DEFAULT_PROJECT ? '' : p)
export const projectLabel = (p) => p || 'default (global)'

// openEventStream reads the SSE endpoint with fetch() because EventSource
// cannot attach the Basic authorization header used by the local admin mode.
export function openEventStream(onEvent, onStatus) {
  let closed = false
  const ctrl = new AbortController()

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

  async function connect() {
    while (!closed) {
      try {
        const headers = { Accept: 'text/event-stream' }
        if (basicCreds) headers.Authorization = `Basic ${basicCreds}`
        const res = await fetch('/api/v1/events', { headers, signal: ctrl.signal })
        if (!res.ok || !res.body) throw new Error(`event stream failed: ${res.status}`)
        onStatus?.('open')
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        while (!closed) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          let idx
          while ((idx = buffer.indexOf('\n\n')) >= 0) {
            const block = buffer.slice(0, idx)
            buffer = buffer.slice(idx + 2)
            const data = block
              .split('\n')
              .filter((line) => line.startsWith('data:'))
              .map((line) => line.slice(5).trimStart())
              .join('\n')
            if (!data) continue
            try {
              onEvent?.(JSON.parse(data))
            } catch {
              /* ignore malformed event */
            }
          }
        }
        if (!closed) throw new Error('event stream closed')
      } catch (err) {
        if (closed || ctrl.signal.aborted) return
        onStatus?.('reconnecting', err)
        await sleep(3000)
      }
    }
  }

  connect()
  return () => {
    closed = true
    ctrl.abort()
  }
}
