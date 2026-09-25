// StatusIcon renders an ArgoCD-like health symbol: a green check when the last
// check would change nothing, red when the state would change (drift), and a
// grey/red glyph for unknown/failed.
export default function StatusIcon({ state, size = 20 }) {
  const kind =
    state === 'ok'
      ? 'ok'
      : state === 'drift'
        ? 'drift'
        : state === 'failed' || state === 'error'
          ? 'fail'
          : 'unknown'

  return (
    <svg className={`status-icon ok-${kind}`} width={size} height={size} viewBox="0 0 16 16" aria-label={state || 'unknown'}>
      {kind === 'ok' && (
        <>
          <circle cx="8" cy="8" r="7" fill="var(--ok)" />
          <path d="M4.2 8.4 6.8 11 11.8 5.6" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" fill="none" />
        </>
      )}
      {kind === 'drift' && (
        <>
          <circle cx="8" cy="8" r="7" fill="var(--drift)" />
          <path d="M5 5.2 2.4 8 5 10.8 M4.6 8h9" stroke="#fff" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" fill="none" />
          <path d="M11 5.2 13.6 8 11 10.8" stroke="#fff" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" fill="none" />
        </>
      )}
      {kind === 'fail' && (
        <>
          <circle cx="8" cy="8" r="7" fill="var(--err)" />
          <path d="M8 4.4v4.2" stroke="#fff" strokeWidth="1.9" strokeLinecap="round" />
          <circle cx="8" cy="11.1" r="0.9" fill="#fff" />
        </>
      )}
      {kind === 'unknown' && (
        <>
          <circle cx="8" cy="8" r="7" fill="none" stroke="var(--unknown)" strokeWidth="1.4" />
          <text x="8" y="11.4" textAnchor="middle" fontSize="9.5" fill="var(--unknown)">
            ?
          </text>
        </>
      )}
    </svg>
  )
}