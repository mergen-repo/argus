import { useCallback, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'

interface UseTabUrlSyncOptions {
  defaultTab: string
  aliases?: Record<string, string>
  validTabs?: string[]
  paramName?: string
}

export function useTabUrlSync({
  defaultTab,
  aliases = {},
  validTabs,
  paramName = 'tab',
}: UseTabUrlSyncOptions): [string, (tab: string) => void] {
  const [searchParams, setSearchParams] = useSearchParams()

  const rawTab = searchParams.get(paramName)

  // Resolve alias (e.g. circuit → health, notifications → alerts)
  const resolvedFromAlias = rawTab ? (aliases[rawTab] ?? rawTab) : rawTab

  // Validate against known tabs
  const isValid =
    resolvedFromAlias !== null &&
    (validTabs === undefined || validTabs.includes(resolvedFromAlias))

  const activeTab = isValid && resolvedFromAlias ? resolvedFromAlias : defaultTab

  // Redirect effect: if rawTab is an alias or an invalid value, rewrite URL without
  // adding a history entry. Effect self-terminates: after redirect, rawTab resolves
  // to the canonical value and both guards evaluate false — no infinite loop.
  useEffect(() => {
    if (!rawTab) return
    const needsAliasRedirect = !!aliases[rawTab]
    const needsInvalidFallback = !needsAliasRedirect && !!validTabs && !validTabs.includes(rawTab)
    if (!needsAliasRedirect && !needsInvalidFallback) return
    const target = needsAliasRedirect ? aliases[rawTab] : defaultTab
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set(paramName, target)
        return next
      },
      { replace: true },
    )
  }, [rawTab, aliases, validTabs, defaultTab, paramName, setSearchParams])

  const setTab = useCallback(
    (tab: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.set(paramName, tab)
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams, paramName],
  )

  return [activeTab, setTab]
}
