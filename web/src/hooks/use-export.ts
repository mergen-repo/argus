import { useCallback, useState } from 'react'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth'

type ExportFilters = Record<string, string | number | undefined>

export function useExport(resource: string) {
  const [exporting, setExporting] = useState(false)
  const token = useAuthStore((s) => s.token)

  const exportCSV = useCallback(
    async (filters?: ExportFilters) => {
      setExporting(true)
      const toastId = toast.loading(`Exporting ${resource}…`)
      try {
        const params = new URLSearchParams()
        if (filters) {
          Object.entries(filters).forEach(([k, v]) => {
            if (v !== undefined && v !== '') params.set(k, String(v))
          })
        }
        const url = `/api/v1/${resource}/export.csv${params.size ? `?${params}` : ''}`
        const res = await fetch(url, {
          headers: { Authorization: `Bearer ${token}` },
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)

        const cd = res.headers.get('content-disposition') ?? ''
        const match = cd.match(/filename="?([^";\n]+)"?/)
        const filename = match?.[1] ?? `${resource}_export.csv`

        const blob = await res.blob()
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = filename
        a.click()
        URL.revokeObjectURL(a.href)
        toast.success(`${resource} exported`, { id: toastId })
      } catch (err) {
        toast.error('Export failed', { id: toastId })
      } finally {
        setExporting(false)
      }
    },
    [resource, token],
  )

  return { exportCSV, exporting }
}
