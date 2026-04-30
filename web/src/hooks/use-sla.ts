import { useCallback, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { SLAMonthSummary, SLABreachesResponse } from '@/types/sla'

interface ApiResponse<T> {
  status: string
  data: T
}

interface SLAHistoryParams {
  months?: number
  year?: number
  operatorId?: string
}

export function useSLAHistory({ months, year, operatorId }: SLAHistoryParams = {}) {
  return useQuery({
    queryKey: ['sla', 'history', { months, year, operatorId }],
    queryFn: async () => {
      const p = new URLSearchParams()
      if (year) p.set('year', String(year))
      if (months) p.set('months', String(months))
      if (operatorId) p.set('operator_id', operatorId)
      const res = await api.get<ApiResponse<SLAMonthSummary[]>>(
        `/sla/history${p.size ? `?${p}` : ''}`,
      )
      return res.data.data
    },
    staleTime: 30_000,
  })
}

export class SLANotAvailableError extends Error {
  code = 'sla_month_not_available'
}

export function useSLAMonthDetail(year: number, month: number) {
  return useQuery({
    queryKey: ['sla', 'month', year, month],
    queryFn: async () => {
      try {
        const res = await api.get<ApiResponse<SLAMonthSummary>>(`/sla/months/${year}/${month}`)
        return res.data.data
      } catch (err: unknown) {
        const e = err as { response?: { status?: number; data?: { error?: { code?: string } } } }
        if (e?.response?.status === 404 && e.response?.data?.error?.code === 'sla_month_not_available') {
          throw new SLANotAvailableError('No SLA data for this month')
        }
        throw err
      }
    },
    staleTime: 30_000,
    enabled: year > 0 && month > 0,
    retry: (count, err) => {
      if (err instanceof SLANotAvailableError) return false
      return count < 2
    },
  })
}

export function useSLAOperatorBreaches(
  operatorId: string | null,
  year: number,
  month: number,
) {
  return useQuery({
    queryKey: ['sla', 'breaches', operatorId, year, month],
    queryFn: async () => {
      const res = await api.get<SLABreachesResponse>(
        `/sla/operators/${operatorId}/months/${year}/${month}/breaches`,
      )
      return res.data
    },
    staleTime: 30_000,
    enabled: Boolean(operatorId) && year > 0 && month > 0,
  })
}

interface UpdateOperatorSLAPayload {
  id: string
  sla_uptime_target?: number
  sla_latency_threshold_ms?: number
}

export function useUpdateOperatorSLA() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...payload }: UpdateOperatorSLAPayload) => {
      const res = await api.patch(`/operators/${id}`, payload)
      return res.data
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['operators'] })
      qc.invalidateQueries({ queryKey: ['sla'] })
    },
  })
}

interface SLAPDFDownloadParams {
  year: number
  month: number
  operatorId?: string
  operatorCode?: string
}

// useSLAPDFDownload performs an auth-attached fetch then triggers a blob download.
// A plain <a download> cannot attach the Bearer token this project uses (see
// web/src/hooks/use-export.ts for the same pattern on CDR exports).
export function useSLAPDFDownload() {
  const token = useAuthStore((s) => s.token)
  const [pending, setPending] = useState(false)

  const download = useCallback(
    async ({ year, month, operatorId, operatorCode }: SLAPDFDownloadParams) => {
      const mm = String(month).padStart(2, '0')
      const params = new URLSearchParams({ year: String(year), month: mm })
      if (operatorId) params.set('operator_id', operatorId)
      const url = `/api/v1/sla/pdf?${params.toString()}`
      const filename = `sla-${year}-${mm}-${operatorCode ?? (operatorId ? operatorId.slice(0, 8) : 'all')}.pdf`

      setPending(true)
      const toastId = toast.loading('Preparing PDF…')
      try {
        const res = await fetch(url, {
          headers: token ? { Authorization: `Bearer ${token}` } : undefined,
        })
        if (!res.ok) {
          if (res.status === 404) throw new Error('No SLA data available for this month')
          throw new Error(`HTTP ${res.status}`)
        }
        const blob = await res.blob()
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = filename
        a.click()
        URL.revokeObjectURL(a.href)
        toast.success('PDF downloaded', { id: toastId })
      } catch (err) {
        toast.error('PDF download failed', {
          id: toastId,
          description: err instanceof Error ? err.message : 'Unknown error',
        })
      } finally {
        setPending(false)
      }
    },
    [token],
  )

  return { download, pending }
}
