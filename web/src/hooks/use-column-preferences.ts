import { useState, useCallback, useEffect, useRef } from 'react'
import { api } from '@/lib/api'

const LOCAL_PREFIX = 'argus:columns:'

export function useColumnPreferences(pageKey: string, defaultColumns: string[]) {
  const [columns, setColumnsState] = useState<string[]>(() => {
    try {
      const stored = localStorage.getItem(LOCAL_PREFIX + pageKey)
      if (stored) return JSON.parse(stored) as string[]
    } catch {}
    return defaultColumns
  })

  const syncTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const setColumns = useCallback(
    (cols: string[]) => {
      setColumnsState(cols)
      localStorage.setItem(LOCAL_PREFIX + pageKey, JSON.stringify(cols))

      if (syncTimerRef.current) clearTimeout(syncTimerRef.current)
      syncTimerRef.current = setTimeout(() => {
        api
          .patch('/users/me/preferences', { columns: { [pageKey]: cols } })
          .catch(() => {})
      }, 1000)
    },
    [pageKey],
  )

  useEffect(() => {
    api
      .get<{ status: string; data: { columns?: Record<string, string[]> } }>('/users/me/preferences')
      .then((res) => {
        const serverCols = res.data.data?.columns?.[pageKey]
        if (serverCols && serverCols.length > 0) {
          setColumnsState(serverCols)
          localStorage.setItem(LOCAL_PREFIX + pageKey, JSON.stringify(serverCols))
        }
      })
      .catch(() => {})
  }, [pageKey])

  useEffect(() => () => { if (syncTimerRef.current) clearTimeout(syncTimerRef.current) }, [])

  return { columns, setColumns }
}
