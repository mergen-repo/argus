import { useState, useEffect, useCallback } from 'react'

function parseHash(hash: string, validTabs: string[], defaultTab: string): string {
  const value = hash.replace(/^#/, '')
  if (value && validTabs.includes(value)) return value
  return defaultTab
}

export function useHashTab(
  defaultTab: string,
  validTabs: string[],
): [string, (value: string) => void] {
  const [tab, setTabState] = useState<string>(() => {
    if (typeof window === 'undefined') return defaultTab
    return parseHash(window.location.hash, validTabs, defaultTab)
  })

  useEffect(() => {
    const handlePopState = () => {
      setTabState(parseHash(window.location.hash, validTabs, defaultTab))
    }
    window.addEventListener('popstate', handlePopState)
    return () => {
      window.removeEventListener('popstate', handlePopState)
    }
  }, [validTabs, defaultTab])

  const setTab = useCallback((value: string) => {
    window.history.pushState(null, '', '#' + value)
    setTabState(value)
  }, [])

  return [tab, setTab]
}
