import { Power, PowerOff } from 'lucide-react'

export const hostAutoHealOn = (inst) => inst?.autoheal !== false

// appAutoHealMeta explains the effective auto-apply state of one application on
// one host under AND semantics (host AND application AND override).
export function appAutoHealMeta(inst, app) {
  const hostOn = hostAutoHealOn(inst)
  const appOn = app?.application?.autoheal !== false
  const override = !!app?.autoheal_set
  if (app?.autoheal) {
    return { label: override ? 'on (host override)' : 'on', reason: '', locked: false }
  }
  if (!hostOn) {
    return { label: 'off (host disabled)', reason: 'Host auto-apply is disabled. Enable the host switch first.', locked: true, hostLocked: true }
  }
  if (!appOn) {
    return { label: 'off (app disabled)', reason: 'Application auto-apply is disabled for all hosts. Enable it for the application to unlock this host.', locked: true, appLocked: true }
  }
  return { label: override ? 'off (host override)' : 'off', reason: '', locked: false }
}

// AutoHealChip renders the auto-apply state with an icon and colour so it is
// obvious whether a control is host-level, application-level or an override.
// scope: 'host' | 'app' | 'assignment'
export default function AutoHealChip({ enabled, scope = 'app', reason, small = true, label }) {
  const base =
    scope === 'host' ? 'host auto-apply' : scope === 'assignment' ? 'override' : 'auto-apply'
  const text = label || (enabled ? base : `${base} off`)
  return (
    <span
      className={`autoheal-chip ${enabled ? 'on' : 'off'}${small ? ' small' : ''}`}
      title={reason || text}
    >
      {enabled ? <Power size={11} aria-hidden="true" /> : <PowerOff size={11} aria-hidden="true" />}
      {text}
    </span>
  )
}
