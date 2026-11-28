import { useEffect, useMemo, useState } from 'react'
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'

function pageWindow(page, pages) {
  const wanted = new Set([0, pages - 1, page - 2, page - 1, page, page + 1, page + 2])
  return [...wanted].filter((value) => value >= 0 && value < pages).sort((a, b) => a - b)
}

export default function Pager({
  total,
  limit,
  offset,
  onPage,
  onLimit,
  pageSizes = [25, 50, 100, 200],
  label = 'items',
}) {
  const pages = Math.max(1, Math.ceil(total / limit))
  const page = Math.min(pages - 1, Math.floor(offset / limit))
  const from = total === 0 ? 0 : Math.min(offset + 1, total)
  const to = Math.min(offset + limit, total)
  const [jump, setJump] = useState(String(page + 1))
  const windowPages = useMemo(() => pageWindow(page, pages), [page, pages])

  useEffect(() => { setJump(String(page + 1)) }, [page])

  if (!total) return null

  const go = (next) => onPage(Math.max(0, Math.min(pages - 1, next)) * limit)
  const submitJump = (event) => {
    event.preventDefault()
    const next = Number(jump)
    if (!Number.isFinite(next)) return
    go(Math.round(next) - 1)
  }

  return (
    <div className="pager">
      <span className="pager-summary muted">
        {from.toLocaleString()}–{to.toLocaleString()} of {total.toLocaleString()} {label}
      </span>
      <div className="pager-controls">
        {onLimit && (
          <label className="pager-size">
            <span>Rows</span>
            <select value={limit} onChange={(event) => onLimit(Number(event.target.value))} aria-label="Rows per page">
              {pageSizes.map((size) => <option key={size} value={size}>{size}</option>)}
            </select>
          </label>
        )}
        <div className="pager-btns" role="group" aria-label="Pagination">
          <button type="button" className="small icon-btn" disabled={page === 0} onClick={() => go(0)} title="First page" aria-label="First page">
            <ChevronsLeft size={14} />
          </button>
          <button type="button" className="small icon-btn" disabled={page === 0} onClick={() => go(page - 1)} title="Previous page" aria-label="Previous page">
            <ChevronLeft size={14} />
          </button>
          {windowPages.map((value, index) => {
            const previous = windowPages[index - 1]
            return (
              <span className="pager-page-wrap" key={value}>
                {previous != null && value - previous > 1 && <span className="pager-gap">…</span>}
                <button
                  type="button"
                  className={`small pager-page${value === page ? ' active' : ''}`}
                  onClick={() => go(value)}
                  aria-label={`Page ${value + 1}`}
                  aria-current={value === page ? 'page' : undefined}
                >
                  {value + 1}
                </button>
              </span>
            )
          })}
          <button type="button" className="small icon-btn" disabled={page >= pages - 1} onClick={() => go(page + 1)} title="Next page" aria-label="Next page">
            <ChevronRight size={14} />
          </button>
          <button type="button" className="small icon-btn" disabled={page >= pages - 1} onClick={() => go(pages - 1)} title="Last page" aria-label="Last page">
            <ChevronsRight size={14} />
          </button>
        </div>
        {pages > 1 && (
          <form className="pager-jump" onSubmit={submitJump}>
            <span>Page</span>
            <input
              type="number"
              min="1"
              max={pages}
              value={jump}
              onChange={(event) => setJump(event.target.value)}
              aria-label="Jump to page"
            />
            <span>/ {pages.toLocaleString()}</span>
          </form>
        )}
      </div>
    </div>
  )
}
