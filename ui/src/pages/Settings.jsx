import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Activity, Database, GitBranch, KeyRound, RefreshCw, Rocket, Send,
  Settings2, ShieldCheck, TimerReset, Webhook,
} from 'lucide-react'
import { api } from '../api/client.js'
import { useAuth } from '../App.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import { toast } from '../components/Toast.jsx'

function duration(seconds) {
  const s = Number(seconds) || 0
  if (!s) return 'disabled'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d) return `${d} day${d === 1 ? '' : 's'}${h ? ` ${h}h` : ''}`
  if (h) return `${h}h${m ? ` ${m}m` : ''}`
  return `${m}m`
}

function yesNo(value) {
  return value ? <span className="state-badge enabled">enabled</span> : <span className="state-badge disabled">off</span>
}

function SettingRow({ label, value, hint, children }) {
  return (
    <div className="set-row">
      <div className="set-label">
        <span>{label}</span>
        {hint && <span className="sub">{hint}</span>}
      </div>
      <div className="set-value">{children ?? value}</div>
    </div>
  )
}

export default function Settings() {
  const { can } = useAuth()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [testing, setTesting] = useState(false)
  const loadedRef = useRef(false)

  const load = useCallback(async () => {
    if (!loadedRef.current) setLoading(true)
    try {
      setData(await api.settings())
    } catch (e) {
      if (!loadedRef.current) toast.error(e.message)
    } finally {
      loadedRef.current = true
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const test = async () => {
    setTesting(true)
    try {
      const res = await api.testWebhook()
      toast.success(res.message || 'Webhook test delivered')
    } catch (e) {
      toast.error(e.message)
    } finally {
      setTesting(false)
    }
  }

  if (loading && !data) {
    return (
      <section>
        <h1>Settings</h1>
        <div className="skeleton-grid">
          {[1, 2, 3].map((i) => <SkeletonCard key={i} />)}
        </div>
      </section>
    )
  }

  if (!data) {
    return (
      <section>
        <h1>Settings</h1>
        <div className="empty"><p className="muted">Settings are unavailable.</p></div>
      </section>
    )
  }

  const auth = data.auth || {}
  const security = data.security || {}
  const webhook = data.webhook || {}
  const retention = data.retention || {}
  const features = data.features || {}
  const anyAuth = auth.basic_admin || auth.oidc

  return (
    <section className="settings-page">
      <div className="ov-head">
        <div>
          <h1><Settings2 size={24} /> Settings</h1>
          <p className="page-intro">Effective hub configuration. Values come from environment variables or server flags.</p>
        </div>
        <div className="ov-actions">
          <button type="button" className="btn ghost small" onClick={load} title="Reload settings">
            <RefreshCw size={14} /> Reload
          </button>
        </div>
      </div>

      <div className="settings-grid">
        <div className="card">
          <div className="card-head">
            <div>
              <h3><ShieldCheck size={16} /> Authentication &amp; security</h3>
              <span className="sub">Operator access and secret storage</span>
            </div>
            <span className={`state-badge ${anyAuth ? 'enabled' : 'failed'}`}>{anyAuth ? 'protected' : 'open'}</span>
          </div>
          <div className="card-body settings-body">
            <SettingRow label="Local admin login" hint="HELIOS_OPERATOR_USER / PASSWORD" value={yesNo(auth.basic_admin)} />
            <SettingRow label="OIDC / SSO" hint="HELIOS_OIDC_ISSUER + client credentials" value={yesNo(auth.oidc)} />
            <SettingRow label="TLS" hint="HELIOS_TLS_CERT / HELIOS_TLS_KEY" value={yesNo(security.tls)} />
            <SettingRow label="Secret encryption key" hint="HELIOS_SECRET_KEY" value={yesNo(security.secret_key_set)} />
            {!security.secret_key_set && (
              <div className="settings-note warn">
                <KeyRound size={15} /> Repository credentials and secret variables are stored in plaintext until a stable key is configured.
              </div>
            )}
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <div>
              <h3><Webhook size={16} /> Webhook</h3>
              <span className="sub">Failed runs, stuck assignments and blocked syncs</span>
            </div>
            <span className={`state-badge ${webhook.configured ? 'enabled' : 'disabled'}`}>{webhook.configured ? 'configured' : 'not configured'}</span>
          </div>
          <div className="card-body settings-body">
            <SettingRow label="Endpoint" value={webhook.endpoint || <span className="muted">—</span>} />
            <SettingRow label="HMAC signature" hint="X-Helios-Signature" value={yesNo(webhook.signed)} />
            <SettingRow label="Delivery timeout" value={webhook.timeout_seconds ? `${webhook.timeout_seconds}s` : '—'} />
            {webhook.configured ? (
              <div className="form-actions settings-actions">
                <button type="button" className="btn small" onClick={test} disabled={testing || !can('settings', 'update')}>
                  <Send size={14} /> {testing ? 'Sending…' : 'Send test event'}
                </button>
              </div>
            ) : (
              <div className="settings-note">
                Set <code>HELIOS_WEBHOOK_URL</code> (and optionally <code>HELIOS_WEBHOOK_SECRET</code>, <code>HELIOS_WEBHOOK_TIMEOUT</code>) and restart the server.
              </div>
            )}
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <div>
              <h3><TimerReset size={16} /> Retention</h3>
              <span className="sub">Automatic history pruning</span>
            </div>
            <span className="state-badge enabled">{retention.last_run ? 'running' : 'pending'}</span>
          </div>
          <div className="card-body settings-body">
            <SettingRow label="Run history" hint="HELIOS_RETENTION_RUNS" value={duration(retention.runs_seconds)} />
            <SettingRow label="Completed commands" hint="HELIOS_RETENTION_COMMANDS" value={duration(retention.commands_seconds)} />
            <SettingRow label="Audit log" hint="HELIOS_RETENTION_AUDIT" value={duration(retention.audit_seconds)} />
            <SettingRow label="Prune interval" hint="HELIOS_RETENTION_INTERVAL" value={duration(retention.interval_seconds)} />
            <SettingRow label="Last pass" value={retention.last_run ? new Date(retention.last_run).toLocaleString() : 'not run yet'} />
            <div className="set-counts">
              <span><strong>{retention.deleted_runs || 0}</strong> runs</span>
              <span><strong>{retention.deleted_commands || 0}</strong> commands</span>
              <span><strong>{retention.deleted_audit || 0}</strong> audit entries</span>
            </div>
            {retention.error && <div className="settings-note warn">{retention.error}</div>}
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <div>
              <h3><Database size={16} /> Runtime</h3>
              <span className="sub">Build, storage and optional features</span>
            </div>
            <span className="state-badge enabled">{data.version || 'dev'}</span>
          </div>
          <div className="card-body settings-body">
            <SettingRow label="Base URL" value={<span className="mono">{data.base_url || '—'}</span>} />
            <SettingRow label="Store" value={<span className="mono">{data.store || '—'}</span>} />
            <SettingRow label="Repository snapshot cache" value={yesNo(features.repo_cache)} />
            <SettingRow label="Agentless runner" hint="HELIOS_RUNNER_TOKEN" value={yesNo(features.runner)} />
            <SettingRow label="GitOps controller" value={yesNo(features.gitops)} />
            <SettingRow label="Tunnel gateway" hint="HELIOS_TUNNEL_GATEWAY" value={<span className="mono">{features.tunnel_gateway || '—'}</span>} />
          </div>
        </div>
      </div>

      <div className="settings-foot">
        <Activity size={15} />
        <span>Configuration is read-only in the UI. Change the environment or server flags, then restart the hub.</span>
        <span className="spacer" />
        <GitBranch size={14} /> <code>helm/helios-server/values.yaml</code>
        <Rocket size={14} /> <code>HELIOS_VERSION</code>
      </div>
    </section>
  )
}
