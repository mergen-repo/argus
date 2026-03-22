import { useQuery, useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useCallback } from 'react'
import { api } from '@/lib/api'
import { wsClient } from '@/lib/ws'
import type { Job, JobProgressEvent, JobCompletedEvent, JobError } from '@/types/job'
import type { ListResponse, ApiResponse } from '@/types/sim'

const JOBS_KEY = ['jobs'] as const

export function useJobList(filters: { type?: string; state?: string }) {
  return useInfiniteQuery({
    queryKey: [...JOBS_KEY, 'list', filters],
    queryFn: async ({ pageParam }) => {
      const params = new URLSearchParams()
      if (pageParam) params.set('cursor', pageParam as string)
      params.set('limit', '20')
      if (filters.type) params.set('type', filters.type)
      if (filters.state) params.set('state', filters.state)
      const res = await api.get<ListResponse<Job>>(`/jobs?${params.toString()}`)
      return res.data
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more ? lastPage.meta.cursor : undefined,
    staleTime: 10_000,
  })
}

export function useJobDetail(id: string) {
  return useQuery({
    queryKey: [...JOBS_KEY, 'detail', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<Job>>(`/jobs/${id}`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 5_000,
  })
}

export function useJobErrors(id: string) {
  return useQuery({
    queryKey: [...JOBS_KEY, 'errors', id],
    queryFn: async () => {
      const res = await api.get<ApiResponse<JobError[]>>(`/jobs/${id}/errors`)
      return res.data.data
    },
    enabled: !!id,
    staleTime: 30_000,
  })
}

export function useRetryJob() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (jobId: string) => {
      const res = await api.post(`/jobs/${jobId}/retry`)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: JOBS_KEY })
    },
  })
}

export function useCancelJob() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (jobId: string) => {
      const res = await api.post(`/jobs/${jobId}/cancel`)
      return res.data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: JOBS_KEY })
    },
  })
}

export function useRealtimeJobProgress() {
  const queryClient = useQueryClient()

  const progressHandler = useCallback(
    (data: unknown) => {
      const event = data as JobProgressEvent
      if (!event.job_id) return

      queryClient.setQueryData<{ pages: ListResponse<Job>[]; pageParams: string[] }>(
        [...JOBS_KEY, 'list', {}],
        (old) => {
          if (!old?.pages) return old
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.map((job) =>
                job.id === event.job_id
                  ? {
                      ...job,
                      state: event.state as Job['state'],
                      processed_items: event.processed_items,
                      failed_items: event.failed_items,
                      progress_pct: event.progress_pct,
                      total_items: event.total_items,
                    }
                  : job,
              ),
            })),
          }
        },
      )
    },
    [queryClient],
  )

  const completedHandler = useCallback(
    (data: unknown) => {
      const event = data as JobCompletedEvent
      if (!event.job_id) return

      queryClient.setQueryData<{ pages: ListResponse<Job>[]; pageParams: string[] }>(
        [...JOBS_KEY, 'list', {}],
        (old) => {
          if (!old?.pages) return old
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.map((job) =>
                job.id === event.job_id
                  ? {
                      ...job,
                      state: event.final_state as Job['state'],
                      processed_items: event.processed_items,
                      failed_items: event.failed_items,
                      progress_pct: event.progress_pct,
                      total_items: event.total_items,
                      completed_at: event.completed_at,
                    }
                  : job,
              ),
            })),
          }
        },
      )

      queryClient.invalidateQueries({ queryKey: [...JOBS_KEY, 'detail', event.job_id] })
    },
    [queryClient],
  )

  useEffect(() => {
    const unsub1 = wsClient.on('job.progress', progressHandler)
    const unsub2 = wsClient.on('job.completed', completedHandler)
    return () => {
      unsub1()
      unsub2()
    }
  }, [progressHandler, completedHandler])
}
