import { NavLink } from 'react-router-dom'
import { Gauge, Globe2 } from 'lucide-react'

export default function FleetTabs() {
  const linkClass = ({ isActive }) => 'seg-btn' + (isActive ? ' active' : '')
  return (
    <div className="seg fleet-tabs" role="group" aria-label="Fleet view">
      <NavLink to="/" end className={linkClass}>
        <Gauge size={14} /> Overview
      </NavLink>
      <NavLink to="/map" className={linkClass}>
        <Globe2 size={14} /> World map
      </NavLink>
    </div>
  )
}
