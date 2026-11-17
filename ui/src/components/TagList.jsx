export function parseTags(value) {
  const tags = []
  const seen = new Set()
  for (const raw of String(value || '').split(',')) {
    let tag = raw.trim()
    if (!tag) continue
    const chars = Array.from(tag)
    if (chars.length > 64) tag = chars.slice(0, 64).join('')
    const key = tag.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    tags.push(tag)
    if (tags.length === 64) break
  }
  return tags
}

export const formatTags = (tags) => (tags || []).join(', ')

export default function TagList({ tags, className = '' }) {
  if (!tags?.length) return null
  return (
    <span className={`tag-list${className ? ` ${className}` : ''}`}>
      {tags.map((tag) => (
        <span className="tag-chip" key={tag}>{tag}</span>
      ))}
    </span>
  )
}
