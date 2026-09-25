let basicCreds = null

export function setBasic(user, pass) {
  basicCreds = btoa(`${user}:${pass}`)
}

export function clearBasic() {
  basicCreds = null
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
  createInstance: (name) => request('POST', '/api/v1/instances', { name }),
  deleteInstance: (id) => request('DELETE', `/api/v1/instances/${id}`),
  assign: (id, appId) => request('POST', `/api/v1/instances/${id}/assign`, { app_id: appId }),
  unassign: (id, appId) => request('DELETE', `/api/v1/instances/${id}/assign/${appId}`),
  sync: (id, appId) => request('POST', `/api/v1/instances/${id}/apply`, { app_id: appId }),
  check: (id, appId) => request('POST', `/api/v1/instances/${id}/check`, { app_id: appId }),
  runs: (id, query) => request('GET', `/api/v1/instances/${id}/runs${query || ''}`),
  runLog: (runId) => request('GET', `/api/v1/runs/${runId}/log`),

  setAutoHeal: (id, appId, enabled, inherit) =>
    request('PUT', `/api/v1/instances/${id}/assign/${appId}/autoheal`, { enabled, inherit }),

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