import { useState, useCallback } from 'react'
import { Copy, Check } from 'lucide-react'
import { toast } from './Toast.jsx'

async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {}
  }
  if (typeof document.execCommand !== 'function') return false
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '0'
  area.style.left = '-9999px'
  document.body.appendChild(area)
  area.select()
  area.setSelectionRange(0, text.length)
  try {
    return document.execCommand('copy')
  } finally {
    document.body.removeChild(area)
  }
}

export default function CopyButton({ value, label = 'Copy', size = 'small', title = 'Copy to clipboard' }) {
  const [copied, setCopied] = useState(false)

  const handleClick = useCallback(async () => {
    try {
      const ok = await copyText(String(value ?? ''))
      if (!ok) throw new Error('copy failed')
      setCopied(true)
      toast.success('Copied to clipboard', 1500)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error('Failed to copy', 2000)
    }
  }, [value])

  const Icon = copied ? Check : Copy
  const className = `copy-btn ${size} ${copied ? 'copied' : ''}`

  return (
    <button
      type="button"
      className={className}
      onClick={handleClick}
      title={copied ? 'Copied!' : title}
      aria-label={copied ? 'Copied' : title}
    >
      <Icon size={size === 'small' ? 13 : 15} strokeWidth={2.5} aria-hidden="true" />
      {label !== 'Copy' && <span className="label">{label}</span>}
    </button>
  )
}