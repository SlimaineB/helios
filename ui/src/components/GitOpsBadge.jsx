import Badge from './Badge.jsx'

export const managedByGitOps = (obj) => !!obj?.managed_by
export const managedTitle = (obj) =>
  obj?.managed_by
    ? `Managed by GitOps config repo "${obj.managed_by}". Edit the manifest in Git; the UI is read-only.`
    : ''

export default function GitOpsBadge({ obj, small = true }) {
  if (!managedByGitOps(obj)) return null
  return (
    <span title={managedTitle(obj)}>
      <Badge state="gitops" label={`GitOps · ${obj.managed_by}`} small={small} />
    </span>
  )
}
