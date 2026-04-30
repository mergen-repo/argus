// FIX-236 DEV-552: filter-based bulk preview hook.
//
// Calls `POST /api/v1/{resource}/bulk/preview-count` with the active list
// filter and surfaces { count, sample_ids, capped } so the FE can decide
// whether to show the destructive-confirm dialog before dispatching the
// bulk-by-filter endpoint.

import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

export interface BulkPreviewResult {
  count: number
  sample_ids: string[]
  capped: boolean
  cap: number
}

interface PreviewVars {
  resource: string
  filter: Record<string, string | undefined>
  maxAffected?: number
}

/**
 * Returns a TanStack mutation that resolves a filter against the server. The
 * endpoint shape is `POST /api/v1/{resource}/bulk/preview-count` with body
 * `{ filter, max_affected? }`.
 *
 * Usage pattern:
 *   const preview = useBulkPreviewCount()
 *   const r = await preview.mutateAsync({ resource: 'sims', filter: {...} })
 *   if (r.count > 1000) showDestructiveConfirm({ count: r.count })
 *   else proceedDirectly()
 */
export function useBulkPreviewCount() {
  return useMutation({
    mutationFn: async ({ resource, filter, maxAffected }: PreviewVars) => {
      const cleanFilter: Record<string, string> = {}
      Object.entries(filter).forEach(([k, v]) => {
        if (v !== undefined && v !== '') cleanFilter[k] = v
      })
      const res = await api.post<ApiResponse<BulkPreviewResult>>(
        `/${resource}/bulk/preview-count`,
        { filter: cleanFilter, max_affected: maxAffected },
      )
      return res.data.data
    },
  })
}
