import { useMemo, useRef, useState, type DragEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AlertCircle,
  CheckCircle2,
  Download,
  FileSpreadsheet,
  RefreshCw,
  Upload,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { FileInput } from '@/components/ui/file-input'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Badge } from '@/components/ui/badge'
import { useIMEIPoolBulkImport } from '@/hooks/use-imei-pools'
import { useJobPolling } from '@/hooks/use-jobs'
import {
  IMEI_POOLS,
  POOL_LABEL,
  hasCSVInjection,
  type IMEIPool,
} from '@/types/imei-pool'

const MAX_BYTES = 10 * 1024 * 1024 // 10 MB
const MAX_ROWS = 100_000

interface BulkImportTabProps {
  initialPool?: IMEIPool
}

interface JobErrorRow {
  row?: number
  imei_or_tac?: string
  iccid?: string
  error_code?: string
  error_message?: string
  [k: string]: unknown
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

function downloadErrorCSV(errors: JobErrorRow[]) {
  const headers = ['row', 'imei_or_tac', 'error_code', 'error_message']
  const escape = (v: unknown) => {
    if (v === null || v === undefined) return ''
    const s = String(v)
    if (/[",\n]/.test(s)) return `"${s.replace(/"/g, '""')}"`
    return s
  }
  const rows = errors.map((e) =>
    [escape(e.row), escape(e.imei_or_tac ?? e.iccid ?? ''), escape(e.error_code), escape(e.error_message)].join(','),
  )
  const csv = [headers.join(','), ...rows].join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `imei-pool-import-errors-${Date.now()}.csv`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export function BulkImportTab({ initialPool = 'whitelist' }: BulkImportTabProps) {
  const navigate = useNavigate()
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const [pool, setPool] = useState<IMEIPool>(initialPool)
  const [file, setFile] = useState<File | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [validationError, setValidationError] = useState<string | null>(null)
  // Non-blocking pre-flight warning: first 1000 rows of the file are scanned
  // client-side for cells starting with =, +, -, @ or tab. Server still
  // enforces the same rule via the worker, so this is purely UX (the import
  // would still partial-succeed). STORY-095 Gate F-A11.
  const [csvPreflightWarning, setCsvPreflightWarning] = useState<string | null>(null)

  const upload = useIMEIPoolBulkImport(pool)
  const job = useJobPolling(jobId, { intervalMs: 2000 })

  const isPolling = !!jobId && (job.data?.state === 'queued' || job.data?.state === 'running')
  const isFinished = !!jobId && (job.data?.state === 'completed' || job.data?.state === 'failed' || job.data?.state === 'cancelled')

  const handleFile = (f: File | null) => {
    setValidationError(null)
    setCsvPreflightWarning(null)
    if (!f) {
      setFile(null)
      return
    }
    if (f.size > MAX_BYTES) {
      setValidationError(`File too large (${formatBytes(f.size)}). Max 10 MB.`)
      return
    }
    if (!/\.csv$/i.test(f.name)) {
      setValidationError('Only .csv files are accepted.')
      return
    }
    setFile(f)
    // Pre-flight scan for CSV-injection: read up to ~1MB of text, parse the
    // first 1000 rows, and surface a non-blocking warning if any field begins
    // with a formula-trigger character. Cap protects very large files.
    void scanForCSVInjection(f).then((report) => {
      if (report.suspectRows.length > 0) {
        const sample = report.suspectRows.slice(0, 3).join(', ')
        setCsvPreflightWarning(
          `Heads-up — ${report.suspectRows.length} row${report.suspectRows.length === 1 ? '' : 's'} of the first ${report.scannedRows} contain a value starting with =, +, -, @ or tab (rows: ${sample}${report.suspectRows.length > 3 ? '…' : ''}). The server will reject these on import.`,
        )
      }
    })
  }

  // Reads up to 1 MB of the file as text, splits on newline, scans first 1000
  // rows, and returns a list of 1-based row numbers whose any column tripped
  // hasCSVInjection().
  async function scanForCSVInjection(f: File): Promise<{ scannedRows: number; suspectRows: number[] }> {
    const SCAN_BYTES = 1024 * 1024
    const SCAN_ROWS = 1000
    const slice = f.slice(0, SCAN_BYTES)
    let text: string
    try {
      text = await slice.text()
    } catch {
      return { scannedRows: 0, suspectRows: [] }
    }
    const lines = text.split(/\r?\n/)
    // Skip the header row (line 0); body rows start at index 1 = "row 2".
    const body = lines.slice(1, 1 + SCAN_ROWS)
    const suspect: number[] = []
    for (let i = 0; i < body.length; i++) {
      if (!body[i]) continue
      const cells = body[i].split(',')
      if (cells.some((c) => hasCSVInjection(c))) {
        suspect.push(i + 2) // 1-based row number including header
      }
    }
    return { scannedRows: body.length, suspectRows: suspect }
  }

  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setDragOver(false)
    const f = e.dataTransfer.files?.[0]
    if (f) handleFile(f)
  }

  const handleSubmit = async () => {
    if (!file) return
    try {
      const result = await upload.mutateAsync(file)
      setJobId(result.job_id)
      toast.success('Import queued', { description: `Job ${result.job_id.slice(0, 8)} started.` })
    } catch (err) {
      const e = err as { response?: { data?: { error?: { message?: string } } } }
      const msg = e?.response?.data?.error?.message
      if (msg) toast.error(msg)
    }
  }

  const handleReset = () => {
    setFile(null)
    setJobId(null)
    setValidationError(null)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const errorRows: JobErrorRow[] = useMemo(() => {
    const report = job.data?.error_report
    if (Array.isArray(report)) return report as JobErrorRow[]
    if (report && typeof report === 'object' && Array.isArray((report as { errors?: unknown }).errors)) {
      return (report as { errors: JobErrorRow[] }).errors
    }
    return []
  }, [job.data?.error_report])

  const progressPct =
    job.data && job.data.total_items > 0
      ? Math.min(100, Math.round((job.data.processed_items / job.data.total_items) * 100))
      : 0

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="p-5 space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="text-[13px] font-semibold text-text-primary">Bulk Import</h2>
              <p className="text-xs text-text-secondary mt-1">
                Upload a CSV to register many entries at once. Max 10 MB and 100,000 rows. Each row is validated; invalid rows are reported in the error CSV.
              </p>
            </div>
            <div>
              <label htmlFor="bulk-target-pool" className="text-xs font-medium text-text-secondary block mb-1.5">
                Target pool
              </label>
              <Select
                id="bulk-target-pool"
                value={pool}
                onChange={(e) => setPool(e.target.value as IMEIPool)}
                options={IMEI_POOLS.map((p) => ({ value: p, label: POOL_LABEL[p] }))}
                disabled={isPolling}
                className="w-[160px]"
              />
            </div>
          </div>

          {!jobId && (
            <>
              <div
                onDragOver={(e) => {
                  e.preventDefault()
                  setDragOver(true)
                }}
                onDragLeave={() => setDragOver(false)}
                onDrop={handleDrop}
                className={
                  'rounded-[var(--radius-md)] border-2 border-dashed p-10 text-center transition-colors ' +
                  (dragOver
                    ? 'border-accent bg-accent-dim/40'
                    : 'border-border bg-bg-elevated hover:border-text-tertiary')
                }
              >
                <FileSpreadsheet className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
                <p className="text-sm font-medium text-text-primary mb-1">
                  Drop CSV here, or choose a file
                </p>
                <p className="text-xs text-text-secondary mb-4">
                  Schema: <span className="font-mono text-text-primary">imei_or_tac, kind, device_model, description, quarantine_reason, block_reason, imported_from</span>
                </p>
                <FileInput
                  ref={fileInputRef}
                  accept=".csv,text/csv"
                  className="sr-only"
                  onChange={(e) => handleFile(e.target.files?.[0] ?? null)}
                  aria-label="Choose CSV file"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => fileInputRef.current?.click()}
                  className="gap-1.5"
                >
                  <Upload className="h-3.5 w-3.5" />
                  Choose File
                </Button>
                <p className="mt-3 text-[11px] text-text-tertiary font-mono">
                  Max 10 MB / {MAX_ROWS.toLocaleString()} rows
                </p>
              </div>

              {validationError && (
                <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim p-3 text-xs text-danger flex items-start gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                  <span>{validationError}</span>
                </div>
              )}

              {!validationError && csvPreflightWarning && (
                <div
                  role="status"
                  className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim p-3 text-xs text-warning flex items-start gap-2"
                >
                  <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                  <span>{csvPreflightWarning}</span>
                </div>
              )}

              {file && !validationError && (
                <div className="rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-3 flex items-center gap-3">
                  <FileSpreadsheet className="h-5 w-5 text-accent shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-text-primary truncate">{file.name}</p>
                    <p className="text-[11px] text-text-tertiary font-mono">
                      {formatBytes(file.size)} · target: {POOL_LABEL[pool]}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => handleFile(null)}
                    aria-label="Remove file"
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              )}

              <div className="flex items-center justify-end gap-2 pt-2">
                <Button variant="outline" onClick={handleReset} disabled={!file || upload.isPending}>
                  Cancel
                </Button>
                <Button
                  onClick={handleSubmit}
                  disabled={!file || !!validationError || upload.isPending}
                  className="gap-1.5"
                >
                  {upload.isPending && <Spinner className="h-3.5 w-3.5" />}
                  {upload.isPending ? 'Uploading…' : 'Start Import'}
                </Button>
              </div>
            </>
          )}

          {jobId && (
            <div className="rounded-[var(--radius-md)] border border-border bg-bg-elevated p-4 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  {isPolling ? (
                    <Spinner className="h-4 w-4 text-accent" />
                  ) : job.data?.state === 'completed' ? (
                    <CheckCircle2 className="h-4 w-4 text-success" />
                  ) : (
                    <AlertCircle className="h-4 w-4 text-danger" />
                  )}
                  <span className="text-sm font-semibold text-text-primary">
                    Job {jobId.slice(0, 8)}…
                  </span>
                  {job.data?.state && (
                    <Badge
                      variant={
                        job.data.state === 'completed'
                          ? 'success'
                          : job.data.state === 'failed' || job.data.state === 'cancelled'
                            ? 'danger'
                            : 'default'
                      }
                      className="uppercase tracking-wider text-[10px]"
                    >
                      {job.data.state}
                    </Badge>
                  )}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => navigate(`/jobs?focus=${jobId}`)}
                  className="text-xs"
                >
                  View in Jobs
                </Button>
              </div>

              <div className="space-y-1.5">
                <div className="flex items-center justify-between text-[11px] text-text-tertiary font-mono">
                  <span>
                    {(job.data?.processed_items ?? 0).toLocaleString()} / {(job.data?.total_items ?? 0).toLocaleString()} processed
                  </span>
                  <span>{progressPct}%</span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded-full bg-bg-hover">
                  <div
                    className={
                      'h-full transition-all duration-500 ' +
                      (job.data?.state === 'failed' ? 'bg-danger' : 'bg-accent')
                    }
                    style={{ width: `${progressPct}%` }}
                  />
                </div>
                <div className="flex items-center justify-between text-[11px] font-mono text-text-secondary">
                  <span>Failed: <span className="text-danger">{(job.data?.failed_items ?? 0).toLocaleString()}</span></span>
                  <span>
                    Success: <span className="text-success">{((job.data?.processed_items ?? 0) - (job.data?.failed_items ?? 0)).toLocaleString()}</span>
                  </span>
                </div>
              </div>

              {isFinished && (
                <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
                  {errorRows.length > 0 && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => downloadErrorCSV(errorRows)}
                      className="gap-1.5"
                    >
                      <Download className="h-3.5 w-3.5" />
                      Errors CSV
                    </Button>
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleReset}
                    className="gap-1.5"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    New Import
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
