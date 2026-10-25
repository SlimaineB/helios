export function Skeleton({ className = '', style, width, height = 16, borderRadius = 4 }) {
  return (
    <div
      className={`skeleton ${className}`}
      style={{
        ...style,
        width: width || '100%',
        height,
        borderRadius,
        background: 'linear-gradient(90deg, var(--panel-2) 25%, var(--panel-3) 50%, var(--panel-2) 75%)',
        backgroundSize: '200% 100%',
        animation: 'shimmer 1.4s ease-in-out infinite',
      }}
    />
  )
}

export function SkeletonRow({ columns = 4, className = '' }) {
  return (
    <div className={`skeleton-row ${className}`} style={{ display: 'grid', gridTemplateColumns: `repeat(${columns}, 1fr)`, gap: '12px', padding: '10px 12px' }}>
      {Array.from({ length: columns }).map((_, i) => (
        <Skeleton key={i} height={14} borderRadius={4} style={{ width: i === 0 ? '60%' : '80%' }} />
      ))}
    </div>
  )
}

export function SkeletonTable({ rows = 5, columns = 6, className = '' }) {
  return (
    <div className={`skeleton-table ${className}`}>
      <div className="grid" style={{ overflow: 'hidden' }}>
        <thead>
          <tr style={{ opacity: 0.4 }}>
            {Array.from({ length: columns }).map((_, i) => (
              <th key={i} style={{ padding: '9px 12px' }}>
                <Skeleton height={11} width="70%" borderRadius={3} />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }).map((_, r) => (
            <tr key={r}>
              {Array.from({ length: columns }).map((_, c) => (
                <td key={c} style={{ padding: '9px 12px' }}>
                  <Skeleton height={13} width={c === 0 ? '50%' : '80%'} borderRadius={3} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </div>
    </div>
  )
}

export function SkeletonCard({ className = '' }) {
  return (
    <div className={`card skeleton-card ${className}`} style={{ pointerEvents: 'none' }}>
      <div className="card-head">
        <Skeleton height={20} width="30%" borderRadius={4} />
        <Skeleton height={16} width="20%" borderRadius={999} style={{ marginLeft: 'auto' }} />
      </div>
      <div style={{ marginTop: '12px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
        <Skeleton height={14} width="60%" borderRadius={4} />
        <Skeleton height={14} width="40%" borderRadius={4} />
        <Skeleton height={14} width="80%" borderRadius={4} />
      </div>
    </div>
  )
}

export function SkeletonEmpty({ className = '', lines = 3 }) {
  return (
    <div className={`empty skeleton-empty ${className}`} style={{ padding: '3rem 1rem' }}>
      <Skeleton height={48} width={48} borderRadius={999} style={{ margin: '0 auto 1rem' }} />
      {Array.from({ length: lines }).map((_, i) => (
        <Skeleton key={i} height={14} width={i === 1 ? '50%' : '70%'} borderRadius={4} style={{ margin: '0.5rem auto' }} />
      ))}
    </div>
  )
}