import { HeartPulse, HeartCrack, TriangleAlert, CircleHelp } from 'lucide-react'

const META = {
  ok: { Icon: HeartPulse, text: 'Healthy' },
  drift: { Icon: TriangleAlert, text: 'Drift' },
  failed: { Icon: HeartCrack, text: 'Unhealthy' },
  unknown: { Icon: CircleHelp, text: 'Unassigned' },
}

export default function HealthBadge({ state = 'unknown', label, size = 14, small = false, title }) {
  const meta = META[state] || META.unknown
  const { Icon } = meta
  return (
    <span className={`health-chip ${META[state] ? state : 'unknown'}${small ? ' small' : ''}`} title={title || label || meta.text}>
      <Icon size={size} />
      <span>{label || meta.text}</span>
    </span>
  )
}
