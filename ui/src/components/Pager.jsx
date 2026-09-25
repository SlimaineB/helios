export default function Pager({ total, limit, offset, onPage }) {
  if (!total) return null
  const page = Math.floor(offset / limit)
  const pages = Math.max(1, Math.ceil(total / limit))
  const from = offset + 1
  const to = Math.min(offset + limit, total)
  return (
    <div className="pager">
      <span className="muted">
        {from}–{to} of {total}
      </span>
      <span className="pager-btns">
        <button className="small" disabled={page === 0} onClick={() => onPage(offset - limit)}>
          ‹ Prev
        </button>
        <button className="small" disabled={page >= pages - 1} onClick={() => onPage(offset + limit)}>
          Next ›
        </button>
      </span>
    </div>
  )
}