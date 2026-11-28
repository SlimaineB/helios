export function searchTerms(query) {
  return String(query || '').trim().toLowerCase().split(/\s+/).filter(Boolean)
}

export function matchesSearch(text, query) {
  const terms = searchTerms(query)
  if (!terms.length) return true
  const haystack = String(text || '').toLowerCase()
  return terms.every((term) => haystack.includes(term))
}
