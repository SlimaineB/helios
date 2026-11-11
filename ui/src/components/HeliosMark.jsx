export default function HeliosMark({ className = '' }) {
  return (
    <span className={`brand-mark ${className}`.trim()} aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round">
        <circle cx="12" cy="12" r="3.4" fill="currentColor" stroke="none" />
        <path d="M12 2.4v2.1M12 19.5v2.1M4.2 12H2.1M21.9 12h-2.1" />
        <path d="m6.5 6.5-1.5-1.5M19 19l-1.5-1.5M17.5 6.5 19 5M5 19l1.5-1.5" />
      </svg>
    </span>
  )
}
