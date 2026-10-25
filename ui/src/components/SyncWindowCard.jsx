import { Link } from 'react-router-dom'
import { Ban, CircleCheck, Clock } from 'lucide-react'
import { useAuth } from '../App.jsx'

const ORDER = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']
const LABEL = { mon: 'Mon', tue: 'Tue', wed: 'Wed', thu: 'Thu', fri: 'Fri', sat: 'Sat', sun: 'Sun' }

const toHM = (m) => {
  const n = Number.isFinite(m) ? m : 0
  return `${String(Math.floor(n / 60)).padStart(2, '0')}:${String(n % 60).padStart(2, '0')}`
}

export function formatDays(days) {
  const list = String(days || '').split(',').filter(Boolean)
  if (!list.length) return 'Every day'
  const idx = [...new Set(list)].map((d) => ORDER.indexOf(d)).filter((i) => i >= 0).sort((a, b) => a - b)
  if (idx.length === 7) return 'Every day'
  const groups = []
  let start = idx[0]
  let prev = idx[0]
  for (let i = 1; i <= idx.length; i++) {
    const cur = idx[i]
    if (cur === prev + 1) { prev = cur; continue }
    groups.push(start === prev ? LABEL[ORDER[start]] : `${LABEL[ORDER[start]]}–${LABEL[ORDER[prev]]}`)
    start = cur
    prev = cur
  }
  return groups.join(', ')
}

function slotsNow(tz) {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: tz || 'UTC',
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }).formatToParts(new Date())
    const get = (t) => parts.find((p) => p.type === t)?.value
    const wd = (get('weekday') || '').slice(0, 3).toLowerCase()
    const h = Number(get('hour')) % 24
    const m = Number(get('minute'))
    return { wd, minutes: h * 60 + m }
  } catch {
    return slotsNow('UTC')
  }
}

export function windowIsActive(w) {
  if (!w.enabled) return false
  const { wd, minutes } = slotsNow(w.timezone)
  const days = String(w.days || '').split(',').filter(Boolean)
  if (days.length && !days.includes(wd)) return false
  const s = w.start_min
  const e = w.end_min
  if (s === e) return true
  return s < e ? minutes >= s && minutes < e : minutes >= s || minutes < e
}

export default function SyncWindowCard({ window: w, appName, onEdit, onDelete }) {
  const { can } = useAuth()
  const active = windowIsActive(w)
  const blocksNow = w.enabled && (w.kind === 'deny' ? active : !active)
  const isDeny = w.kind === 'deny'
  const schedule = `${formatDays(w.days)} · ${toHM(w.start_min)}–${toHM(w.end_min)} ${w.timezone || 'UTC'}`

  let chip
  if (!w.enabled) chip = { state: 'unknown', label: 'Disabled', Icon: Clock }
  else if (blocksNow) chip = { state: 'failed', label: 'Syncs blocked', Icon: Ban }
  else chip = { state: 'ok', label: 'Syncs allowed', Icon: CircleCheck }
  const ChipIcon = chip.Icon

  return (
    <article className={`window-card window-${chip.state}`}>
      <header className="card-head">
        <div className="window-main">
          <div className="cell-title">
            <strong>{w.name}</strong>
            <span className="cell-sub">
              {w.app_id ? (
                <>Application: <Link className="entity-link" to={`/instances?app=${w.app_id}`}>{appName}</Link></>
              ) : 'All applications'}
            </span>
          </div>
        </div>
        <div className="card-actions">
          <span className={`health-chip ${chip.state} small`}>
            <ChipIcon size={13} />
            <span>{chip.label}</span>
          </span>
        </div>
      </header>

      <div className="card-body">
        <div className="window-schedule">
          <Clock size={15} />
          <span className="mono">{schedule}</span>
        </div>
        <p className="window-rule">
          {isDeny
            ? 'Blocks syncs inside this time range.'
            : 'Allows syncs only inside this time range — blocked outside.'}
        </p>
      </div>

      <footer className="card-foot">
        <div className="actions">
          <button className="btn ghost small" disabled={!can('windows', 'update')} onClick={() => onEdit(w)}>Edit</button>
          <button className="btn danger small" disabled={!can('windows', 'delete')} onClick={() => onDelete(w)}>Delete</button>
        </div>
      </footer>
    </article>
  )
}
