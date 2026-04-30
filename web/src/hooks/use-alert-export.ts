import { useCallback, useState } from 'react'
import axios from 'axios'
import { toast } from 'sonner'
import { api } from '@/lib/api'

export type AlertExportFormat = 'csv' | 'json' | 'pdf'

export interface UseAlertExportResult {
  exportAs: (format: AlertExportFormat, filters: Record<string, string>) => Promise<void>
  exporting: boolean
}

// extractBlobError tries to read a JSON error envelope out of a blob response.
// If the blob is JSON it returns the server message; otherwise undefined.
async function extractBlobError(data: unknown): Promise<string | undefined> {
  if (!(data instanceof Blob)) return undefined
  try {
    const text = await data.text()
    if (!text) return undefined
    const parsed = JSON.parse(text) as { error?: { message?: string } }
    return parsed?.error?.message
  } catch {
    return undefined
  }
}

export function useAlertExport(): UseAlertExportResult {
  const [exporting, setExporting] = useState(false)

  const exportAs = useCallback(
    async (format: AlertExportFormat, filters: Record<string, string>) => {
      setExporting(true)
      const toastId = toast.loading('Preparing export…')
      try {
        const params = new URLSearchParams()
        Object.entries(filters).forEach(([k, v]) => {
          if (v !== undefined && v !== '') params.set(k, v)
        })
        const qs = params.size ? `?${params.toString()}` : ''
        // FIX-229 Gate F-A10: route through the central axios client so the
        // request inherits Authorization + 401 refresh + future global headers.
        // /alerts/export* is in silentPaths so axios won't double-toast on 4xx.
        const res = await api.get(`/alerts/export.${format}${qs}`, {
          responseType: 'blob',
        })

        const cd = (res.headers['content-disposition'] as string | undefined) ?? ''
        const match = cd.match(/filename="?([^";\n]+)"?/)
        const isoSafe = new Date().toISOString().replace(/[:.]/g, '-')
        const filename = match?.[1] ?? `alerts-${isoSafe}.${format}`

        const blob = res.data as Blob
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = filename
        a.click()
        URL.revokeObjectURL(a.href)

        toast.success('Export downloaded', { id: toastId })
      } catch (err: unknown) {
        let message = 'Export failed'
        if (axios.isAxiosError(err)) {
          const status = err.response?.status
          // When responseType is 'blob' the error body itself is a Blob even
          // for JSON envelopes — re-parse it so we surface the server message.
          const fromBlob = await extractBlobError(err.response?.data)
          if (fromBlob) {
            message = fromBlob
          } else if (status) {
            message = `Export failed (HTTP ${status})`
          }
        }
        toast.error(message, { id: toastId })
      } finally {
        setExporting(false)
      }
    },
    [],
  )

  return { exportAs, exporting }
}
