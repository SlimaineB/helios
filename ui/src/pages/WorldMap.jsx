import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import { Link } from 'react-router-dom'
import {
  Globe2, Search, ZoomIn, ZoomOut, Maximize2, LocateFixed,
  RefreshCw, Server, PowerOff, X, CircleDot,
} from 'lucide-react'
import { api } from '../api/client.js'
import { toast } from '../components/Toast.jsx'
import { SkeletonCard } from '../components/Skeleton.jsx'
import world from '../assets/world-land.json'

const W = 1000
const H = 500

const project = ([lon, lat]) => [((lon + 180) / 360) * W, ((90 - lat) / 180) * H]

function ringToPath(ring) {
  if (!ring?.length) return ''
  let d = ''
  for (let i = 0; i < ring.length; i++) {
    const [x, y] = project(ring[i])
    d += `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`
  }
  return `${d}Z`
}

function geomToPath(g) {
  if (!g) return ''
  if (g.type === 'Polygon') return g.coordinates.map(ringToPath).join(' ')
  if (g.type === 'MultiPolygon') return g.coordinates.flatMap((p) => p.map(ringToPath)).join(' ')
  return ''
}

const LAND_PATHS = world.features.map((f) => geomToPath(f.geometry)).filter(Boolean)

const GRATICULE = (() => {
  const lines = []
  for (let lon = -180; lon <= 180; lon += 30) {
    const [x1, y1] = project([lon, -85])
    const [x2, y2] = project([lon, 85])
    lines.push(`M${x1} ${y1}L${x2} ${y2}`)
  }
  for (let lat = -60; lat <= 60; lat += 30) {
    const [x1, y1] = project([-180, lat])
    const [x2, y2] = project([180, lat])
    lines.push(`M${x1} ${y1}L${x2} ${y2}`)
  }
  return lines.join(' ')
})()

// Region hints derived from host names/projects (e.g. prod-eu-fin-01). Country
// tokens win over coarse region tokens when both are present.
const GEO = {
  eu: { label: 'Europe', lon: 8.68, lat: 50.11, rank: 1 },
  us: { label: 'United States', lon: -77.49, lat: 39.04, rank: 1 },
  ap: { label: 'Asia-Pacific', lon: 103.82, lat: 1.35, rank: 1 },
  af: { label: 'Africa', lon: 28.05, lat: -26.2, rank: 1 },
  me: { label: 'Middle East', lon: 55.27, lat: 25.2, rank: 1 },
  sa: { label: 'South America', lon: -46.63, lat: -23.55, rank: 1 },
  de: { label: 'Germany', lon: 8.68, lat: 50.11, rank: 2 },
  fr: { label: 'France', lon: 2.35, lat: 48.85, rank: 2 },
  uk: { label: 'United Kingdom', lon: -0.13, lat: 51.51, rank: 2 },
  gb: { label: 'United Kingdom', lon: -0.13, lat: 51.51, rank: 2 },
  nl: { label: 'Netherlands', lon: 4.9, lat: 52.37, rank: 2 },
  es: { label: 'Spain', lon: -3.7, lat: 40.42, rank: 2 },
  it: { label: 'Italy', lon: 9.19, lat: 45.46, rank: 2 },
  ie: { label: 'Ireland', lon: -6.26, lat: 53.35, rank: 2 },
  se: { label: 'Sweden', lon: 18.06, lat: 59.33, rank: 2 },
  pl: { label: 'Poland', lon: 21.01, lat: 52.23, rank: 2 },
  ch: { label: 'Switzerland', lon: 8.54, lat: 47.37, rank: 2 },
  at: { label: 'Austria', lon: 16.37, lat: 48.21, rank: 2 },
  be: { label: 'Belgium', lon: 4.35, lat: 50.85, rank: 2 },
  pt: { label: 'Portugal', lon: -9.14, lat: 38.72, rank: 2 },
  no: { label: 'Norway', lon: 10.75, lat: 59.91, rank: 2 },
  fi: { label: 'Finland', lon: 24.94, lat: 60.17, rank: 2 },
  dk: { label: 'Denmark', lon: 12.57, lat: 55.68, rank: 2 },
  ca: { label: 'Canada', lon: -79.38, lat: 43.65, rank: 2 },
  br: { label: 'Brazil', lon: -46.63, lat: -23.55, rank: 2 },
  mx: { label: 'Mexico', lon: -99.13, lat: 19.43, rank: 2 },
  sg: { label: 'Singapore', lon: 103.82, lat: 1.35, rank: 2 },
  jp: { label: 'Japan', lon: 139.69, lat: 35.69, rank: 2 },
  in: { label: 'India', lon: 72.88, lat: 19.08, rank: 2 },
  cn: { label: 'China', lon: 121.47, lat: 31.23, rank: 2 },
  hk: { label: 'Hong Kong', lon: 114.17, lat: 22.32, rank: 2 },
  kr: { label: 'South Korea', lon: 126.98, lat: 37.57, rank: 2 },
  au: { label: 'Australia', lon: 151.21, lat: -33.87, rank: 2 },
  nz: { label: 'New Zealand', lon: 174.76, lat: -36.85, rank: 2 },
  ae: { label: 'United Arab Emirates', lon: 55.27, lat: 25.2, rank: 2 },
  il: { label: 'Israel', lon: 34.78, lat: 32.08, rank: 2 },
  za: { label: 'South Africa', lon: 28.05, lat: -26.2, rank: 2 },
  ng: { label: 'Nigeria', lon: 3.38, lat: 6.52, rank: 2 },
  ke: { label: 'Kenya', lon: 36.82, lat: -1.29, rank: 2 },
  eg: { label: 'Egypt', lon: 31.24, lat: 30.04, rank: 2 },
}

