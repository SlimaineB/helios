import { useCallback, useEffect, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { api } from '../api/client.js'
import { useLiveRefresh } from '../hooks/useLiveRefresh.js'

export default function SyncLockBanner() {
  const [lock, setLock] = useState(null)
  const refresh = useCallback(async () => {
    try {
      const data = await api.syncLock()
      setLock(data?.enabled ? data : null)
    } catch {
      /* banner is best-effort */
    }
  }, [])
  useEffect(() => { refresh() }, [refresh])
  useLiveRefresh(refresh, { interval: 15000 })
  if (!lock) return null
  return (
    <div className="sync-lock-banner" role="alert">
      <AlertTriangle size={16} />
      <span>
        <strong>Global sync lock enabled.</strong> All syncs/applies are frozen fleet-wide; checks still run and drift may accumulate.
        {lock.reason && <> Reason: <em>{lock.reason}</em></>}
      </span>
    </div>
  )
}
