import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { EventCatalogEntry, EventCatalogResponse } from '@/types/events'

async function fetchEventCatalog(): Promise<EventCatalogEntry[]> {
  const res = await api.get<EventCatalogResponse>('/events/catalog')
  return res.data.data.events
}

export function useEventCatalog() {
  const query = useQuery({
    queryKey: ['events', 'catalog'],
    queryFn: fetchEventCatalog,
    staleTime: 5 * 60 * 1000,
    gcTime: 10 * 60 * 1000,
    refetchOnWindowFocus: false,
  })

  const catalog = query.data

  const types = useMemo(() => {
    if (!catalog) return [] as string[]
    return [...new Set(catalog.map((c) => c.type))].sort()
  }, [catalog])

  const entityTypes = useMemo(() => {
    if (!catalog) return [] as string[]
    return [...new Set(catalog.map((c) => c.entity_type))].filter(Boolean).sort()
  }, [catalog])

  const sources = useMemo(() => {
    if (!catalog) return [] as string[]
    return [...new Set(catalog.map((c) => c.source))].filter(Boolean).sort()
  }, [catalog])

  return {
    catalog,
    types,
    entityTypes,
    sources,
    isLoading: query.isLoading,
    error: query.error,
  }
}
