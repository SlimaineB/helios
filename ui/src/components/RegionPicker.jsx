import { useState } from 'react'
import { Plus } from 'lucide-react'
import { api } from '../api/client.js'
import { useAuth } from '../App.jsx'
import { toast } from './Toast.jsx'

export default function RegionPicker({ value, onChange, regions = [], onRegionCreated }) {
  const { can } = useAuth()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [saving, setSaving] = useState(false)

  const close = () => {
    setOpen(false)
    setName('')
  }

  const create = async () => {
    const trimmed = name.trim()
    if (!trimmed || saving) return
    setSaving(true)
    try {
      const created = await api.createRegion({ name: trimmed })
      onRegionCreated?.(created)
      onChange(created.name)
      close()
      toast.success('Region added')
    } catch (e) {
      toast.error(e.message)
    } finally {
      setSaving(false)
    }
  }

  const onKeyDown = (e) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      create()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      close()
    }
  }

  const hasValue = value && regions.some((r) => r.name === value)
  return (
    <>
      <div className="region-picker">
        <select value={value} onChange={(e) => onChange(e.target.value)}>
          <option value="">No region</option>
          {value && !hasValue && <option value={value}>{value} (not configured)</option>}
          {regions.map((r) => <option key={r.id} value={r.name}>{r.name}</option>)}
        </select>
        {can('regions', 'create') && !open && (
          <button type="button" className="btn ghost small" onClick={() => setOpen(true)}>
            <Plus size={13} /> New
          </button>
        )}
      </div>
      {open && (
        <div className="inline-create">
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder="e.g. eu-west-1"
            aria-label="New region name"
          />
          <button type="button" className="small" disabled={saving || !name.trim()} onClick={create}>
            {saving ? 'Adding…' : 'Add region'}
          </button>
          <button type="button" className="ghost small" onClick={close}>Cancel</button>
        </div>
      )}
    </>
  )
}
