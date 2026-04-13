import { useCallback, useState } from 'react'
import { toast } from 'sonner'
import { toPng, toCanvas } from 'html-to-image'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'

export interface ChartAnnotation {
  id: string
  chart_key: string
  timestamp: string
  label: string
  user_id?: string
  created_at: string
}

export function useChartExport(ref: React.RefObject<HTMLElement | null>) {
  const [exporting, setExporting] = useState(false)

  const exportPng = useCallback(
    async (filename?: string) => {
      if (!ref.current) return
      setExporting(true)
      const toastId = toast.loading('Exporting chart…')
      try {
        let dataUrl: string
        try {
          dataUrl = await toPng(ref.current, { cacheBust: true, pixelRatio: 2 })
        } catch {
          const canvas = await toCanvas(ref.current)
          dataUrl = canvas.toDataURL('image/png')
        }
        const a = document.createElement('a')
        a.href = dataUrl
        a.download = filename ?? 'chart.png'
        a.click()
        toast.success('Chart exported', { id: toastId })
      } catch (err) {
        toast.error('Chart export failed', { id: toastId })
      } finally {
        setExporting(false)
      }
    },
    [ref],
  )

  return { exportPng, exporting }
}

export function useChartAnnotations(
  chartKey: string,
  from?: string,
  to?: string,
) {
  const qc = useQueryClient()
  const key = ['chart-annotations', chartKey, from, to]

  const query = useQuery({
    queryKey: key,
    queryFn: async () => {
      const params = new URLSearchParams({ chart_key: chartKey })
      if (from) params.set('from', from)
      if (to) params.set('to', to)
      const res = await api.get<{ status: string; data: ChartAnnotation[] }>(
        `/analytics/charts/${encodeURIComponent(chartKey)}/annotations?${params}`,
      )
      return res.data.data
    },
    enabled: !!chartKey,
    staleTime: 30_000,
  })

  const create = useMutation({
    mutationFn: (body: { timestamp: string; label: string }) =>
      api.post(`/analytics/charts/${encodeURIComponent(chartKey)}/annotations`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  const remove = useMutation({
    mutationFn: (id: string) =>
      api.delete(`/analytics/charts/${encodeURIComponent(chartKey)}/annotations/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  })

  return { ...query, create, remove }
}