function detectLocation(inst) {
  const hay = `${inst.name || ''} ${inst.project || ''}`
  const tokens = hay.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
  let best = null
  for (const t of tokens) {
    const g = GEO[t]
    if (g && (!best || g.rank >= best.rank)) best = g
  }
  return best
}

const hostState = (inst) => {
  const sts = (inst.apps || []).map((a) => a.latest_check?.status).filter(Boolean)
  if (sts.includes('failed')) return 'error'
  if (sts.includes('drift')) return 'drift'
  if (sts.includes('ok')) return 'ok'
  return 'unknown'
}

const STATE_LABEL = { ok: 'Healthy', drift: 'Drift', error: 'Failed', unknown: 'No check' }
const STATE_ORDER = { error: 0, drift: 1, unknown: 2, ok: 3 }

function layoutMarkers(instances) {
  const groups = new Map()
  const located = []
  const unknown = []
  for (const inst of instances) {
    const loc = detectLocation(inst)
    if (!loc) { unknown.push(inst); continue }
    if (!groups.has(loc.label)) groups.set(loc.label, [])
    groups.get(loc.label).push({ inst, loc })
  }
  for (const list of groups.values()) {
    list.forEach((item, i) => {
      let { lon, lat } = item.loc
      if (list.length > 1) {
        const ang = i * 2.399963
        const r = 0.85 * Math.sqrt(i + 1)
        lat += r * Math.sin(ang)
        lon += (r * Math.cos(ang)) / Math.max(0.3, Math.cos((lat * Math.PI) / 180))
      }
      located.push({ inst: item.inst, loc: item.loc, lon, lat })
    })
  }
  return { located, unknown }
}

