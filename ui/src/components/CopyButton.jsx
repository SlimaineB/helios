import { useState, useCallback } from 'react'
import { Copy, Check } from 'lucide-react'
import { toast } from './Toast.jsx'

export default function CopyButton({ value, label = 'Copy', size = 'small', title = 'Copy to clipboard' }) {
  const [copied, setCopied] = useState(false)

  const handleClick = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(value)
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