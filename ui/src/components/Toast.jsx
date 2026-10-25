import { useEffect, useState } from 'react'

let toasts = []
const listeners = []

const notify = (msg, type = 'info', duration = 4000) => {
  const id = Date.now() + Math.random()
  toasts = [...toasts, { id, msg, type }]
  listeners.forEach((l) => l([...toasts]))
  if (duration > 0) setTimeout(() => dismiss(id), duration)
  return id
}

const dismiss = (id) => {
  toasts = toasts.filter((t) => t.id !== id)
  listeners.forEach((l) => l([...toasts]))
}

export const useToasts = () => {
  const [list, setList] = useState(toasts)
  useEffect(() => {
    listeners.push(setList)
    return () => {
      const i = listeners.indexOf(setList)
      if (i >= 0) listeners.splice(i, 1)
    }
  }, [])
  return list
}

export const toast = {
  success: (msg, d) => notify(msg, 'success', d),
  error: (msg, d) => notify(msg, 'error', d),
  warn: (msg, d) => notify(msg, 'warn', d),
  info: (msg, d) => notify(msg, 'info', d),
  dismiss,
}

export default function ToastContainer() {
  const list = useToasts()
  if (!list.length) return null
  return (
    <div className="toast-container" role="region" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} className={`toast ${t.type}`} onClick={() => dismiss(t.id)}>
          <span className="toast-icon">
            {t.type === 'success' && '✓'}
            {t.type === 'error' && '✕'}
            {t.type === 'warn' && '⚠'}
            {t.type === 'info' && 'ℹ'}
          </span>
          <span className="toast-msg">{t.msg}</span>
          <button className="toast-close" onClick={(e) => { e.stopPropagation(); dismiss(t.id); }} aria-label="dismiss">×</button>
        </div>
      ))}
    </div>
  )
}