export default function WorldMap() {
  const [instances, setInstances] = useState(null)
  const [loading, setLoading] = useState(true)
  const [appFilter, setAppFilter] = useState('')
  const [stateFilter, setStateFilter] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(null)
  const [hover, setHover] = useState(null) // { inst, sx, sy }
  const [view, setView] = useState({ k: 1, x: 0, y: 0 })
  const svgRef = useRef(null)
  const drag = useRef(null)

  const load = useCallback(async () => {
    try {
      const all = []
      let offset = 0
      let total = Infinity
      while (all.length < total && all.length < 5000) {
        const d = await api.instances(`?limit=500&offset=${offset}`)
        const items = Array.isArray(d.items) ? d.items : []
        if (!items.length) break
        all.push(...items)
        total = Number.isFinite(Number(d.total)) ? Number(d.total) : all.length
        offset += items.length
      }
      setInstances(all)
    } catch (e) {
      toast.error(e.message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])
  useEffect(() => {
    const t = setInterval(() => { if (!document.hidden) load() }, 15000)
    return () => clearInterval(t)
  }, [load])

  // Wheel zoom must be non-passive to call preventDefault (and stop page scroll).
  useEffect(() => {
    const el = svgRef.current
    if (!el) return
    const handler = (e) => {
      e.preventDefault()
      const rect = el.getBoundingClientRect()
      const px = ((e.clientX - rect.left) / rect.width) * W
      const py = ((e.clientY - rect.top) / rect.height) * H
      setView((v) => {
        const k = Math.min(8, Math.max(1, v.k * (e.deltaY < 0 ? 1.2 : 1 / 1.2)))
        const nx = px - (px - v.x) * (k / v.k)
        const ny = py - (py - v.y) * (k / v.k)
        return clampView({ k, x: nx, y: ny })
      })
    }
    el.addEventListener('wheel', handler, { passive: false })
    return () => el.removeEventListener('wheel', handler)
  }, [])

  const { located, unknown } = useMemo(
    () => layoutMarkers(instances || []),
    [instances]
  )

  const appOptions = useMemo(() => {
    const map = new Map()
    for (const inst of instances || []) {
      for (const a of inst.apps || []) map.set(a.application.id, a.application.name)
    }
    return [...map.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [instances])

  const q = query.trim().toLowerCase()
  const matches = useCallback((inst, loc) => {
    if (appFilter && !(inst.apps || []).some((a) => String(a.application.id) === appFilter)) return false
    if (stateFilter && hostState(inst) !== stateFilter) return false
    if (q) {
      const hay = [inst.name, inst.id, inst.project, loc?.label, ...(inst.apps || []).map((a) => a.application.name)]
        .filter(Boolean).join(' ').toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  }, [appFilter, stateFilter, q])

  const visible = located.filter((m) => matches(m.inst, m.loc))
  const visibleUnknown = unknown.filter((inst) => matches(inst, null))
  const stats = useMemo(() => {
    const s = { ok: 0, drift: 0, error: 0, unknown: 0 }
    for (const m of visible) s[hostState(m.inst)]++
    return s
  }, [visible])

  const clampView = (v) => {
    const k = Math.min(8, Math.max(1, v.k))
    const minX = W - W * k
    const minY = H - H * k
    return { k, x: Math.min(0, Math.max(minX, v.x)), y: Math.min(0, Math.max(minY, v.y)) }
  }

  const zoomBy = (factor, px = W / 2, py = H / 2) => {
    setView((v) => {
      const k = Math.min(8, Math.max(1, v.k * factor))
      return clampView({ k, x: px - (px - v.x) * (k / v.k), y: py - (py - v.y) * (k / v.k) })
    })
  }

  const onPointerDown = (e) => {
    drag.current = { x: e.clientX, y: e.clientY, vx: view.x, vy: view.y }
    e.currentTarget.setPointerCapture?.(e.pointerId)
  }
  const onPointerMove = (e) => {
    if (!drag.current) return
    const rect = svgRef.current.getBoundingClientRect()
    const dx = ((e.clientX - drag.current.x) / rect.width) * W
    const dy = ((e.clientY - drag.current.y) / rect.height) * H
    setView((v) => clampView({ k: v.k, x: drag.current.vx + dx, y: drag.current.vy + dy }))
  }
  const onPointerUp = () => { drag.current = null }

  const focusInstance = (inst) => {
    const loc = detectLocation(inst)
    setSelected(inst.id)
    if (!loc) return
    const [cx, cy] = project([loc.lon, loc.lat])
    setView((v) => clampView({ k: Math.max(v.k, 2.4), x: W / 2 - cx * Math.max(v.k, 2.4), y: H / 2 - cy * Math.max(v.k, 2.4) }))
  }

  const reset = () => { setView({ k: 1, x: 0, y: 0 }); setSelected(null) }

  if (loading && !instances) {
    return (
      <section>
        <h1>World map</h1>
        <SkeletonCard />
      </section>
    )
  }

  const scale = view.k

  return (
    <section className="world-map-page">
      <div className="wm-head">
        <div>
          <h1><Globe2 size={22} /> World map</h1>
          <p className="sub muted">Every located host, coloured by drift state. Filter by application or host, zoom and pan.</p>
        </div>
        <div className="wm-stats">
          <span className="wm-stat ok"><CircleDot size={13} /> {stats.ok} healthy</span>
          <span className="wm-stat drift"><CircleDot size={13} /> {stats.drift} drift</span>
          <span className="wm-stat error"><CircleDot size={13} /> {stats.error} failed</span>
          <span className="wm-stat unknown"><CircleDot size={13} /> {stats.unknown} unknown</span>
          <button type="button" className="btn ghost small" onClick={load} title="Refresh"><RefreshCw size={14} /> Refresh</button>
        </div>
      </div>

      <div className="wm-toolbar">
        <div className="wm-field">
          <Search size={14} />
          <input
            type="search"
            placeholder="Filter host, id, project, app or region…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {query && <button type="button" className="wm-clear" onClick={() => setQuery('')} aria-label="Clear search"><X size={13} /></button>}
        </div>
        <select value={appFilter} onChange={(e) => setAppFilter(e.target.value)} aria-label="Application filter">
          <option value="">All applications</option>
          {appOptions.map(([id, name]) => <option key={id} value={String(id)}>{name}</option>)}
        </select>
        <select value={stateFilter} onChange={(e) => setStateFilter(e.target.value)} aria-label="State filter">
          <option value="">All states</option>
          <option value="ok">Healthy</option>
          <option value="drift">Drift</option>
          <option value="error">Failed</option>
          <option value="unknown">No check</option>
        </select>
        {(query || appFilter || stateFilter) && (
          <button type="button" className="btn ghost small" onClick={() => { setQuery(''); setAppFilter(''); setStateFilter('') }}>Clear</button>
        )}
        <span className="wm-count">{visible.length + visibleUnknown.length} host{visible.length + visibleUnknown.length === 1 ? '' : 's'}</span>
      </div>

      <div className="wm-layout">
        <div className="wm-canvas-wrap">
          <svg
            ref={svgRef}
            className="wm-svg"
            viewBox={`0 0 ${W} ${H}`}
            preserveAspectRatio="xMidYMid meet"
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerLeave={onPointerUp}
            role="img"
            aria-label="World map of hosts"
          >
            <defs>
              <linearGradient id="wm-ocean" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#0b1f3a" />
                <stop offset="55%" stopColor="#0f2c52" />
                <stop offset="100%" stopColor="#0a1930" />
              </linearGradient>
              <radialGradient id="wm-glow" cx="50%" cy="40%" r="65%">
                <stop offset="0%" stopColor="#1e4f8a" stopOpacity="0.55" />
                <stop offset="100%" stopColor="#0b1f3a" stopOpacity="0" />
              </radialGradient>
            </defs>
            <rect width={W} height={H} fill="url(#wm-ocean)" />
            <rect width={W} height={H} fill="url(#wm-glow)" />
            <g transform={`translate(${view.x} ${view.y}) scale(${scale})`}>
              <path d={GRATICULE} className="wm-graticule" vectorEffect="non-scaling-stroke" />
              {LAND_PATHS.map((d, i) => (
                <path key={i} d={d} className="wm-land" vectorEffect="non-scaling-stroke" />
              ))}
            </g>

            <g className="wm-markers">
              {visible.map((m) => {
                const [cx, cy] = project([m.lon, m.lat])
                const sx = cx * scale + view.x
                const sy = cy * scale + view.y
                const st = hostState(m.inst)
                const isSel = selected === m.inst.id
                return (
                  <g
                    key={m.inst.id}
                    className={`wm-marker ${st}${isSel ? ' selected' : ''}`}
                    transform={`translate(${sx} ${sy})`}
                    onMouseEnter={() => setHover({ inst: m.inst, sx, sy, loc: m.loc })}
                    onMouseLeave={() => setHover(null)}
                    onClick={(e) => { e.stopPropagation(); focusInstance(m.inst) }}
                  >
                    <circle className="wm-halo" r={15} />
                    <circle className="wm-dot" r={7} />
                    {m.inst.autoheal === false && <circle className="wm-offring" r={11} />}
                  </g>
                )
              })}
            </g>
          </svg>

          <div className="wm-zoom">
            <button type="button" onClick={() => zoomBy(1.4)} title="Zoom in" aria-label="Zoom in"><ZoomIn size={16} /></button>
            <button type="button" onClick={() => zoomBy(1 / 1.4)} title="Zoom out" aria-label="Zoom out"><ZoomOut size={16} /></button>
            <button type="button" onClick={reset} title="Reset view" aria-label="Reset view"><Maximize2 size={16} /></button>
          </div>

          <div className="wm-legend">
            <span><i className="dot ok" /> Healthy</span>
            <span><i className="dot drift" /> Drift</span>
            <span><i className="dot error" /> Failed</span>
            <span><i className="dot unknown" /> No check</span>
            <span className="wm-legend-off"><i className="ring" /> auto-apply off</span>
          </div>

          {hover && (
            <div
              className="wm-tooltip"
              style={{ left: `${(hover.sx / W) * 100}%`, top: `${(hover.sy / H) * 100}%` }}
            >
              <strong>{hover.inst.name}</strong>
              <span className={`wm-tooltip-state ${hostState(hover.inst)}`}>{STATE_LABEL[hostState(hover.inst)]}</span>
              <span className="wm-tooltip-row">{hover.loc.label} · {hover.inst.project || 'global'}</span>
              <span className="wm-tooltip-row">{(hover.inst.apps || []).length} app{(hover.inst.apps || []).length === 1 ? '' : 's'}{hover.inst.autoheal === false ? ' · auto-apply off' : ''}</span>
            </div>
          )}

          {visible.length === 0 && (
            <div className="wm-empty muted">
              <LocateFixed size={18} /> No located host matches these filters.
            </div>
          )}
        </div>

        <aside className="wm-side">
          <div className="wm-side-head">
            <strong>Hosts</strong>
            <span className="muted">{visible.length + visibleUnknown.length}</span>
          </div>
          <ul className="wm-list">
            {[...visible].sort((a, b) => STATE_ORDER[hostState(a.inst)] - STATE_ORDER[hostState(b.inst)]).map((m) => {
              const st = hostState(m.inst)
              return (
                <li key={m.inst.id}>
                  <button
                    type="button"
                    className={`wm-item${selected === m.inst.id ? ' selected' : ''}`}
                    onClick={() => focusInstance(m.inst)}
                  >
                    <span className={`dot ${st}`} />
                    <span className="wm-item-main">
                      <span className="wm-item-name">{m.inst.name}</span>
                      <span className="wm-item-sub">{m.loc.label} · {m.inst.project || 'global'}</span>
                    </span>
                    <span className={`wm-badge ${st}`}>{STATE_LABEL[st]}</span>
                    {m.inst.autoheal === false && <PowerOff size={13} className="wm-off" title="Host auto-apply off" />}
                  </button>
                </li>
              )
            })}
            {visibleUnknown.map((inst) => (
              <li key={inst.id}>
                <Link to={`/instances/${inst.id}`} className="wm-item unknown-loc">
                  <span className={`dot ${hostState(inst)}`} />
                  <span className="wm-item-main">
                    <span className="wm-item-name">{inst.name}</span>
                    <span className="wm-item-sub">Location unknown · {inst.project || 'global'}</span>
                  </span>
                  <span className="wm-badge unknown">Unplaced</span>
                </Link>
              </li>
            ))}
            {visible.length + visibleUnknown.length === 0 && (
              <li className="wm-list-empty muted">No hosts match.</li>
            )}
          </ul>
          {unknown.length > 0 && (
            <p className="wm-hint">
              <Server size={13} /> {unknown.length} host{unknown.length === 1 ? '' : 's'} have no region in their name.
              Add a token like <code>eu</code>, <code>us</code>, <code>de</code>… to place them.
            </p>
          )}
        </aside>
      </div>
    </section>
  )
}
