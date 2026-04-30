import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { ApiResponse, ListResponse } from '@/types/sim'

const REPORTS_KEY = ['reports'] as const

export interface ReportDefinition {
  id: string
  category: string
  name: string
  description: string
  format_options: string[]
}

export function useReportDefinitions() {
  return useQuery({
    queryKey: [...REPORTS_KEY, 'definitions'],
    queryFn: async () => {
      const res = await api.get<ApiResponse<ReportDefinition[]>>('/reports/definitions')
      return res.data.data ?? []
    },
    staleTime: 5 * 60_000,
  })
}

export interface ScheduledReport {
  id: string
  tenant_id: string
  report_type: string
  schedule_cron: string
  format: string
  recipients: string[]
  filters: Record<string, unknown>
  state: string
  next_run_at: string | null
  last_run_at: string | null
  created_at: string
  updated_at: string
}

export interface GenerateReportRequest {
  report_type: string
  format: string
  filters?: Record<string, unknown>
}

export interface GenerateReportResponse {
  job_id: string
  status: string
}

export function useGenerateReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (req: GenerateReportRequest) => {
      const res = await api.post<ApiResponse<GenerateReportResponse>>('/reports/generate', req)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] })
    },
  })
}

export function useScheduledReports(cursor: string = '', limit: number = 50) {
  return useQuery({
    queryKey: [...REPORTS_KEY, 'scheduled', cursor, limit],
    queryFn: async () => {
      const params = new URLSearchParams()
      if (cursor) params.set('cursor', cursor)
      params.set('limit', String(limit))
      const res = await api.get<ListResponse<ScheduledReport>>(
        `/reports/scheduled?${params.toString()}`,
      )
      return res.data
    },
    staleTime: 30_000,
  })
}

export function useCreateScheduledReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (req: {
      report_type: string
      schedule_cron: string
      format: string
      recipients: string[]
      filters?: Record<string, unknown>
    }) => {
      const res = await api.post<ApiResponse<ScheduledReport>>('/reports/scheduled', req)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...REPORTS_KEY, 'scheduled'] })
    },
  })
}

export function useUpdateScheduledReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async ({
      id,
      patch,
    }: {
      id: string
      patch: Partial<{
        schedule_cron: string
        recipients: string[]
        filters: Record<string, unknown>
        state: string
        format: string
      }>
    }) => {
      const res = await api.patch<ApiResponse<ScheduledReport>>(`/reports/scheduled/${id}`, patch)
      return res.data.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...REPORTS_KEY, 'scheduled'] })
    },
  })
}

export function useDeleteScheduledReport() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) => {
      await api.delete(`/reports/scheduled/${id}`)
      return id
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: [...REPORTS_KEY, 'scheduled'] })
    },
  })
}
