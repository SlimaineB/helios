import { useEffect, useRef, useState } from 'react'
import { Search, X } from 'lucide-react'

function splitTerms(raw) {
  return String(raw || '').trim().split(/\s+/).filter(Boolean)
}

function addTerms(current, incoming) {
  const next = [...current]
  const seen = new Set(current.map((term) => term.toLowerCase()))
  for (const term of incoming) {
    const key = term.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    next.push(term)
  }
  return next
}

export default function SearchBox({ value = '', onSearch, placeholder = 'Search…', className = '' }) {
  const [tokens, setTokens] = useState(() => splitTerms(value))
  const [draft, setDraft] = useState('')
  const inputRef = useRef(null)
  const lastValue = useRef(value)

  useEffect(() => {
    if (value === lastValue.current) return
    lastValue.current = value
    setTokens(splitTerms(value))
    setDraft('')
  }, [value])

  const emit = (next) => {
    const query = next.join(' ')
    lastValue.current = query
    onSearch(query)
  }

  const commitDraft = () => {
    const next = addTerms(tokens, splitTerms(draft))
    setTokens(next)
    setDraft('')
    return next
  }

  const runSearch = () => emit(commitDraft())

  const removeToken = (index) => {
    const next = tokens.filter((_, i) => i !== index)
    setTokens(next)
    emit(next)
  }

  const clear = () => {
    setTokens([])
    setDraft('')
    emit([])
  }

  const onKeyDown = (event) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      runSearch()
      return
    }
    if (event.key === ' ' || event.key === 'Tab') {
      if (draft.trim()) {
        event.preventDefault()
        commitDraft()
      }
      return
    }
    if (event.key === 'Backspace' && draft === '' && tokens.length > 0) {
      event.preventDefault()
      removeToken(tokens.length - 1)
      return
    }
    if (event.key === 'Escape') {
      if (draft) setDraft('')
      else if (tokens.length) clear()
    }
  }

  const onPaste = (event) => {
    const text = event.clipboardData?.getData('text') || ''
    if (!/\s/.test(text.trim())) return
    event.preventDefault()
    setTokens(addTerms(tokens, splitTerms(text)))
  }

  return (
    <div className={`search-box${className ? ` ${className}` : ''}`} data-search-box onClick={() => inputRef.current?.focus()}>
      <Search size={14} className="search-box-icon" />
      {tokens.map((token, index) => (
        <span className="search-token" key={`${token}-${index}`}>
          {token}
          <button
            type="button"
            aria-label={`Remove ${token}`}
            onClick={(event) => {
              event.stopPropagation()
              removeToken(index)
            }}
          >
            ×
          </button>
        </span>
      ))}
      <input
        ref={inputRef}
        type="text"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        placeholder={tokens.length ? '' : placeholder}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        aria-label={placeholder}
      />
      {(tokens.length > 0 || draft) && (
        <button type="button" className="search-clear" onClick={(event) => { event.stopPropagation(); clear() }} aria-label="Clear search">
          <X size={13} />
        </button>
      )}
    </div>
  )
}
